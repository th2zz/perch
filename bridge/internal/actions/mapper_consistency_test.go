package actions

import (
	"strings"
	"testing"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

// TestTheRunnableFormMatchesTheDisplayedCommand.
//
// Every Command carries the same work twice: a `CLI` sentence the user is shown
// ("this is what your tap does") and a `Remote` argv the bridge can actually hand
// to a process when the target is another machine. Two spellings of one action is
// exactly the kind of thing that drifts, and the drift would be invisible: the
// screen would promise one command and the other computer would receive another
// (constitution II).
//
// So the two are compared mechanically, for every mapping the product can produce,
// including the low-level answer case where the sentence contains two commands.
func TestTheRunnableFormMatchesTheDisplayedCommand(t *testing.T) {
	m := NewMapper()

	for combo := range ruleKeys {
		agent, rule, found := strings.Cut(combo, "/")
		if !found {
			t.Fatalf("rule key %q is not agent/rule", combo)
		}
		obs := Observation{
			AgentKind:       agent,
			Rule:            rule,
			RecognisedAgent: true,
			AgentStatus:     herdr.StatusBlocked,
		}
		for _, kind := range []Kind{KindApprove, KindReject} {
			cmd, err := m.Map(Request{DecisionID: "d1", Kind: kind, Target: "wA:p1"}, obs)
			if err != nil {
				// A rule may legitimately have no key for one of the two directions.
				continue
			}
			assertFormsAgree(t, combo, cmd)
		}
		if keys, _, ok := LookupKeys(agent, rule); ok && keys.AcceptsTypedAnswer() {
			cmd, err := m.Map(Request{DecisionID: "d1", Kind: KindAnswer, Target: "wA:p1", Text: "résumé update\nwith a newline"}, obs)
			if err != nil {
				t.Fatalf("%s: answer should map: %v", combo, err)
			}
			assertFormsAgree(t, combo+"/answer", cmd)
		}
	}

	// Prompt to a ready agent, and the escape hatch's text delivery (no agent).
	ready, err := m.Map(
		Request{DecisionID: "d1", Kind: KindPrompt, Target: "wA:p1", Text: "npm test"},
		Observation{AgentKind: "codex", RecognisedAgent: true, AgentStatus: herdr.StatusIdle},
	)
	if err != nil {
		t.Fatalf("prompt to a ready agent: %v", err)
	}
	assertFormsAgree(t, "prompt/agent", ready)

	text, err := m.Map(
		Request{Kind: KindPrompt, Target: "wE:p1", Text: "echo hi"},
		Observation{},
	)
	if err != nil {
		t.Fatalf("escape hatch text: %v", err)
	}
	assertFormsAgree(t, "prompt/pane", text)
}

func TestPromptToAPaneLaunchedHarnessUsesPaneRun(t *testing.T) {
	cmd, err := NewMapper().Map(
		Request{Kind: KindPrompt, Target: "wA:p1", Text: "hello"},
		Observation{
			AgentKind: "dsh-tui", RecognisedAgent: true, AgentStatus: herdr.StatusIdle,
		},
	)
	if err != nil {
		t.Fatalf("dsh-tui prompt: %v", err)
	}
	if cmd.Method != MethodPaneAnswer {
		t.Fatalf("method = %q, want pane input+submit", cmd.Method)
	}
	if len(cmd.Keys) != 1 || cmd.Keys[0] != "enter" {
		t.Errorf("keys = %v, want Enter", cmd.Keys)
	}
	if got := strings.Join(cmd.Remote[0], " "); !strings.Contains(got, "pane run wA:p1 hello") {
		t.Errorf("remote = %q, want pane run", got)
	}
	assertFormsAgree(t, "prompt/dsh-tui", cmd)
}

func assertFormsAgree(t *testing.T, label string, cmd Command) {
	t.Helper()
	if len(cmd.Remote) == 0 {
		t.Fatalf("%s: no runnable form was built, so a remote action would be refused", label)
	}
	// The screen form is what a person may copy into a shell, so *every* command in it has to start with
	// the program name — a chained sentence whose second half begins at `pane` is `pane: command not
	// found` when somebody pastes it. `splitDisplayedCommands` drops the token wherever it appears, so
	// this is the check that notices a missing one.
	if cmd.CLI[0] != herdr.Program {
		t.Errorf("%s: the shown command does not start with %s\n  cli: %v", label, herdr.Program, cmd.CLI)
	}
	for i, arg := range cmd.CLI {
		if arg == herdr.Program || arg == herdr.Separator {
			continue
		}
		if i > 0 && cmd.CLI[i-1] == herdr.Separator {
			t.Errorf("%s: the command after %s does not start with %s\n  cli: %v",
				label, herdr.Separator, herdr.Program, cmd.CLI)
		}
	}
	displayed := splitDisplayedCommands(cmd.CLI)
	if len(displayed) != len(cmd.Remote) {
		t.Fatalf("%s: the screen shows %d command(s) and the runnable form has %d\n  cli: %v\n  remote: %v",
			label, len(displayed), len(cmd.Remote), cmd.CLI, cmd.Remote)
	}
	for i := range displayed {
		want := strings.Join(displayed[i], " ")
		got := strings.Join(cmd.Remote[i], " ")
		if want != got {
			t.Errorf("%s: command %d differs\n  shown:   %s\n  runs as: herdr --machine <host> %s",
				label, i+1, want, got)
		}
	}
}

// splitDisplayedCommands turns `["herdr","pane","run",…,"&&","herdr",…]` into the
// argv lists it describes.
func splitDisplayedCommands(cli []string) [][]string {
	var out [][]string
	var cur []string
	for _, arg := range cli {
		switch arg {
		case "herdr":
			continue
		case "&&":
			if len(cur) > 0 {
				out = append(out, cur)
				cur = nil
			}
		default:
			cur = append(cur, arg)
		}
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}
