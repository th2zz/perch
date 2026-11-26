package server

import (
	"net/http"
	"testing"
	"time"
)

// TestAnsweringAReadDoesNotAnnounceAChange.
//
// The app refreshes when it hears "snapshot", and to refresh it asks for /v1/snapshot.
// The phone's background service does the same thing for every event it hears. So if
// answering a read also publishes a change, the two chase each other for ever: the
// phone re-reads the world as fast as a round trip allows, with nobody touching it, and
// every lap costs a herdr read on the user's computer.
//
// No unit test of either half shows that. This one looks at the one place where it
// would be visible — the bridge's own event stream.
func TestAnsweringAReadDoesNotAnnounceAChange(t *testing.T) {
	panes := []map[string]any{{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": true, "agent_status": "idle", "revision": 1,
	}}
	_, socketPath := startFakeHerdr(t, panes...)
	ts, core := newTestServerWithSocket(t, socketPath)

	events, cancel := core.Watch()
	defer cancel()

	// Exactly what the app does when it refreshes.
	resp := do(t, ts, http.MethodGet, "/v1/snapshot", testToken, "")
	_ = resp.Body.Close()

	// Give anything the handler published time to arrive.
	var announced []string
	deadline := time.After(300 * time.Millisecond)
collect:
	for {
		select {
		case ev := <-events:
			announced = append(announced, ev.Type)
		case <-deadline:
			break collect
		}
	}

	for _, kind := range announced {
		if kind == "snapshot" || kind == "decision" || kind == "resolved" {
			t.Errorf("answering a read announced a %q to every connected phone: the app "+
				"refreshes on %q, so it would refresh for ever (saw %v)", kind, kind, announced)
		}
	}
}

// TestAWaitingAgentStillAnnouncesItself is the other half, and the reason the test
// above is not satisfied by announcing nothing at all: a read that *discovers* a
// waiting agent is a change, and the phone has to hear about it without polling.
//
// The distinction the two tests draw together is: a read may announce what it found,
// never the fact that it happened.
func TestAChangeStillAnnouncesItself(t *testing.T) {
	panes := []map[string]any{{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": true, "agent": "codex",
		"agent_status": "blocked", "revision": 2,
	}}
	_, socketPath := startFakeHerdr(t, panes...)
	ts, c := newTestServerWithSocket(t, socketPath)

	events, cancel := c.Watch()
	defer cancel()

	resp := do(t, ts, http.MethodGet, "/v1/snapshot", testToken, "")
	_ = resp.Body.Close()

	select {
	case ev := <-events:
		if ev.Type != "decision" {
			t.Errorf("the first event was %q, want the request itself", ev.Type)
		}
		if ev.Decision == nil || ev.Decision.PaneID != "wX:p1" {
			t.Errorf("the announced request is not the one that is waiting: %+v", ev.Decision)
		}
	case <-time.After(time.Second):
		t.Fatal("a waiting agent was not announced; the phone would only find it by polling")
	}
}
