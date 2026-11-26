package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/th2zz/perch/bridge/internal/testherdr"
)

// TestARequestFoundByAReadIsAsActionableAsOneFoundByAnEvent.
//
// There are two ways a waiting agent reaches the queue: an event (the agent just
// blocked — the common case), or a read (the agent was already waiting when the bridge
// started, or an event was missed — the reconcile path, whose entire purpose is not to
// lose those).
//
// The two must produce the same card. The event carries herdr's detection rule, which
// is what makes approve/reject knowable at all; a read carries no rule, and nothing
// used to go and ask. So a request discovered by a read arrived permanently
// unmappable: the phone showed "Perch cannot press this one for you" for a dialog it
// knew perfectly well how to press, and no later event would fix it, because the agent
// was already blocked and nothing was going to change.
func TestARequestFoundByAReadIsAsActionableAsOneFoundByAnEvent(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": true, "agent": "codex", "display_agent": "codex",
		"agent_status": "blocked", "revision": 5,
	}
	// The detection rule, as `agent explain` reports it. The shim must be installed
	// before the core is built: the bridge resolves its herdr binary once.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "explain-local.txt"),
		[]byte("agent: codex\nstate: blocked\nmanifest: codex\nrule: live_strong_blocker\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	testherdr.Install(t, dir)

	_, socketPath := startFakeHerdr(t, pane)
	ts, core := newTestServerWithSocket(t, socketPath)

	// The app's read: this is how a request that was already waiting arrives.
	resp := do(t, ts, http.MethodGet, "/v1/snapshot", testToken, "")
	_ = resp.Body.Close()

	// Describing a request happens out of band — reading its output and asking herdr
	// which rule matched are both slower than creating the card — so the card becomes
	// complete a moment later. What matters is that it *does*: this waits for the
	// outcome rather than for the absence of a reason that is true at birth.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pending := core.Queue().List(false)
		if len(pending) == 1 && pending[0].Approvable {
			if pending[0].Rule != "live_strong_blocker" {
				t.Errorf("rule = %q, want the one herdr reports", pending[0].Rule)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("a request discovered by a read never became actionable: %+v", core.Queue().List(false))
}
