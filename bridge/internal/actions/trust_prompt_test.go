package actions

import (
	"strings"
	"testing"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

// The two layouts of Claude's folder-trust question, captured from a live pane.
// The home-folder one is the reason this special case exists: its cursor starts on
// the option that exits Claude.
const (
	claudeTrustHome = "\n Quick safety check: Is this a project you created or one you trust?\n\n" +
		" Claude Code'll be able to read, edit, and execute files here.\n\n" +
		" ❯ No, exit\n" +
		"   Yes, I trust this folder\n\n" +
		" Enter to confirm · Esc to cancel"
	claudeTrustProject = "\n Quick safety check: Is this a project you created or one you trust?\n\n" +
		" ❯ Yes, I trust this folder\n" +
		"   No, exit\n\n" +
		" Enter to confirm · Esc to cancel"
	claudeTrustMoved = "\n Quick safety check: Is this a project you created or one you trust?\n\n" +
		"   Yes, I trust this folder\n" +
		" ❯ No, exit\n\n" +
		" Enter to confirm · Esc to cancel"
)

func TestTheCautiousTrustDefaultNeedsOneArrowBeforeEnter(t *testing.T) {
	keys, reason, ok := LookupKeysForPrompt("claude", "live_blocked_form", claudeTrustHome)
	if !ok || reason != "" {
		t.Fatalf("the home trust prompt must be mappable: ok=%v reason=%q", ok, reason)
	}
	if got := strings.Join(keys.Approve, " "); got != "down enter" {
		t.Errorf("approve = %q, want down then enter: Enter alone chooses No, exit", got)
	}
	if got := strings.Join(keys.Reject, " "); got != "esc" {
		t.Errorf("reject = %q, want esc", got)
	}
}

func TestAnOrdinaryProjectStillApprovesWithEnter(t *testing.T) {
	keys, _, ok := LookupKeysForPrompt("claude", "live_blocked_form", claudeTrustProject)
	if !ok {
		t.Fatal("the ordinary trust prompt must be mappable")
	}
	if got := strings.Join(keys.Approve, " "); got != "enter" {
		t.Errorf("approve = %q, want enter: Yes is already selected", got)
	}
}

func TestTheArrowFollowsTheYesOptionWhenTheCursorMoved(t *testing.T) {
	keys, _, ok := LookupKeysForPrompt("claude", "live_blocked_form", claudeTrustMoved)
	if !ok {
		t.Fatal("a moved cursor must still be mappable")
	}
	if got := strings.Join(keys.Approve, " "); got != "up enter" {
		t.Errorf("approve = %q, want up then enter: Yes is above the cursor", got)
	}
}

func TestAnUnreadableCursorNeverGetsABlindEnter(t *testing.T) {
	// Same dialog, marker stripped (a wrapped or rearranged screen). Enter would
	// activate whatever happens to be selected, which in home is "No, exit".
	text := strings.ReplaceAll(claudeTrustHome, "❯", " ")
	keys, reason, ok := LookupKeysForPrompt("claude", "live_blocked_form", text)
	if !ok {
		t.Fatal("the trust prompt is still recognisable without its cursor")
	}
	if len(keys.Approve) != 0 {
		t.Fatalf("approve = %v, want no approve key when the cursor is unknown", keys.Approve)
	}
	if strings.Join(keys.Reject, " ") != "esc" {
		t.Errorf("reject = %v, want esc", keys.Reject)
	}
	if reason == "" {
		t.Error("the refusal must explain why Approve is unavailable")
	}
}

func TestTheMapperSendsTheArrowBeforeTheConfirmingEnter(t *testing.T) {
	cmd, err := NewMapper().Map(
		Request{DecisionID: "d1", Kind: KindApprove, Target: "wA:p1"},
		Observation{
			AgentKind:       "claude",
			RecognisedAgent: true,
			AgentStatus:     herdr.StatusBlocked,
			Rule:            "live_blocked_form",
			PromptText:      claudeTrustHome,
		},
	)
	if err != nil {
		t.Fatalf("home trust approval: %v", err)
	}
	if got := strings.Join(cmd.Keys, " "); got != "down enter" {
		t.Errorf("keys = %q, want down enter", got)
	}
	assertFormsAgree(t, "claude/home-trust", cmd)
}

func TestARuleWithoutATrustPromptKeepsItsOrdinaryMapping(t *testing.T) {
	keys, _, ok := LookupKeysForPrompt(
		"claude", "live_blocked_form",
		" Do you want to proceed?\n ❯ 1. Yes\n   2. No\n Esc to cancel",
	)
	if !ok {
		t.Fatal("ordinary live_blocked_form must still be mappable")
	}
	if got := strings.Join(keys.Approve, " "); got != "enter" {
		t.Errorf("approve = %q, want enter", got)
	}
}
