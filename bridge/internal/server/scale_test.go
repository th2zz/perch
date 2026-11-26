package server

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

// TestTheBridgeKeepsUpWithAFullWorkspace.
//
// The user this product is for has many repositories open at once, which is the whole
// reason they want a queue instead of a terminal. Forty projects with five sessions
// each is an ordinary Tuesday for that person, and it is the shape that would break
// something quadratic — a per-pane subprocess, a per-event full re-read, a payload that
// grows with the square of the workspaces.
//
// The numbers below are a budget, not a benchmark: they are loose enough not to be
// flaky and tight enough that a change which turns "one read" into "one read per pane"
// fails here instead of on somebody's phone.
func TestTheBridgeKeepsUpWithAFullWorkspace(t *testing.T) {
	panes := make([]map[string]any, 0, 200)
	blocked := 20
	for w := 0; w < 40; w++ {
		for p := 0; p < 5; p++ {
			status := "idle"
			if len(panes) < blocked {
				status = "blocked"
			}
			panes = append(panes, map[string]any{
				"pane_id":       fmt.Sprintf("w%02d:p%d", w, p+1),
				"terminal_id":   fmt.Sprintf("term_%02d_%d", w, p+1),
				"workspace_id":  fmt.Sprintf("w%02d", w),
				"tab_id":        fmt.Sprintf("w%02d:t1", w),
				"focused":       p == 0,
				"agent":         "codex",
				"display_agent": fmt.Sprintf("codex-%02d-%d", w, p+1),
				"agent_status":  status,
				"revision":      len(panes) + 1,
			})
		}
	}

	fake, socketPath := startFakeHerdr(t, panes...)
	ts, core := newTestServerWithSocket(t, socketPath)

	start := time.Now()
	resp := do(t, ts, http.MethodGet, "/v1/snapshot", testToken, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	elapsed := time.Since(start)

	// 40 workspaces, 200 panes, in one payload.
	mirror := core.Mirror()
	if mirror == nil {
		t.Fatal("no mirror was built")
	}
	if got := len(mirror.Panes); got != 200 {
		t.Errorf("mirror has %d panes, want 200", got)
	}
	if got := len(mirror.Workspaces); got != 40 {
		t.Errorf("mirror has %d workspaces, want 40", got)
	}

	// Every blocked pane is in the queue: nothing is dropped at scale.
	if got := len(core.Queue().List(false)); got != blocked {
		t.Errorf("queue holds %d requests, want the %d waiting agents", got, blocked)
	}

	t.Logf("200 panes: %v, %d bytes", elapsed, len(body))
	if elapsed > time.Second {
		t.Errorf("a read of 200 panes took %v, which the phone would feel", elapsed)
	}
	if len(body) > 512<<10 {
		t.Errorf("the payload is %d bytes; a phone on a train pays for that", len(body))
	}

	// The subscription set is one request with one entry per pane, not one request per
	// pane: herdr rejects a status subscription that names no pane, and 200 round trips
	// per reconcile would be the actual cost of many projects.
	subs := herdr.AgentStatesSubscriptions(paneIDsOf(panes))
	if len(subs) != 200+4 {
		t.Errorf("subscriptions = %d, want 204 (four lifecycle: created, closed, exited, agent detected; plus one per pane)", len(subs))
	}
	if len(fake.methods()) == 0 {
		t.Error("the fake herdr was never asked anything")
	}
}

// paneIDsOf mirrors what the reconciler does with a snapshot.
func paneIDsOf(panes []map[string]any) []string {
	out := make([]string, 0, len(panes))
	for _, pane := range panes {
		if id, ok := pane["pane_id"].(string); ok {
			out = append(out, id)
		}
	}
	return out
}

var _ = herdr.StatusBlocked
