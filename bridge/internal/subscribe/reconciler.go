// Package subscribe keeps a live view of herdr's panes and agent states.
//
// The shape of this component is dictated by two measured facts
// (lab/bridge-probe, re-verified against the 0.9.1 schema):
//
//   - pane.agent_status_changed is REJECTED unless it carries pane_id, so
//     status cannot be subscribed globally. It must be subscribed per pane.
//   - The schema's delivery enum for events.subscribe lists only
//     pane.output_matched, pane.agent_status_changed and pane.scroll_changed,
//     while the submittable set is much wider and includes lifecycle events.
//     Lifecycle delivery is therefore treated as a hint, never as the source of
//     truth for the pane inventory.
//
// Consequences baked in here:
//
//   - Subscribe first, snapshot second. A status change that happens between
//     the two is either seen by the stream or is already in the snapshot.
//     Snapshotting before subscribing would leave an unobserved window.
//   - Rebalance by opening the new stream before closing the old one. A brief
//     overlap can duplicate an event (harmless: consumers dedupe by pane+seq)
//     whereas a gap can silently lose a decision request, which is not.
//   - A slow periodic reconcile backstops the ambiguous lifecycle delivery.
package subscribe

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

// Source is the part of the herdr client this package needs, so tests can
// substitute a fake.
type Source interface {
	Snapshot(ctx context.Context) (*herdr.Snapshot, error)
	Subscribe(ctx context.Context, subs []herdr.Subscription) (*herdr.Stream, error)
}

// Options tunes the reconciler. Zero values pick the documented defaults.
type Options struct {
	// ReconcileInterval is the backstop snapshot period. It exists because
	// lifecycle delivery is not guaranteed by the schema; it is not the primary
	// mechanism.
	ReconcileInterval time.Duration
	// MaxBackoff bounds reconnect backoff.
	MaxBackoff time.Duration
	// Logger receives lifecycle notes. Defaults to a discard logger.
	Logger *slog.Logger
}

func (o Options) withDefaults() Options {
	if o.ReconcileInterval <= 0 {
		o.ReconcileInterval = 5 * time.Minute
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = 30 * time.Second
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return o
}

// Handlers receive reconciled state.
type Handlers struct {
	// OnSnapshot is called with a full authoritative state each time the
	// reconciler re-reads the session. Consumers MUST rebuild their mirror from
	// it rather than merging (data-model.md §3).
	OnSnapshot func(*herdr.Snapshot)
	// OnStatusChange is called for each pane.agent_status_changed event, and
	// again for each pane whose status differs from the last snapshot, so a
	// missed event cannot hide a blocked agent.
	OnStatusChange func(herdr.StatusChange, bool)
	// OnResync is called when the stream was lost and the reconciler had to
	// reconnect. Consumers can use it to tell the user the view was refreshed.
	OnResync func(reason error)
}

// Reconciler maintains the subscription set and the mirror.
type Reconciler struct {
	src  Source
	opts Options
	h    Handlers

	mu       sync.Mutex
	stream   *herdr.Stream
	panes    map[string]struct{}
	lastSeen map[string]herdr.AgentStatus
	rebalCh  chan struct{}
}

// New builds a reconciler.
func New(src Source, opts Options, h Handlers) *Reconciler {
	return &Reconciler{
		src:      src,
		opts:     opts.withDefaults(),
		h:        h,
		panes:    map[string]struct{}{},
		lastSeen: map[string]herdr.AgentStatus{},
		rebalCh:  make(chan struct{}, 1),
	}
}

// Panes returns the current pane id set (for tests and diagnostics).
func (r *Reconciler) Panes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.panes))
	for id := range r.panes {
		out = append(out, id)
	}
	return out
}

// Run blocks until ctx is done, reconnecting as needed.
func (r *Reconciler) Run(ctx context.Context) {
	backoff := 250 * time.Millisecond
	for ctx.Err() == nil {
		err := r.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if r.h.OnResync != nil {
				r.h.OnResync(err)
			}
			r.opts.Logger.Warn("herdr stream lost, reconnecting", "err", err)
		}
		// Jittered backoff: a herdr restart must not turn every bridge in the
		// world into a synchronised hammer.
		sleep := backoff + time.Duration(rand.Int63n(int64(backoff/2+1)))
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleep):
		}
		if backoff < r.opts.MaxBackoff {
			backoff *= 2
		}
	}
}

// session runs one subscribe-and-drain cycle.
func (r *Reconciler) session(ctx context.Context) error {
	// 1. Read current panes, then arm the stream.
	snap, err := r.src.Snapshot(ctx)
	if err != nil {
		return err
	}
	stream, err := r.src.Subscribe(ctx, herdr.AgentStatesSubscriptions(paneIDs(snap.Panes)))
	if err != nil {
		return err
	}
	r.setStream(stream, paneIDs(snap.Panes))

	// 2. Snapshot again now that the stream is live, and reconcile. Anything
	//    that changed before or during step 1 shows up here; anything after
	//    arrives as an event. No window is unobserved.
	live, err := r.src.Snapshot(ctx)
	if err != nil {
		return err
	}
	r.publishSnapshot(live)
	if !sameSet(paneIDs(live.Panes), r.currentPanes()) {
		r.requestRebalance()
	}

	defer func() {
		if s := r.takeStream(); s != nil {
			_ = s.Close()
		}
	}()

	reconcile := time.NewTicker(r.opts.ReconcileInterval)
	defer reconcile.Stop()

	for {
		// Re-read the stream each iteration so a rebalance performed below is
		// picked up by the next select.
		cur := r.getStream()
		if cur == nil {
			return errors.New("subscription stream missing after rebalance")
		}
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-cur.Err():
			if !ok || err == nil {
				return errors.New("event stream closed")
			}
			return err
		case <-reconcile.C:
			if err := r.reconcileOnce(ctx); err != nil {
				return err
			}
		case <-r.rebalCh:
			if err := r.rebalance(ctx); err != nil {
				return err
			}
		case ev, ok := <-cur.Events():
			if !ok {
				return streamEnded(cur)
			}
			r.handleEvent(ctx, ev)
		}
	}
}

// streamEnded explains why a stream stopped: the terminal error if the reader
// recorded one, otherwise a plain closure. Both are non-nil, because either way
// the subscription is gone and the caller must reconnect.
func streamEnded(s *herdr.Stream) error {
	select {
	case err, ok := <-s.Err():
		if ok && err != nil {
			return err
		}
	default:
	}
	return errors.New("event stream closed")
}

func (r *Reconciler) handleEvent(ctx context.Context, ev herdr.Event) {
	switch {
	case herdr.StatusChangeEvent(ev.Kind):
		sc, err := herdr.DecodeStatusChange(ev.Data)
		if err != nil {
			r.opts.Logger.Warn("undecodable status change", "err", err)
			return
		}
		r.markSeen(sc.PaneID, sc.AgentStatus)
		if r.h.OnStatusChange != nil {
			r.h.OnStatusChange(*sc, false)
		}
	case herdr.LifecycleEvent(ev.Kind):
		// Lifecycle is a hint: the pane inventory may have changed, so re-read
		// it rather than trusting the payload.
		r.requestRebalance()
	case herdr.EventsLostEvent(ev.Kind):
		// The server told us a subscriber fell behind. Correctness requires a
		// full re-read, not a resumption (documented in 08.5).
		r.opts.Logger.Warn("herdr reported events_lost; re-reading session")
		r.requestRebalance()
	}
}

func (r *Reconciler) requestRebalance() {
	select {
	case r.rebalCh <- struct{}{}:
	default:
	}
}

// reconcileOnce re-reads state and, if the pane set changed, rebalances.
func (r *Reconciler) reconcileOnce(ctx context.Context) error {
	snap, err := r.src.Snapshot(ctx)
	if err != nil {
		return err
	}
	r.publishSnapshot(snap)
	if !sameSet(r.currentPanes(), paneIDs(snap.Panes)) {
		r.requestRebalance()
		return nil
	}
	return nil
}

// rebalance opens a new stream for the updated pane set before retiring the old
// one, so no event can slip through the swap.
func (r *Reconciler) rebalance(ctx context.Context) error {
	snap, err := r.src.Snapshot(ctx)
	if err != nil {
		return err
	}
	next := paneIDs(snap.Panes)

	r.mu.Lock()
	unchanged := sameKeys(r.panes, next)
	old := r.stream
	r.mu.Unlock()

	if unchanged {
		r.publishSnapshot(snap)
		return nil
	}

	newStream, err := r.src.Subscribe(ctx, herdr.AgentStatesSubscriptions(next))
	if err != nil {
		return err
	}
	r.setStream(newStream, next)
	r.opts.Logger.Info("rebalanced herdr subscriptions", "panes", len(next))
	if old != nil {
		// The session loop re-reads r.stream at the top of each iteration, so
		// it is already pointing at newStream by the time this returns.
		_ = old.Close()
	}
	r.publishSnapshot(snap)
	return nil
}

func (r *Reconciler) publishSnapshot(snap *herdr.Snapshot) {
	states := make(map[string]herdr.AgentStatus, len(snap.Panes))
	for _, p := range snap.Panes {
		states[p.PaneID] = p.AgentStatus
	}

	r.mu.Lock()
	prev := r.lastSeen
	r.lastSeen = states
	r.panes = setOf(paneIDs(snap.Panes))
	r.mu.Unlock()

	if r.h.OnSnapshot != nil {
		r.h.OnSnapshot(snap)
	}

	// Emit synthetic status changes for anything the events did not mention.
	// This is the backstop that makes "a missed event cannot hide a blocked
	// agent" true, which spec's edge cases require.
	if r.h.OnStatusChange == nil {
		return
	}
	for _, p := range snap.Panes {
		if before, ok := prev[p.PaneID]; !ok || before != p.AgentStatus {
			r.h.OnStatusChange(herdr.StatusChange{
				PaneID:       p.PaneID,
				WorkspaceID:  p.WorkspaceID,
				Agent:        p.Agent,
				DisplayAgent: p.DisplayAgent,
				Title:        p.Title,
				AgentStatus:  p.AgentStatus,
				StateLabels:  p.StateLabels,
			}, true)
		}
	}
}

func (r *Reconciler) markSeen(paneID string, status herdr.AgentStatus) {
	r.mu.Lock()
	r.lastSeen[paneID] = status
	r.mu.Unlock()
}

func (r *Reconciler) setStream(s *herdr.Stream, panes []string) {
	r.mu.Lock()
	r.stream = s
	r.panes = setOf(panes)
	r.mu.Unlock()
}

func (r *Reconciler) getStream() *herdr.Stream {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stream
}

func (r *Reconciler) takeStream() *herdr.Stream {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stream
	r.stream = nil
	return s
}

func (r *Reconciler) currentPanes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.panes))
	for id := range r.panes {
		out = append(out, id)
	}
	return out
}

func paneIDs(panes []herdr.Pane) []string {
	out := make([]string, 0, len(panes))
	for _, p := range panes {
		out = append(out, p.PaneID)
	}
	return out
}

func setOf(ids []string) map[string]struct{} {
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func sameSet(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	return sameKeys(setOf(a), b)
}

func sameKeys(seen map[string]struct{}, b []string) bool {
	if len(seen) != len(b) {
		return false
	}
	for _, id := range b {
		if _, ok := seen[id]; !ok {
			return false
		}
	}
	return true
}
