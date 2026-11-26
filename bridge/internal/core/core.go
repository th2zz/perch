// Package core wires herdr, the subscription reconciler, the attention queue
// and the action mapper into the single object the HTTP server and the CLI
// both talk to.
package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/th2zz/perch/bridge/internal/actions"
	"github.com/th2zz/perch/bridge/internal/discover"
	"github.com/th2zz/perch/bridge/internal/herdr"
	"github.com/th2zz/perch/bridge/internal/latency"
	"github.com/th2zz/perch/bridge/internal/queue"
	"github.com/th2zz/perch/bridge/internal/state"
	"github.com/th2zz/perch/bridge/internal/subscribe"
)

// Options configures the core.
type Options struct {
	HostID    string
	HostLabel string
	Logger    *slog.Logger
	// ReconcileInterval is passed through to the reconciler.
	ReconcileInterval time.Duration
	// SummaryLines is how much pane output is read to describe a request.
	SummaryLines int
	// DisableSubscriptions makes the core poll-free and read-only, which is
	// what `doctor` and one-shot CLI commands want.
	DisableSubscriptions bool
}

// Core is the assembled bridge runtime.
type Core struct {
	opts     Options
	log      *slog.Logger
	store    *state.Store
	api      *herdr.Client
	cli      *herdr.CLI
	q        *queue.Queue
	m        *actions.Mapper
	sessions *sessionMap
	// lat measures the promise the whole design rests on: how long a decision
	// takes to reach the phone.
	lat *latency.Recorder

	mu       sync.RWMutex
	mirror   *discover.Mirror
	lastSync time.Time
	labels   map[string]string
	resyncs  int

	watchers   map[int]chan Event
	watcherSeq int

	// enriching counts the background reads that follow a new request. They exist because reading a
	// pane is slower than emitting an event, and a notification must not wait on it — but they also
	// *write* (the queue is cached to disk), so a caller that is shutting down has to be able to wait
	// for them. Two things needed this: the tests, where a write landing after the end of a test
	// tripped over the temporary directory being removed (the "directory not empty" flake that sat
	// in the release checklist), and the bridge itself, which should finish a write it started
	// rather than be killed mid-file.
	enriching sync.WaitGroup

	subOK      atomic.Bool
	lastClient atomic.Int64 // unix nanos
	// daemon is true when this process is the long-lived bridge rather than a
	// one-shot command. The check-up needs to tell the difference: run from a
	// shell, a one-shot IS a process, and reporting that as "the bridge is
	// running" tells the user everything is fine while nothing is listening.
	daemon atomic.Bool

	lastPushErr string
	lastPushAt  time.Time

	// The host list comes from `herdr machine list`, which is a process spawn.
	// Building a mirror happens on every reconcile and every snapshot request, so
	// asking every time meant one subprocess per event — churn on the user's machine
	// that nothing on screen would explain. The list changes when the user adds a
	// machine, which is rare, so it is held for a while.
	machinesMu  sync.Mutex
	machines    []herdr.Machine
	machinesAt  time.Time
	machinesTTL time.Duration

	// Other machines' last known state, and how that read went (hosts.go).
	remoteMu sync.Mutex
	remote   map[string]*remoteHost

	// requests tallies how much the phone is asking for (requests.go), and
	// clientWritten bounds how often that reaches disk.
	requests      requestTally
	clientMu      sync.Mutex
	clientWritten int64

	// enrichSlots bounds how many requests are described at once. Each description
	// spawns the herdr CLI, and the moment a bridge starts with many agents already
	// waiting is exactly when `go func` per request would spawn them all at once.
	enrichSlots chan struct{}
}

// Event is a server-sent notification for connected clients.
type Event struct {
	// Type is one of the Event* constants in events.go. It is not a literal here
	// so that the set stays enumerable and testable against the app's list.
	Type     string            `json:"type"`
	Decision *queue.Decision   `json:"decision,omitempty"`
	Mirror   *discover.Mirror  `json:"mirror,omitempty"`
	Reason   string            `json:"reason,omitempty"`
	Extra    map[string]string `json:"extra,omitempty"`
}

// New assembles the core. It does not talk to herdr yet; call Run for that.
func New(opts Options) (*Core, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.HostID == "" {
		opts.HostID = discover.LocalHostID
	}
	if opts.HostLabel == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "this computer"
		}
		opts.HostLabel = host
	}
	if opts.SummaryLines <= 0 {
		opts.SummaryLines = 12
	}
	store, err := state.Open()
	if err != nil {
		return nil, err
	}
	return &Core{
		opts:        opts,
		log:         opts.Logger,
		store:       store,
		api:         herdr.NewClient(""),
		cli:         herdr.NewCLI(),
		q:           queue.New(store, queue.Options{}),
		m:           actions.NewMapper(),
		sessions:    loadSessionMap(store),
		lat:         latency.NewRecorder(store.Path(state.FileLatency)),
		labels:      map[string]string{},
		watchers:    map[int]chan Event{},
		machinesTTL: 5 * time.Minute,
		enrichSlots: make(chan struct{}, 4),
	}, nil
}

// Store exposes the state directory (used by doctor, pairing and logs).
func (c *Core) Store() *state.Store { return c.store }

// Logger is the core's logger; subcommands share it so output stays consistent.
func (c *Core) Logger() *slog.Logger { return c.log }

// Close waits for the background work this core started, so a caller can shut down without leaving
// a write in flight. It does not stop the reconciler: that one lives as long as the context it was
// given, which is the caller's own business.
func (c *Core) Close() {
	c.enriching.Wait()
}

// HostLabel names this machine in the app.
func (c *Core) HostLabel() string { return c.opts.HostLabel }

// NotePushFailure records the last push error so the check-up can report it
// instead of leaving the user with silently missing notifications.
func (c *Core) NotePushFailure(err error) {
	c.mu.Lock()
	c.lastPushErr = err.Error()
	c.lastPushAt = time.Now()
	c.mu.Unlock()
}

// LastPushFailure returns the most recent push error, if any, and when.
func (c *Core) LastPushFailure() (string, time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastPushErr, c.lastPushAt
}

// SubscriptionHealthy reports whether the event stream is currently up. The
// health check uses it, and `ensure-running` uses it to decide whether the
// daemon is actually doing its job.
func (c *Core) SubscriptionHealthy() bool { return c.subOK.Load() }

// MarkRunningAsDaemon tells the core that this process is the supervised bridge.
func (c *Core) MarkRunningAsDaemon() { c.daemon.Store(true) }

// DaemonProcess returns the pid of the bridge that is actually serving, and
// whether there is one. It answers "is something listening on my behalf", which is
// a different question from "did this program start".
func (c *Core) DaemonProcess() (int, bool) {
	if c.daemon.Load() {
		return os.Getpid(), true
	}
	return c.store.RunningPID()
}

// MarkClientSeen records that the phone reached us. The timestamp is what the
// "phone reachability" check reports.
func (c *Core) MarkClientSeen() { c.lastClient.Store(time.Now().UnixNano()) }

// LastClientSeen returns when the app last made a request, or the zero time.
func (c *Core) LastClientSeen() time.Time {
	ns := c.lastClient.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// Queue exposes the attention queue.
func (c *Core) Queue() *queue.Queue { return c.q }

// NoteNotificationShown records the hop the phone is responsible for, as the
// phone measured it. It returns false when the decision is unknown, which is the
// ordinary outcome for a phone that reports after the bridge has restarted.
func (c *Core) NoteNotificationShown(decisionID string, shownMillis int64) bool {
	return c.lat.Shown(decisionID, shownMillis)
}

// Latency returns what has been measured, plus the most recent samples.
func (c *Core) Latency() (latency.Stats, []latency.Measurement) {
	return c.lat.Stats(), c.lat.Recent(10)
}

// NoteColdStart records how long the phone took to show its first screen.
func (c *Core) NoteColdStart(millis int64) { c.lat.AddStartup(millis) }

// ColdStart returns the startup figures the phone has reported.
func (c *Core) ColdStart() latency.Stats { return c.lat.StartupStats() }

// PressKeys sends keys from the escape hatch's key bar.
//
// This is the one write that is not one of the four decision classes, and it is
// deliberately the narrowest thing here: a fixed vocabulary (actions.TerminalKeys),
// a bound on how many at once, and no route to it from a decision card. See
// actions/pane_keys.go for why this exists at all — the short version is that
// FR-017 requires the user to be able to write to the session, and 37 of the 61
// blocked dialogs in the installed manifests are ones the decision path refuses.
func (c *Core) PressKeys(ctx context.Context, hostID, paneID string, keys []string) ([]string, error) {
	if err := actions.ValidatePaneKeys(paneID, keys); err != nil {
		return nil, err
	}
	machine, err := c.hostMachine(ctx, hostID)
	if err != nil {
		return nil, err
	}
	argv := herdr.SendKeysCommand(paneID, keys)
	equivalent := herdr.WithProgram(argv)
	if machine != "" {
		if err := c.cli.RunMachine(ctx, machine, argv...); err != nil {
			return nil, fmt.Errorf("herdr refused the key on %s: %w", machine, err)
		}
		return equivalent, nil
	}
	if err := c.api.SendKeysToPane(ctx, paneID, keys); err != nil {
		return nil, fmt.Errorf("herdr refused the key: %w", err)
	}
	return equivalent, nil
}

// Client exposes the herdr socket client.
func (c *Core) Client() *herdr.Client { return c.api }

// CLI exposes the herdr CLI wrapper.
func (c *Core) CLI() *herdr.CLI { return c.cli }

// Mirror returns the latest mirror, or nil before the first read.
func (c *Core) Mirror() *discover.Mirror {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mirror
}

// LastSync reports when the mirror was last refreshed.
func (c *Core) LastSync() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastSync
}

// RefreshOnce reads herdr and rebuilds the mirror without subscribing. It is the
// read-only path used by onboarding previews, doctor, one-shot commands — and by the
// app's own snapshot request.
//
// **It announces nothing.** This used to publish a `snapshot` event, which reads as
// "something changed" to every connected phone — and both the app and its background
// service answer that event by asking for a snapshot. So each read published a reason
// to read again: an endless loop, each lap costing a herdr read on the user's computer,
// with nobody touching the phone. What a read may announce is what it *found* (a newly
// discovered request — see ReconcileQueueFrom); the fact that a read happened is not
// news. A client that has just connected is given the current picture directly by the
// stream's greeting instead.
func (c *Core) RefreshOnce(ctx context.Context) (*discover.Mirror, error) {
	snap, err := c.api.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	m := c.buildMirror(ctx, snap)
	return m, nil
}

// Run keeps the mirror and the queue current until ctx is done.
func (c *Core) Run(ctx context.Context) error {
	if c.opts.DisableSubscriptions {
		_, err := c.RefreshOnce(ctx)
		return err
	}

	rec := subscribe.New(c.api, subscribe.Options{
		ReconcileInterval: c.opts.ReconcileInterval,
		Logger:            c.log,
	}, subscribe.Handlers{
		OnSnapshot:     c.onSnapshot,
		OnStatusChange: c.onStatusChange,
		OnResync: func(err error) {
			c.subOK.Store(false)
			c.mu.Lock()
			c.resyncs++
			c.mu.Unlock()
			c.publish(Event{Type: EventResync, Reason: err.Error()})
		},
	})

	// Run the reconciler on its own goroutine so callers can also drive a
	// periodic prune from the same context.
	done := make(chan struct{})
	go func() {
		defer close(done)
		rec.Run(ctx)
	}()

	// Other machines are polled, because herdr offers no way to subscribe to them
	// (see hosts.go). Their reads never touch this goroutine, so a remote host that
	// is asleep cannot delay the local view.
	go c.runRemotePoller(ctx)

	prune := time.NewTicker(time.Minute)
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			<-done
			return nil
		case now := <-prune.C:
			for _, d := range c.q.Prune(now) {
				expired := d
				c.publish(Event{Type: EventResolved, Decision: &expired})
			}
		}
	}
}

func (c *Core) buildMirror(ctx context.Context, snap *herdr.Snapshot) *discover.Mirror {
	now := time.Now()
	m := discover.Build(c.placed(ctx, snap, now), now)

	labels := make(map[string]string, len(snap.Workspaces))
	for _, w := range snap.Workspaces {
		labels[w.WorkspaceID] = w.Label
	}

	c.mu.Lock()
	c.mirror = m
	c.lastSync = m.TakenAt
	c.labels = labels
	c.mu.Unlock()
	return m
}

func (c *Core) onSnapshot(snap *herdr.Snapshot) {
	c.subOK.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m := c.buildMirror(ctx, snap)

	c.mu.RLock()
	labels := c.labels
	c.mu.RUnlock()

	c.reconcileQueue(snap.Panes, labels, m.TakenAt)
	c.publish(Event{Type: EventSnapshot, Mirror: m})
}

// reconcileQueue brings the pending decisions in line with an authoritative read.
//
// It is one function because two callers need exactly this and nothing else: the
// subscription's snapshot, and the read the app pulls on every reconnect. The second
// one used to skip it, which meant a bridge whose event stream was down would answer
// "nothing needs you" while agents waited — the mirror and the queue disagreeing, in
// the direction that costs the user their afternoon.
func (c *Core) reconcileQueue(panes []herdr.Pane, labels map[string]string, at time.Time) {
	result := c.q.Reconcile(panes, c.opts.HostID, labels, at)
	for _, d := range result.Created {
		created := d
		c.log.Info("decision request", "pane", created.PaneID, "agent", created.AgentKind, "rule", created.Rule)
		c.publish(Event{Type: EventDecision, Decision: &created})
		c.enriching.Add(1)
		go func() {
			defer c.enriching.Done()
			c.enrich(&created)
		}()
	}
	// A request that is no longer answerable has to disappear from the phone too.
	// Without this, a snapshot pass would clear it here and leave it on screen
	// indefinitely — the queue would be right and the user would be misled.
	for _, d := range result.Resolved {
		resolved := d
		c.publish(Event{Type: EventResolved, Decision: &resolved})
	}
}

// ReconcileQueueFrom brings the queue in line with a mirror the caller already read.
//
// It exists so a read of the world can update both halves of it without reading herdr
// twice: the app's snapshot request already has the panes in hand.
func (c *Core) ReconcileQueueFrom(m *discover.Mirror) {
	if m == nil {
		return
	}
	panes := make([]herdr.Pane, 0, len(m.Panes))
	for _, placed := range m.Panes {
		if placed.HostID != discover.LocalHostID {
			continue
		}
		panes = append(panes, placed.Pane)
	}
	labels := make(map[string]string, len(m.Workspaces))
	for _, w := range m.Workspaces {
		labels[w.WorkspaceID] = w.Label
	}
	c.reconcileQueue(panes, labels, m.TakenAt)
}

func (c *Core) onStatusChange(sc herdr.StatusChange, synthetic bool) {
	rule, excerpt := c.describe(sc)

	c.mu.RLock()
	label := c.labels[sc.WorkspaceID]
	c.mu.RUnlock()

	ev := c.q.Observe(queue.Observation{
		Change:         sc,
		HostID:         c.opts.HostID,
		WorkspaceLabel: label,
		Rule:           rule,
		PromptText:     excerpt,
		ObservedAt:     time.Now(),
	})
	if ev.Created != nil {
		created := *ev.Created
		c.log.Info("decision request", "pane", created.PaneID, "agent", created.AgentKind,
			"rule", created.Rule, "synthetic", synthetic)
		c.publish(Event{Type: EventDecision, Decision: &created})
		c.enriching.Add(1)
		go func() {
			defer c.enriching.Done()
			c.enrich(&created)
		}()
	}
	if ev.Resolved != nil {
		resolved := *ev.Resolved
		c.publish(Event{Type: EventResolved, Decision: &resolved})
	}
}

// describe asks herdr which detection rule matched and grabs a short excerpt of
// what the agent is showing. Both come from herdr, not from our own parsing of
// terminal bytes.
func (c *Core) describe(sc herdr.StatusChange) (rule, excerpt string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if det, err := c.cli.Explain(ctx, sc.PaneID); err == nil {
		rule = det.Rule
	}
	lines := c.summaryLinesFor(herdr.Str(sc.Agent))
	if read, err := c.api.ReadPane(ctx, sc.PaneID, "recent", lines); err == nil {
		excerpt = firstLines(read.Text, lines)
	}
	return rule, excerpt
}

// summaryLinesFor is how much of a pane it takes to see the dialog, not only the
// input box under it. Most harnesses draw the question just above the prompt, but
// dsh puts an approval block a screenful higher; the default summary window stopped
// at the prompt box and produced a card with no evidence. This is a read budget,
// not a per-agent key mapping: the dialog parser is still the same for everyone.
func (c *Core) summaryLinesFor(agentKind string) int {
	if agentKind == "dsh-tui" {
		return 60
	}
	if agentKind == "codex" {
		return 24
	}
	return c.opts.SummaryLines
}

// enrich fills in the excerpt once, out of band, so a slow read never delays the
// push notification.
func (c *Core) enrich(d *queue.Decision) {
	needsText := d.PromptText == ""
	needsRule := d.Rule == ""
	if !needsText && !needsRule {
		return
	}

	// Bounded, because a bridge that starts while twenty agents are waiting would
	// otherwise spawn twenty `herdr agent explain` processes at once.
	c.enrichSlots <- struct{}{}
	defer func() { <-c.enrichSlots }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	excerpt := ""
	if needsText {
		lines := c.summaryLinesFor(d.AgentKind)
		if read, err := c.api.ReadPane(ctx, d.PaneID, "recent", lines); err == nil {
			excerpt = firstLines(read.Text, lines)
		}
	}

	// The rule, not just the excerpt. It is what makes approve/reject knowable, and a
	// request discovered by a *read* carries none: the event path gets it for free,
	// and nothing used to go and ask for the other path. The result was a card that
	// said "Perch cannot tell what this dialog is asking" — permanently, because the
	// agent was already blocked and no later event was coming to correct it.
	rule := ""
	if needsRule {
		if det, err := c.cli.Explain(ctx, d.PaneID); err == nil {
			rule = det.Rule
		}
	}
	if rule == "" && excerpt == "" {
		return
	}

	if rule != "" {
		// The same transition the event path uses: "same request, more facts". The
		// decision keeps its identity, so a tap that is already in flight still lands.
		c.q.Observe(queue.Observation{
			Change: herdr.StatusChange{
				PaneID:      d.PaneID,
				WorkspaceID: d.WorkspaceID,
				AgentStatus: herdr.StatusBlocked,
			},
			HostID:        d.HostID,
			Rule:          rule,
			PromptText:    excerpt,
			ObservedAt:    time.Now(),
			Authoritative: true,
		})
		// And the phone is told, because it is looking at the older version of this
		// card — the one without buttons.
		if updated, ok := c.q.Get(d.ID); ok {
			c.publish(Event{Type: EventDecision, Decision: updated})
		}
		return
	}
	c.q.SetPromptText(d.ID, excerpt)
}

func firstLines(text string, n int) string {
	lines := 0
	start := len(text)
	for i := len(text) - 1; i >= 0; i-- {
		if text[i] == '\n' {
			lines++
			if lines > n {
				start = i + 1
				break
			}
		}
	}
	if lines <= n {
		start = 0
	}
	out := text[start:]
	// Trailing whitespace and blank lines add nothing on a card.
	for len(out) > 0 && (out[len(out)-1] == '\n' || out[len(out)-1] == ' ' || out[len(out)-1] == '\t') {
		out = out[:len(out)-1]
	}
	// Keep the payload small: this crosses the phone's network, not ours.
	const max = 1200
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

// ---------------------------------------------------------------- decisions

// DecideResult is what the app gets back from a decision submission.
type DecideResult struct {
	Decision   queue.Decision `json:"decision"`
	Command    []string       `json:"equivalent_command"`
	ValueSaved int            `json:"seconds_saved"`
}

// Decide validates, maps and executes one decision.
//
// Order matters: the herdr command runs first, and only a success marks the
// card handled. A failure leaves the request actionable with the reason
// attached, which is what spec FR-015 asks for.
func (c *Core) Decide(ctx context.Context, req actions.Request) (DecideResult, error) {
	// Structural validation comes before any lookup: a kind outside the four
	// classes is invalid regardless of whether the request id exists, and the
	// app should be told that rather than "unknown decision".
	if err := req.Validate(); err != nil {
		return DecideResult{}, err
	}
	if strings.TrimSpace(req.DecisionID) == "" {
		return c.deliverText(ctx, req)
	}
	// The claim comes before anything that touches the computer. Two callers can
	// arrive for one request — the notification action and the card in the app are
	// separate entry points, and the same tap can reach both — and without this the
	// second one would run the herdr command again. That is a key pressed twice in
	// somebody's dialog, which is the exact harm the four-write rule exists to bound.
	d, err := c.q.Claim(req.DecisionID, time.Now())
	if err != nil {
		return DecideResult{}, err
	}

	// The write is aimed at the *request's* target, while the facts it is mapped from
	// come from the *decision's* pane. Those are two sources for one address, and a
	// client that sent a mismatched pair — a stale card, a bug, or a hand-written
	// curl — would have keys pressed into a pane nobody was looking at. The queue is
	// the authority on which pane a request belongs to (constitution I), so a target
	// that disagrees is refused rather than trusted.
	if target := strings.TrimSpace(req.Target); target != "" && target != d.PaneID {
		c.q.Release(d.ID)
		return DecideResult{}, fmt.Errorf(
			"%w: it is for %s, not %s", actions.ErrTargetMismatch, d.PaneID, target)
	}

	machine, err := c.hostMachine(ctx, d.HostID)
	if err != nil {
		c.q.Release(d.ID)
		return DecideResult{}, err
	}
	obs := c.observeAt(ctx, machine, d.PaneID, d.AgentKind)
	// The queue's excerpt may be older than the dialog in front of the user (they
	// can move the cursor on the computer). The mapping depends on which option is
	// selected, so read the current screen rather than trusting a stale card. This
	// is for every harness, not only Claude: codex and dsh report rules that do not
	// describe their visible approval menu, and the text is the evidence that does.
	obs.PromptText = d.PromptText
	if req.Kind == actions.KindApprove || req.Kind == actions.KindReject {
		if live := c.dialogTextAt(ctx, machine, d.PaneID); live != "" {
			obs.PromptText = live
		}
	}
	cmd, err := c.m.Map(req, obs)
	if err != nil {
		if errors.Is(err, actions.ErrNoDeterministicKeys) {
			// Remember why, so the card can switch to the escape hatch.
			c.q.MarkUnmappable(d.ID, err.Error())
		}
		// A refusal is not an attempt: the user may act again (another way, or after
		// the dialog changes), so the claim goes back.
		c.q.Release(d.ID)
		return DecideResult{}, err
	}
	if err := c.execute(ctx, machine, cmd); err != nil {
		// The command did not run, so the card stays actionable and the claim is
		// released: a laptop that was asleep must not cost the user the tap.
		c.q.Release(d.ID)
		if machine != "" {
			return DecideResult{}, fmt.Errorf("herdr refused the action on %s: %w", machine, err)
		}
		return DecideResult{}, fmt.Errorf("herdr refused the action: %w", err)
	}

	handled, _ := c.q.MarkHandledLocally(d.ID, time.Now())
	if handled == nil {
		// The command ran but the request is no longer claimable — a reconcile pass
		// closed it while the command was in flight. Nothing is broken, but the
		// caller must not be handed a nil dereference, which is what this used to do.
		c.log.Warn("a decision was answered but had already been closed", "id", d.ID)
		return DecideResult{}, fmt.Errorf("%w: this request is already %s", queue.ErrNotPending, d.ID)
	}
	c.q.RecordValue(queue.ValueRecord{
		DecisionID: d.ID,
		DecidedAt:  time.Now(),
		Kind:       req.Kind,
		OnDevice:   true,
	})
	resolved := *handled
	c.publish(Event{Type: EventResolved, Decision: &resolved})

	return DecideResult{
		Decision:   resolved,
		Command:    cmd.CLI,
		ValueSaved: queue.BaselineSeconds,
	}, nil
}

// deliverText is the escape hatch's write half: the user typed something into a
// session and wants it to arrive.
//
// It shares the mapper and the executor with the decision path, so the four write
// classes, the "never guess" rule and the observation step are all the same code.
// The only difference is bookkeeping: there is no queue entry to close, because
// there was no request.
func (c *Core) deliverText(ctx context.Context, req actions.Request) (DecideResult, error) {
	if err := req.ValidateWithoutDecision(); err != nil {
		return DecideResult{}, err
	}

	machine, err := c.hostMachine(ctx, req.Host)
	if err != nil {
		return DecideResult{}, err
	}
	obs := c.observePane(ctx, machine, req.Target)
	if reason, fix, required, err := c.setupRequiredAt(
		ctx, machine, req.Target, obs.AgentKind,
	); required {
		return DecideResult{}, &HarnessSetupError{
			Kind: obs.AgentKind, Reason: reason, Fix: fix,
		}
	} else if err != nil {
		return DecideResult{}, fmt.Errorf(
			"could not check the session before sending text: %w", err,
		)
	}

	cmd, err := c.m.Map(req, obs)
	if err != nil {
		return DecideResult{}, err
	}
	if err := c.execute(ctx, machine, cmd); err != nil {
		if machine != "" {
			return DecideResult{}, fmt.Errorf("herdr refused the delivery on %s: %w", machine, err)
		}
		return DecideResult{}, fmt.Errorf("herdr refused the delivery: %w", err)
	}
	return DecideResult{Command: cmd.CLI}, nil
}

// observeAt gathers the facts the mapper needs about one pane, on this machine or
// another. Remote hosts answer with the same shapes over a different transport.
func (c *Core) observeAt(ctx context.Context, machine, paneID, knownAgent string) actions.Observation {
	obs := actions.Observation{
		AgentKind:       knownAgent,
		RecognisedAgent: knownAgent != "",
		AgentStatus:     herdr.StatusBlocked,
	}
	if machine == "" {
		if det, err := c.cli.Explain(ctx, paneID); err == nil {
			obs.Rule = det.Rule
			if det.State != "" {
				obs.AgentStatus = det.State
			}
			if det.Agent != "" {
				obs.AgentKind = det.Agent
				obs.RecognisedAgent = true
			}
		} else if pane, err := c.api.GetPane(ctx, paneID); err == nil {
			if ae := herdr.Str(pane.Agent); ae != "" {
				obs.AgentKind = ae
				obs.RecognisedAgent = true
			}
			obs.AgentStatus = pane.AgentStatus
		}
		return obs
	}

	if det, err := c.cli.ExplainAt(ctx, machine, paneID); err == nil {
		obs.Rule = det.Rule
		if det.State != "" {
			obs.AgentStatus = det.State
		}
		if det.Agent != "" {
			obs.AgentKind = det.Agent
			obs.RecognisedAgent = true
		}
		return obs
	}
	// No CLI answer: fall back to the last inventory we polled from that machine.
	// It may be up to one poll old, and that is stated rather than hidden — the
	// action itself still runs against the live machine.
	if pane, ok := c.remotePane(machine, paneID); ok {
		if kind := herdr.Str(pane.Agent); kind != "" {
			obs.AgentKind = kind
			obs.RecognisedAgent = true
		}
		obs.AgentStatus = pane.AgentStatus
	}
	return obs
}

// dialogTextAt reads the bottom of a pane for the one mapping that depends on a
// selected option rather than only on herdr's rule. It is best effort: an empty
// answer leaves the decision's own excerpt in place, and the mapper refuses if the
// question is recognisable but its cursor is not.
func (c *Core) dialogTextAt(ctx context.Context, machine, paneID string) string {
	const lines = 60
	if machine == "" {
		read, err := c.api.ReadPane(ctx, paneID, "recent", lines)
		if err != nil {
			return ""
		}
		return firstLines(read.Text, lines)
	}
	text, err := c.cli.ReadPaneTextAt(ctx, machine, paneID, lines)
	if err != nil {
		return ""
	}
	return firstLines(text, lines)
}

// observePane inspects a pane the user is looking at, for the escape hatch's text
// delivery: there is no queued request to describe, so the pane is the subject.
func (c *Core) observePane(ctx context.Context, machine, paneID string) actions.Observation {
	if machine == "" {
		obs := actions.Observation{AgentStatus: herdr.StatusUnknown}
		if pane, err := c.api.GetPane(ctx, paneID); err == nil {
			if kind := herdr.Str(pane.Agent); kind != "" {
				obs.AgentKind = kind
				obs.RecognisedAgent = true
				obs.Rule = c.ruleFor(ctx, paneID, kind)
				obs.AgentStatus = pane.AgentStatus
			}
		}
		return obs
	}
	return c.observeAt(ctx, machine, paneID, "")
}

// execute runs a mapped command wherever its target lives.
func (c *Core) execute(ctx context.Context, machine string, cmd actions.Command) error {
	if machine == "" {
		return c.m.Execute(ctx, c.api, cmd)
	}
	return c.m.ExecuteRemote(ctx, c.cli, machine, cmd)
}

// remotePane looks a pane up in the last inventory polled from a machine.
func (c *Core) remotePane(machine, paneID string) (herdr.Pane, bool) {
	c.remoteMu.Lock()
	defer c.remoteMu.Unlock()
	rh := c.remote[machine]
	if rh == nil || rh.snap == nil {
		return herdr.Pane{}, false
	}
	for _, p := range rh.snap.Panes {
		if p.PaneID == paneID {
			return p, true
		}
	}
	return herdr.Pane{}, false
}

// ---------------------------------------------------------------- watchers

// Watch registers a client for server-sent events.
func (c *Core) Watch() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	c.mu.Lock()
	c.watcherSeq++
	id := c.watcherSeq
	c.watchers[id] = ch
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		if cur, ok := c.watchers[id]; ok {
			delete(c.watchers, id)
			close(cur)
		}
		c.mu.Unlock()
	}
}

func (c *Core) publish(ev Event) {
	// Stamp the first hop the moment the frame is about to leave, which is the
	// only instant that means anything: anything earlier would flatter the
	// product by hiding the queue's own latency.
	if ev.Type == EventDecision && ev.Decision != nil {
		if created := ev.Decision.CreatedAt; !created.IsZero() {
			c.lat.Queue(ev.Decision.ID, time.Since(created).Milliseconds())
		}
	}

	c.mu.RLock()
	targets := make([]chan Event, 0, len(c.watchers))
	for _, ch := range c.watchers {
		targets = append(targets, ch)
	}
	c.mu.RUnlock()
	for _, ch := range targets {
		select {
		case ch <- ev:
		default:
			// A slow client must not stall the bridge; it will get a fresh
			// snapshot from /v1/snapshot on reconnect.
		}
	}
}
