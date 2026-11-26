package queue

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
	"github.com/th2zz/perch/bridge/internal/state"
)

// TestTheQueueFileIsBoundedByCountNotOnlyByAge.
//
// An agent that asks for approval in a loop — a test suite retrying, an agent stuck on
// a permission it keeps re-requesting — produces a closed decision per lap, and the
// whole file is rewritten on every change. Age alone is not a bound: at one per second
// that is eighty-six thousand entries inside the day, each write serialising all of
// them. What gets copied to disk is the thing worth bounding, not what is read.
func TestTheQueueFileIsBoundedByCountNotOnlyByAge(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(state.EnvStateDir, dir)
	st, err := state.Open()
	if err != nil {
		t.Fatalf("open state: %v", err)
	}

	q := New(st, Options{})
	now := time.Now()
	// An agent asking over and over: block, answer, dialog closes, block again. Far more
	// closed requests than the bound, all inside the age window.
	for i := 0; i < maxPersistedDecisions+120; i++ {
		at := now.Add(time.Duration(i) * time.Millisecond)
		created := q.Reconcile([]herdr.Pane{blockedPane("wA:p1")}, "local", nil, at)
		if len(created.Created) != 1 {
			t.Fatalf("lap %d created %d requests, want one", i, len(created.Created))
		}
		q.MarkHandledLocally(created.Created[0].ID, at)
		q.Reconcile([]herdr.Pane{{
			PaneID: "wA:p1", WorkspaceID: "w1", AgentStatus: herdr.StatusIdle,
		}}, "local", nil, at.Add(time.Microsecond))
	}

	var onDisk []Decision
	if err := st.ReadJSON(state.FileDecisions, &onDisk); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(onDisk) != maxPersistedDecisions {
		t.Errorf("the file holds %d decisions, want it bounded at %d", len(onDisk), maxPersistedDecisions)
	}
	// And what is kept is the *newest*: an old closed request is history, the last few
	// are what the app may still be showing.
	for i := 1; i < len(onDisk); i++ {
		if onDisk[i-1].CreatedAt.Before(onDisk[i].CreatedAt) {
			t.Fatalf("the kept decisions are not the newest: %v before %v",
				onDisk[i-1].CreatedAt, onDisk[i].CreatedAt)
		}
	}
}

// TestPendingRequestsAreNeverTrimmedAway.
//
// The bound is for history. A pending request is live work, and dropping it from disk
// would change its identity under a tap that is already in flight — so the file may
// exceed the bound rather than lose one.
func TestPendingRequestsAreNeverTrimmedAway(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(state.EnvStateDir, dir)
	st, err := state.Open()
	if err != nil {
		t.Fatalf("open state: %v", err)
	}

	q := New(st, Options{})
	now := time.Now()

	// A hundred waiting agents, each on its own pane. Every reconcile carries the whole
	// inventory, as every real caller does — a partial one says "these other sessions
	// ended", which is the caller's bug and not the queue's.
	waiting := make([]herdr.Pane, 0, 100)
	for i := 0; i < 100; i++ {
		waiting = append(waiting, herdr.Pane{
			PaneID: fmt.Sprintf("wA:p%d", i), WorkspaceID: "wA", AgentStatus: herdr.StatusBlocked,
		})
	}
	q.Reconcile(waiting, "local", nil, now)

	// Plus enough closed history to push past the bound, from an agent that keeps
	// asking on a pane of its own.
	flapping := func(status herdr.AgentStatus) []herdr.Pane {
		return append(append([]herdr.Pane{}, waiting...), herdr.Pane{
			PaneID: "wB:p1", WorkspaceID: "wB", AgentStatus: status,
		})
	}
	for i := 0; i < maxPersistedDecisions; i++ {
		at := now.Add(time.Duration(i) * time.Millisecond)
		created := q.Reconcile(flapping(herdr.StatusBlocked), "local", nil, at)
		if len(created.Created) != 1 {
			t.Fatalf("history lap %d created %d requests", i, len(created.Created))
		}
		q.MarkHandledLocally(created.Created[0].ID, at)
		q.Reconcile(flapping(herdr.StatusIdle), "local", nil, at.Add(time.Microsecond))
	}

	var onDisk []Decision
	if err := st.ReadJSON(state.FileDecisions, &onDisk); err != nil {
		t.Fatalf("read back: %v", err)
	}
	pending := 0
	for _, d := range onDisk {
		if d.State == StatePending {
			pending++
		}
	}
	if pending != 100 {
		t.Errorf("%d pending requests survived, want all 100", pending)
	}

	// The same after a restart: the waiting agents are still there to be acted on.
	reopened := New(st, Options{})
	if got := len(reopened.List(false)); got != 100 {
		t.Errorf("after a restart the queue holds %d pending requests, want 100", got)
	}
	if _, err := os.Stat(st.Path(state.FileDecisions)); err != nil {
		t.Errorf("no queue file was written: %v", err)
	}
}
