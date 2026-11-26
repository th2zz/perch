package subscribe

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

// fakeSource stands in for herdr: it hands out scripted snapshots and streams, and
// remembers the order in which it was asked for them.
//
// The order is the point of the first test below. The reconciler's central claim is
// that there is no window in which a status change goes unnoticed, and that claim is
// only true if it snapshots, subscribes, and *then* snapshots again — a sequence, not
// a set of calls.
type fakeSource struct {
	mu        sync.Mutex
	calls     []string
	snapshots []*herdr.Snapshot
	streams   []*herdr.Stream
	subscribe chan struct{} // signals each Subscribe call
	watched   [][]string    // per Subscribe call, the pane ids watched for status
}

func (f *fakeSource) Snapshot(context.Context) (*herdr.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "snapshot")
	if len(f.snapshots) == 0 {
		return &herdr.Snapshot{}, nil
	}
	next := f.snapshots[0]
	if len(f.snapshots) > 1 {
		f.snapshots = f.snapshots[1:]
	}
	return next, nil
}

func (f *fakeSource) Subscribe(_ context.Context, subs []herdr.Subscription) (*herdr.Stream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "subscribe")
	watched := make([]string, 0, len(subs))
	for _, s := range subs {
		if s.Type == herdr.EventPaneAgentStatusChanged && s.PaneID != "" {
			watched = append(watched, s.PaneID)
		}
	}
	f.watched = append(f.watched, watched)
	var stream *herdr.Stream
	if len(f.streams) == 0 {
		events := make(chan herdr.Event)
		errs := make(chan error)
		stream = herdr.NewStream(events, errs)
	} else {
		stream = f.streams[0]
		if len(f.streams) > 1 {
			f.streams = f.streams[1:]
		}
	}
	if f.subscribe != nil {
		select {
		case f.subscribe <- struct{}{}:
		default:
		}
	}
	return stream, nil
}

func (f *fakeSource) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// watchedPanes returns, per Subscribe call, the pane ids that call asked status
// for — which is how a test sees that a rebalance dropped a gone pane.
func (f *fakeSource) watchedPanes() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.watched))
	copy(out, f.watched)
	return out
}

// scriptedStream builds a stream whose channels the test drives.
func scriptedStream() (*herdr.Stream, chan herdr.Event, chan error) {
	events := make(chan herdr.Event, 8)
	errs := make(chan error, 1)
	return herdr.NewStream(events, errs), events, errs
}

func newTestReconciler(src Source, interval time.Duration, h Handlers) *Reconciler {
	return New(src, Options{
		ReconcileInterval: interval,
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, h)
}

// TestItSnapshotsAroundTheSubscription.
//
// "Snapshot, then subscribe" and "subscribe, then snapshot" both leave a window in
// which a status change is unattributable: the first can miss a change made between
// the two, the second can miss one made before the stream existed. Snapshotting on
// both sides of the subscription is what closes it, and this is the test that keeps
// somebody from "simplifying" one of them away.
func TestItSnapshotsAroundTheSubscription(t *testing.T) {
	first, second := &herdr.Snapshot{Version: "0.9.1"}, &herdr.Snapshot{Version: "0.9.1"}
	src := &fakeSource{snapshots: []*herdr.Snapshot{first, second}}
	stream, _, _ := scriptedStream()
	src.streams = []*herdr.Stream{stream}

	reconciler := newTestReconciler(src, time.Hour, Handlers{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); reconciler.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(src.callLog()) >= 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	got := src.callLog()
	want := []string{"snapshot", "subscribe", "snapshot"}
	if len(got) < 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("calls = %v, want them to start %v", got, want)
	}
}

// TestALostStreamIsReportedAndRetried.
//
// This is the behaviour the app's "reconnecting" line and spec SC-008 rest on: the
// stream ends, the bridge says so (so the phone can say so), and it comes back by
// itself. A reconciler that silently stopped would look exactly like an idle session.
func TestALostStreamIsReportedAndRetried(t *testing.T) {
	src := &fakeSource{}
	first, _, firstErrs := scriptedStream()
	second, _, _ := scriptedStream()
	src.streams = []*herdr.Stream{first, second}
	src.subscribe = make(chan struct{}, 4)

	resyncs := make(chan error, 4)
	reconciler := newTestReconciler(src, time.Hour, Handlers{
		OnResync: func(err error) { resyncs <- err },
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); reconciler.Run(ctx) }()

	// Wait for the first subscription, then take the stream away.
	select {
	case <-src.subscribe:
	case <-time.After(2 * time.Second):
		t.Fatal("the reconciler never subscribed")
	}
	firstErrs <- errors.New("socket hang up")

	select {
	case err := <-resyncs:
		if err == nil {
			t.Error("a lost stream was reported with no reason")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a lost stream was not reported; the phone would never know")
	}

	select {
	case <-src.subscribe:
		// Reconnected, with no help from anybody.
	case <-time.After(3 * time.Second):
		t.Fatal("the reconciler did not reconnect")
	}

	cancel()
	<-done
}

// TestAStatusChangeEventBecomesAStatusChange, and is marked as coming from the
// stream rather than from a snapshot — the flag the queue uses to decide whether a
// duplicate may be suppressed.
func TestAStatusChangeEventBecomesAStatusChange(t *testing.T) {
	src := &fakeSource{}
	stream, events, _ := scriptedStream()
	src.streams = []*herdr.Stream{stream}

	changes := make(chan struct {
		change    herdr.StatusChange
		synthetic bool
	}, 4)
	reconciler := newTestReconciler(src, time.Hour, Handlers{
		OnStatusChange: func(sc herdr.StatusChange, synthetic bool) {
			changes <- struct {
				change    herdr.StatusChange
				synthetic bool
			}{sc, synthetic}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); reconciler.Run(ctx) }()
	defer func() { cancel(); <-done }()

	time.Sleep(100 * time.Millisecond)
	events <- herdr.Event{
		Kind: "pane.agent_status_changed",
		Data: []byte(`{"pane_id":"wX:p1","workspace_id":"wX","agent_status":"blocked"}`),
	}

	select {
	case got := <-changes:
		if got.change.PaneID != "wX:p1" || got.change.AgentStatus != herdr.StatusBlocked {
			t.Errorf("decoded the wrong change: %+v", got.change)
		}
		if got.synthetic {
			t.Error("a change that arrived as an event was reported as synthetic")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a status change event was ignored")
	}
}

// TestThePeriodicReconcileStillHappens.
//
// Lifecycle delivery is a hint, not a guarantee (the schema's delivery enum is
// narrower than what can be submitted), so a slow re-read backstops it. Without this
// test somebody could delete the ticker as "redundant polling" and only find out
// when an agent waited for ever.
func TestThePeriodicReconcileStillHappens(t *testing.T) {
	src := &fakeSource{}
	stream, _, _ := scriptedStream()
	src.streams = []*herdr.Stream{stream}

	snapshots := make(chan struct{}, 8)
	reconciler := newTestReconciler(src, 80*time.Millisecond, Handlers{
		OnSnapshot: func(*herdr.Snapshot) { snapshots <- struct{}{} },
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); reconciler.Run(ctx) }()
	defer func() { cancel(); <-done }()

	// The first two come from the startup sequence; the third can only come from the
	// periodic re-read.
	for i := 0; i < 3; i++ {
		select {
		case <-snapshots:
		case <-time.After(3 * time.Second):
			t.Fatalf("only %d snapshots arrived; the periodic reconcile did not run", i)
		}
	}
}

// TestAnExitedPaneIsDroppedWithoutTheTicker is the regression test for #124.
//
// When someone exits the program inside a pane (types `exit`), herdr removes the pane
// and announces pane_exited — not pane.closed, which only an explicit close emits. The
// reconciler must hear that hint, re-read the session, and rebuild the stream without
// the gone pane. If the exit word is not classified as lifecycle, nothing happens here
// until the periodic reconcile — five minutes of a pane sitting on somebody's phone
// that no longer exists on the host.
func TestAnExitedPaneIsDroppedWithoutTheTicker(t *testing.T) {
	twoPanes := &herdr.Snapshot{Version: "0.9.1", Panes: []herdr.Pane{
		{PaneID: "w1:p1", WorkspaceID: "w1"},
		{PaneID: "w1:p2", WorkspaceID: "w1"},
	}}
	onePane := &herdr.Snapshot{Version: "0.9.1", Panes: []herdr.Pane{
		{PaneID: "w1:p1", WorkspaceID: "w1"},
	}}
	// Two reads for the startup sequence (around the subscription), then the read
	// that the exit must provoke.
	src := &fakeSource{snapshots: []*herdr.Snapshot{twoPanes, twoPanes, onePane}}
	stream, events, _ := scriptedStream()
	src.streams = []*herdr.Stream{stream}

	snapshots := make(chan *herdr.Snapshot, 8)
	reconciler := newTestReconciler(src, time.Hour, Handlers{
		OnSnapshot: func(s *herdr.Snapshot) { snapshots <- s },
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); reconciler.Run(ctx) }()
	defer func() { cancel(); <-done }()

	select {
	case got := <-snapshots:
		if len(got.Panes) != 2 {
			t.Fatalf("the startup snapshot arrived wrong: %+v", got.Panes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the startup snapshot never arrived")
	}

	// What herdr sends when the process inside w1:p2 ends; underscored, the way
	// every delivered kind is spelled.
	events <- herdr.Event{Kind: "pane_exited", Data: []byte(`{"pane_id":"w1:p2","workspace_id":"w1"}`)}

	select {
	case got := <-snapshots:
		if len(got.Panes) != 1 || got.Panes[0].PaneID != "w1:p1" {
			t.Fatalf("the mirror still carries the exited pane: %+v", got.Panes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an exited pane stayed on the mirror; in this test only the exit hint could have cleared it")
	}

	sets := src.watchedPanes()
	if len(sets) < 2 {
		t.Fatalf("opened %d subscriptions, want a rebuild after the exit: %v", len(sets), sets)
	}
	if last := sets[len(sets)-1]; len(last) != 1 || last[0] != "w1:p1" {
		t.Errorf("the rebuilt subscription watches %v, want only [w1:p1]", last)
	}
}
