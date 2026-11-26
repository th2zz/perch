package herdr

import "testing"

// TestParseExplain uses the real output shape observed on this machine:
//
//	agent: codex
//	state: idle
//	manifest: remote:/home/.../codex.toml 2026.10.11.1
//	rule: osc_title_idle (region=osc_title priority=100)
//	evidence: "..."
func TestParseExplain(t *testing.T) {
	text := "agent: codex\n" +
		"state: idle\n" +
		"manifest: remote:/home/user/.local/state/herdr/agent-detection/remote/codex.toml 2026.10.11.1\n" +
		"rule: osc_title_idle (region=osc_title priority=100)\n" +
		"evidence: \"Check build logs and café output | workspace\"\n"

	d := ParseExplain(text)
	if d.Agent != "codex" {
		t.Errorf("agent = %q", d.Agent)
	}
	if d.State != StatusIdle {
		t.Errorf("state = %q", d.State)
	}
	// The rule id must come out clean: the parenthesised detail is metadata, and
	// the key table is keyed on the bare id.
	if d.Rule != "osc_title_idle" {
		t.Errorf("rule = %q, want osc_title_idle", d.Rule)
	}
	if d.RuleDetail != "region=osc_title priority=100" {
		t.Errorf("rule detail = %q", d.RuleDetail)
	}
	// CJK evidence must survive intact; the whole product hinges on this.
	if d.Evidence != "Check build logs and café output | workspace" {
		t.Errorf("evidence = %q", d.Evidence)
	}
}

func TestParseExplainBlockedForm(t *testing.T) {
	text := "agent: claude\n" +
		"state: blocked\n" +
		"manifest: remote:/tmp/claude.toml 2026.09.28.1\n" +
		"rule: bash_permission_prompt (region=whole_recent priority=850)\n" +
		"evidence: \"Do you want to proceed?\"\n"

	d := ParseExplain(text)
	if d.State != StatusBlocked {
		t.Fatalf("state = %q", d.State)
	}
	if !d.State.NeedsDecision() {
		t.Error("a blocked agent must need a decision")
	}
	if d.Rule != "bash_permission_prompt" {
		t.Errorf("rule = %q", d.Rule)
	}
}

// TestUnknownStateIsNotDone pins the edge case that "unknown" must never be
// presented as finished work.
func TestUnknownStateIsNotDone(t *testing.T) {
	if StatusUnknown.NeedsDecision() {
		t.Error("unknown must not be treated as waiting")
	}
	if StatusDone.NeedsDecision() {
		t.Error("done must not be treated as waiting")
	}
	if StatusWorking.NeedsDecision() {
		t.Error("working must not be treated as waiting")
	}
	if !StatusBlocked.NeedsDecision() {
		t.Error("blocked must be treated as waiting")
	}
}

func TestParseStatus(t *testing.T) {
	text := `client:
  version: 0.9.1
  channel: stable
  protocol: 22
  endpoint_protocol_generation: 1

server:
  status: running
  version: 0.9.1
  endpoint_compatible: yes
  private_protocol: 22
  private_protocol_compatible: yes
  socket: /home/user/.config/herdr/herdr.sock

update:
  restart_needed: no
  server_binary_stale: no
`
	st := parseStatus(text)
	if st.ClientVersion != "0.9.1" || !st.ClientProtocolSet || st.ClientProtocol != 22 {
		t.Errorf("client section parsed wrong: %+v", st)
	}
	if st.ServerStatus != "running" || st.Socket != "/home/user/.config/herdr/herdr.sock" {
		t.Errorf("server section parsed wrong: %+v", st)
	}
	if st.EndpointCompatible == nil || !*st.EndpointCompatible {
		t.Errorf("endpoint_compatible parsed wrong: %v", st.EndpointCompatible)
	}
	if st.RestartNeeded == nil || *st.RestartNeeded {
		t.Errorf("restart_needed parsed wrong: %v", st.RestartNeeded)
	}
}

func TestParseProtocol(t *testing.T) {
	if n, ok := ParseProtocol("server:\n  protocol: 22\n"); !ok || n != 22 {
		t.Errorf("got %d, %v", n, ok)
	}
	if _, ok := ParseProtocol("no protocol here"); ok {
		t.Error("should not invent a protocol")
	}
}

func TestParseMachineList(t *testing.T) {
	if got := ParseMachineList("No saved SSH machines.\n"); len(got) != 0 {
		t.Errorf("an empty list must parse to nothing, got %v", got)
	}
	list := ParseMachineList("LABEL    TARGET\nlaptop   alice@192.0.2.5\ncloud    ubuntu@198.51.100.9\n")
	if len(list) != 2 {
		t.Fatalf("got %d machines: %v", len(list), list)
	}
	if list[0].Label != "laptop" || list[0].Target != "alice@192.0.2.5" {
		t.Errorf("first machine parsed wrong: %+v", list[0])
	}
}
