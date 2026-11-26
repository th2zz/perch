package actions

import (
	"strings"
	"testing"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

const codexApprovalText = `
  Would you like to run the following command?

  Environment: local

  Reason: Do you want to allow creating the requested directory outside the workspace?

  $ mkdir -p /home/user/codex-approval-test


› 1. Yes, proceed (y)
  2. Yes, and don't ask again for commands that start with ` + "`mkdir -p /home/user/codex-approval-test`" + ` (p)
  3. No, and tell Codex what to do differently (esc)

  Press enter to confirm or esc to cancel
`

const dshApprovalText = `
    ───────────────────────────────────────────────────────────────── Awaiting approval · bash ─────────────────────────────────────────────────────────────────

      mkdir -p /home/user/dsh-approval-test
    Allow this operation with danger-full-access permissions: The command creates /home/user/dsh-approval-test, which is outside the workspace, so
    workspace-write cannot allow it; full access is needed for this single mkdir as requested.
    Allow this operation?


    ❯1. Yes, allow once
     2. No

    ↑/↓ select · Enter confirm · Esc reject
`

func TestParseCodexApproval(t *testing.T) {
	dialog, ok := ParseDialog(codexApprovalText)
	if !ok {
		t.Fatal("codex approval was not recognised")
	}
	if len(dialog.Options) != 3 {
		t.Fatalf("options = %+v, want three rows", dialog.Options)
	}
	if dialog.Selected != 0 {
		t.Errorf("selected = %d, want the first row", dialog.Selected)
	}
	if dialog.Options[0].Role != OptionAccept || dialog.Options[2].Role != OptionReject {
		t.Errorf("roles = %+v, want yes/no", dialog.Options)
	}
	keys, reason, ok := LookupKeysForPrompt("codex", "osc_title_blocked", codexApprovalText)
	if !ok || reason != "" {
		t.Fatalf("codex approval must be mappable: ok=%v reason=%q", ok, reason)
	}
	if strings.Join(keys.Approve, " ") != "enter" || strings.Join(keys.Reject, " ") != "esc" {
		t.Errorf("keys = %+v, want Enter/Esc", keys)
	}
}

func TestParseDshApproval(t *testing.T) {
	dialog, ok := ParseDialog(dshApprovalText)
	if !ok {
		t.Fatal("dsh approval was not recognised")
	}
	if dialog.Selected != 0 {
		t.Errorf("selected = %d, want the first row", dialog.Selected)
	}
	if dialog.Options[0].Role != OptionAccept || dialog.Options[1].Role != OptionReject {
		t.Errorf("roles = %+v, want yes/no", dialog.Options)
	}
	keys, reason, ok := LookupKeysForPrompt("dsh-tui", "osc_title_working", dshApprovalText)
	if !ok || reason != "" {
		t.Fatalf("dsh approval must be mappable: ok=%v reason=%q", ok, reason)
	}
	if strings.Join(keys.Approve, " ") != "enter" || strings.Join(keys.Reject, " ") != "esc" {
		t.Errorf("keys = %+v, want Enter/Esc", keys)
	}
}

func TestThePlannerWalksTheCursorToYes(t *testing.T) {
	// The same visible dialog, cursor moved to No. Approve has to reach Yes, not
	// confirm the row the person happened to leave selected.
	moved := strings.Replace(dshApprovalText, "❯1. Yes, allow once\n     2. No", "  1. Yes, allow once\n    ❯2. No", 1)
	keys, reason, ok := LookupKeysForPrompt("dsh-tui", "osc_title_working", moved)
	if !ok || reason != "" {
		t.Fatalf("moved cursor must still map: ok=%v reason=%q", ok, reason)
	}
	if got := strings.Join(keys.Approve, " "); got != "up enter" {
		t.Errorf("approve = %q, want up enter", got)
	}
}

func TestAVisibleDialogWithoutACursorDoesNotGuess(t *testing.T) {
	text := strings.ReplaceAll(dshApprovalText, "❯", " ")
	keys, reason, ok := LookupKeysForPrompt("dsh-tui", "osc_title_working", text)
	if !ok {
		t.Fatal("the options and footer are still recognisable")
	}
	if len(keys.Approve) != 0 {
		t.Fatalf("approve = %v, want no approve key without a cursor", keys.Approve)
	}
	if strings.Join(keys.Reject, " ") != "esc" {
		t.Errorf("reject = %v, want esc", keys.Reject)
	}
	if reason == "" {
		t.Error("the refusal must explain why Approve is unavailable")
	}
}

func TestOpaqueDialogsStillUseTheRuleTable(t *testing.T) {
	// A permission prompt with only an Esc footer is not a selection dialog; the
	// old rule table must still drive it.
	text := "Do you want to proceed?\n❯ 1. Yes\n  2. No\n\nEsc to cancel"
	if _, ok := ParseDialog(text); ok {
		t.Fatal("a dialog without a confirm footer was parsed as a selection menu")
	}
	keys, _, ok := LookupKeysForPrompt("claude", "bash_permission_prompt", text)
	if !ok || strings.Join(keys.Approve, " ") != "enter" {
		t.Fatalf("fallback keys = %+v, want the rule table's enter", keys)
	}
}

func TestTheMapperSendsThePlannedCodexAndDshKeys(t *testing.T) {
	for _, tc := range []struct {
		agent string
		text  string
		rule  string
	}{
		{"codex", codexApprovalText, "osc_title_blocked"},
		{"dsh-tui", dshApprovalText, "osc_title_working"},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			cmd, err := NewMapper().Map(
				Request{DecisionID: "d1", Kind: KindApprove, Target: "wA:p1"},
				Observation{
					AgentKind:       tc.agent,
					RecognisedAgent: true,
					AgentStatus:     herdr.StatusBlocked,
					Rule:            tc.rule,
					PromptText:      tc.text,
				},
			)
			if err != nil {
				t.Fatalf("approve: %v", err)
			}
			if strings.Join(cmd.Keys, " ") != "enter" {
				t.Errorf("keys = %v, want Enter", cmd.Keys)
			}
			assertFormsAgree(t, tc.agent, cmd)
		})
	}
}
