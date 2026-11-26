package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/th2zz/perch/bridge/internal/actions"
	"github.com/th2zz/perch/bridge/internal/testherdr"
)

// A saved machine that answers like the real thing.
//
// The fixture is the machine list captured from herdr 0.9.1 plus a session with
// one blocked codex pane; the shim (internal/testherdr) serves them and records
// every invocation, so these tests can assert what the bridge *ran*, not only what
// it returned.
func fixtureDir(t *testing.T, fail string) string {
	t.Helper()
	dir := t.TempDir()
	must(t, dir, "machines.json", `[
  {"id":"aaa","label":"workbox","target":"workbox","session":"workbox","enabled":true,"selected":false},
  {"id":"bbb","label":"old-mac","target":"old-mac","session":"default","enabled":false,"selected":false}
]`)
	must(t, dir, "snapshot-workbox.json", `{"id":"cli:api:snapshot","result":{"type":"session_snapshot","snapshot":{
  "version":"0.9.1","protocol":22,
  "workspaces":[{"workspace_id":"w1","number":1,"label":"perch","focused":true,"pane_count":1,"tab_count":1,"active_tab_id":"w1:t1","agent_status":"blocked"}],
  "tabs":[{"tab_id":"w1:t1","workspace_id":"w1","number":1,"label":"1","focused":true,"pane_count":1,"agent_status":"blocked"}],
  "panes":[{"pane_id":"w1:p1","terminal_id":"term_x","workspace_id":"w1","tab_id":"w1:t1","focused":true,"agent":"codex","agent_status":"blocked","revision":7}],
  "agents":[]}}}`)
	must(t, dir, "explain-workbox.txt",
		"agent: codex\nstate: blocked\nmanifest: codex\nrule: live_strong_blocker (evidence: footer)\n")
	must(t, dir, "panetext-workbox.txt", "Do you want to proceed?\n❯ 1. Yes\n  2. No\n")
	if fail != "" {
		must(t, dir, "fail-"+fail, "")
	}
	testherdr.Install(t, dir)
	return dir
}

func must(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
}

func newTestCore(t *testing.T) *Core {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	c, err := New(Options{DisableSubscriptions: true, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	return c
}

func callsContaining(dir, needle string) []string {
	var out []string
	for _, call := range testherdr.Calls(dir) {
		if strings.Contains(call, needle) {
			out = append(out, call)
		}
	}
	return out
}

// TestAnotherComputerBecomesACard: the whole point of FR-016 is that a blocked
// agent on the user's *other* machine reaches the phone the same way a local one
// does, marked with the computer it came from.
func TestAnotherComputerBecomesACard(t *testing.T) {
	dir := fixtureDir(t, "")
	c := newTestCore(t)
	ctx := context.Background()

	c.RefreshRemoteHosts(ctx)
	if _, err := c.RefreshOnce(ctx); err != nil {
		t.Fatalf("local read: %v", err)
	}

	pending := c.Queue().List(false)
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1: %+v", len(pending), pending)
	}
	d := pending[0]
	if d.HostID != "workbox" {
		t.Errorf("host = %q, want workbox: a card without its computer is unusable", d.HostID)
	}
	if d.WorkspaceLabel != "perch" || d.AgentKind != "codex" {
		t.Errorf("decision = %+v", d)
	}
	if !d.Approvable {
		t.Errorf("the mappable dialog on the other machine is not approvable: %+v", d)
	}

	m := c.Mirror()
	var hosts []string
	for _, h := range m.Hosts {
		hosts = append(hosts, h.HostID+":"+h.Status)
	}
	if len(m.Hosts) != 3 {
		t.Fatalf("hosts = %v, want local + two saved machines", hosts)
	}
	if m.Hosts[1].HostID != "workbox" || m.Hosts[1].Status != "reachable" {
		t.Errorf("host row = %+v", m.Hosts[1])
	}
	// A disabled machine is listed but never read: herdr's own switch is the user's
	// statement about whether to use that computer.
	if m.Hosts[2].HostID != "old-mac" || m.Hosts[2].Status != "disabled" {
		t.Errorf("host row = %+v", m.Hosts[2])
	}
	for _, p := range m.Panes {
		if p.PaneID == "w1:p1" && p.HostID != "workbox" {
			t.Errorf("pane host = %q, want workbox", p.HostID)
		}
	}
	if len(callsContaining(dir, "--machine old-mac")) != 0 {
		t.Errorf("a disabled machine was read anyway: %v", callsContaining(dir, "--machine old-mac"))
	}
}

// TestApprovingOnAnotherComputerRunsThere: the action has to reach the machine the
// card came from, and it has to be the equivalent command line the app promises.
func TestApprovingOnAnotherComputerRunsThere(t *testing.T) {
	dir := fixtureDir(t, "")
	c := newTestCore(t)
	ctx := context.Background()

	c.RefreshRemoteHosts(ctx)
	if _, err := c.RefreshOnce(ctx); err != nil {
		t.Fatalf("local read: %v", err)
	}
	pending := c.Queue().List(false)
	if len(pending) != 1 {
		t.Fatalf("no card to approve: %+v", pending)
	}

	decided, err := c.Decide(ctx, actions.Request{
		DecisionID: pending[0].ID,
		Kind:       actions.KindApprove,
		Target:     pending[0].PaneID,
		Host:       "workbox",
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	want := []string{"herdr", "agent", "send-keys", "w1:p1", "enter"}
	if strings.Join(decided.Command, " ") != strings.Join(want, " ") {
		t.Errorf("equivalent command = %v, want %v", decided.Command, want)
	}
	calls := callsContaining(dir, "agent send-keys")
	if len(calls) != 1 {
		t.Fatalf("remote approvals = %v, want exactly one", calls)
	}
	if !strings.HasPrefix(calls[0], "--machine workbox ") {
		t.Errorf("the approval was not addressed to the machine: %q", calls[0])
	}
	if strings.Contains(calls[0], "local") {
		t.Errorf("the approval leaked to another machine: %q", calls[0])
	}
}

// TestAnAsleepComputerDoesNotBreakTheRest: a machine that cannot be reached has to
// be a fact about that machine — a reason, a next step, and no effect on the view
// of the computer the user is actually working on.
func TestAnAsleepComputerDoesNotBreakTheRest(t *testing.T) {
	fixtureDir(t, "workbox")
	c := newTestCore(t)
	ctx := context.Background()

	c.RefreshRemoteHosts(ctx)
	if _, err := c.RefreshOnce(ctx); err != nil {
		t.Fatalf("local read: %v", err)
	}

	m := c.Mirror()
	if len(m.Hosts) != 3 {
		t.Fatalf("hosts = %d, want 3", len(m.Hosts))
	}
	remote := m.Hosts[1]
	if remote.Status != "unreachable" {
		t.Errorf("status = %q, want unreachable", remote.Status)
	}
	if remote.Reason == "" || remote.Fix == "" {
		t.Errorf("a failure with no reason or no next step (FR-015): %+v", remote)
	}
	if local := m.Hosts[0]; local.HostID != "local" || local.Status != "reachable" {
		t.Errorf("the local machine was affected by a remote failure: %+v", local)
	}
	if len(c.Queue().List(false)) != 0 {
		t.Errorf("an unreadable machine produced cards: %+v", c.Queue().List(false))
	}
}

// TestAnUnknownHostIsRefused: acting on a machine that is not saved is a refusal,
// never a fallback to the local machine. This is the one mistake FR-016 must not
// make, so it is checked at the write path rather than only in the UI.
func TestAnUnknownHostIsRefused(t *testing.T) {
	dir := fixtureDir(t, "")
	c := newTestCore(t)

	_, err := c.Decide(context.Background(), actions.Request{
		DecisionID: "",
		Kind:       actions.KindPrompt,
		Target:     "w1:p1",
		Text:       "hello",
		Host:       "no-such-computer",
	})
	if err == nil {
		t.Fatal("a write aimed at an unknown machine was accepted")
	}
	if !strings.Contains(err.Error(), "no-such-computer") {
		t.Errorf("the refusal does not name the machine: %v", err)
	}
	if calls := callsContaining(dir, "send-text"); len(calls) != 0 {
		t.Errorf("a refused write still ran something: %v", calls)
	}
}

func TestEscapeHatchReadsAnotherComputer(t *testing.T) {
	fixtureDir(t, "")
	c := newTestCore(t)
	ctx := context.Background()

	c.RefreshRemoteHosts(ctx)
	view, err := c.Session(ctx, "workbox", "w1:p1", 20)
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if view.HostID != "workbox" || view.HostLabel != "workbox" {
		t.Errorf("the view does not say which computer it is from: %+v", view)
	}
	if view.AgentKind != "codex" || !view.Approvable {
		t.Errorf("the remote session lost its capabilities: %+v", view)
	}
	if !strings.Contains(view.Text, "Do you want to proceed?") {
		t.Errorf("the remote session has no text: %q", view.Text)
	}

	// And a key from the bar goes to that machine.
	if _, err := c.PressKeys(ctx, "workbox", "w1:p1", []string{"esc"}); err != nil {
		t.Fatalf("PressKeys: %v", err)
	}
}
