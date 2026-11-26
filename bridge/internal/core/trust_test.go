package core

import (
	"context"
	"strings"
	"testing"

	"github.com/th2zz/perch/bridge/internal/actions"
	"github.com/th2zz/perch/bridge/internal/testherdr"
)

func TestApprovingHomeTrustSelectsYes(t *testing.T) {
	dir := t.TempDir()
	must(t, dir, "machines.json", `[
  {"id":"aaa","label":"workbox","target":"workbox","session":"workbox","enabled":true,"selected":false}
]`)
	must(t, dir, "snapshot-workbox.json", `{"id":"cli:api:snapshot","result":{"type":"session_snapshot","snapshot":{
  "version":"0.9.1","protocol":22,
  "workspaces":[{"workspace_id":"w1","number":1,"label":"perch","focused":true,"pane_count":1,"tab_count":1,"active_tab_id":"w1:t1","agent_status":"blocked"}],
  "tabs":[{"tab_id":"w1:t1","workspace_id":"w1","number":1,"label":"1","focused":true,"pane_count":1,"agent_status":"blocked"}],
  "panes":[{"pane_id":"w1:p1","terminal_id":"term_x","workspace_id":"w1","tab_id":"w1:t1","focused":true,"agent":"claude","agent_status":"blocked","revision":7}],
  "agents":[]}}}`)
	must(t, dir, "explain-workbox.txt",
		"agent: claude\nstate: blocked\nmanifest: claude\nrule: live_blocked_form (evidence: footer)\n")
	must(t, dir, "panetext-workbox.txt",
		" Quick safety check: Is this a project you created or one you trust?\n\n"+
			" ❯ No, exit\n"+
			"   Yes, I trust this folder\n\n"+
			" Enter to confirm · Esc to cancel\n")
	testherdr.Install(t, dir)
	t.Setenv("HERDR_SOCKET_PATH", startFakeLocalHerdr(t))

	c := newTestCore(t)
	ctx := context.Background()
	c.RefreshRemoteHosts(ctx)
	if _, err := c.RefreshOnce(ctx); err != nil {
		t.Fatalf("local read: %v", err)
	}
	pending := c.Queue().List(false)
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want one card: %+v", len(pending), pending)
	}
	if !pending[0].Approvable {
		t.Fatalf("the trust question must be approvable: %+v", pending[0])
	}

	decided, err := c.Decide(ctx, actions.Request{
		DecisionID: pending[0].ID,
		Kind:       actions.KindApprove,
		Target:     "w1:p1",
		Host:       "workbox",
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	want := []string{"herdr", "agent", "send-keys", "w1:p1", "down", "enter"}
	if got := strings.Join(decided.Command, " "); got != strings.Join(want, " ") {
		t.Errorf("equivalent command = %v, want %v", decided.Command, want)
	}
	calls := callsContaining(dir, "agent send-keys")
	if len(calls) != 1 {
		t.Fatalf("approvals = %v, want exactly one", calls)
	}
	if !strings.Contains(calls[0], "send-keys w1:p1 down enter") {
		t.Errorf("the approval did not move to Yes before Enter: %q", calls[0])
	}
	if !strings.HasPrefix(calls[0], "--machine workbox ") {
		t.Errorf("the approval was not addressed to the machine: %q", calls[0])
	}
}
