package test

import (
	"errors"
	"slices"
	"testing"

	"github.com/th2zz/perch/bridge/internal/actions"
	"github.com/th2zz/perch/bridge/internal/herdr"
)

// blockedObservation is a deterministic, well-understood prompt: codex's
// "press enter to confirm or esc to cancel" dialog.
func blockedObservation() actions.Observation {
	return actions.Observation{
		AgentKind:       "codex",
		AgentStatus:     herdr.StatusBlocked,
		Rule:            "live_strong_blocker",
		RecognisedAgent: true,
	}
}

// TestActionEquivalence checks the table in contracts/herdr-usage.md row by row.
// Each case names the single herdr command the user's intent must become.
func TestActionEquivalence(t *testing.T) {
	tests := []struct {
		name       string
		req        actions.Request
		obs        actions.Observation
		wantMethod string
		wantKeys   []string
		wantCLI    []string
		wantErr    error
	}{
		{
			name:       "A1 approve uses send-keys, not prompt",
			req:        actions.Request{DecisionID: "d1", Kind: actions.KindApprove, Target: "wA:p1"},
			obs:        blockedObservation(),
			wantMethod: "agent.send_keys",
			wantKeys:   []string{"enter"},
			wantCLI:    []string{"herdr", "agent", "send-keys", "wA:p1", "enter"},
		},
		{
			name:       "A2 reject uses send-keys",
			req:        actions.Request{DecisionID: "d1", Kind: actions.KindReject, Target: "wA:p1"},
			obs:        blockedObservation(),
			wantMethod: "agent.send_keys",
			wantKeys:   []string{"esc"},
			wantCLI:    []string{"herdr", "agent", "send-keys", "wA:p1", "esc"},
		},
		{
			name: "A3 answering a typed dialog submits atomically, bracketed-paste safe",
			req: actions.Request{DecisionID: "d1", Kind: actions.KindAnswer, Target: "wA:p1",
				Text: "use postgres"},
			// Hermes asks a question and expects an answer typed into it. herdr
			// refuses `agent.prompt` while an agent is blocked, so the answer goes
			// through the pane — as one atomic write, because the separate
			// send-text-then-Enter form does not honour bracketed paste, and a
			// multi-line answer would arrive as several submissions.
			obs: actions.Observation{
				AgentKind: "hermes", AgentStatus: herdr.StatusBlocked,
				Rule: "clarification_prompt", RecognisedAgent: true,
			},
			wantMethod: "pane.answer",
			wantKeys:   []string{"enter"},
			wantCLI:    []string{"herdr", "pane", "run", "wA:p1", "use postgres"},
		},
		{
			name: "A3b answering a key-only dialog is refused rather than typed into",
			req: actions.Request{DecisionID: "d1", Kind: actions.KindAnswer, Target: "wA:p1",
				Text: "use postgres"},
			obs: blockedObservation(),
			// codex's live_strong_blocker is a menu. Typing a sentence into it
			// would be garbage typed into a menu, so the honest answer is no.
			wantErr: actions.ErrNoDeterministicKeys,
		},
		{
			name: "A3c answering an agent that is ready for input uses prompt",
			req: actions.Request{DecisionID: "d1", Kind: actions.KindAnswer, Target: "wA:p1",
				Text: "use postgres"},
			obs:        actions.Observation{AgentKind: "codex", AgentStatus: herdr.StatusIdle, RecognisedAgent: true},
			wantMethod: "agent.prompt",
			wantCLI:    []string{"herdr", "agent", "prompt", "wA:p1", "use postgres"},
		},
		{
			name: "A4 prompt to a recognised agent uses prompt",
			req: actions.Request{DecisionID: "d1", Kind: actions.KindPrompt, Target: "wA:p1",
				Text: "hello, try another approach"},
			obs:        actions.Observation{AgentKind: "codex", AgentStatus: herdr.StatusIdle, RecognisedAgent: true},
			wantMethod: "agent.prompt",
			wantCLI:    []string{"herdr", "agent", "prompt", "wA:p1", "hello, try another approach"},
		},
		{
			name: "A5 prompt to a plain shell uses pane send-text",
			req: actions.Request{DecisionID: "d1", Kind: actions.KindPrompt, Target: "wA:p3",
				Text: "npm test"},
			obs:        actions.Observation{AgentStatus: herdr.StatusUnknown, RecognisedAgent: false},
			wantMethod: "pane.send_text",
			wantCLI:    []string{"herdr", "pane", "send-text", "wA:p3", "npm test"},
		},
	}

	m := actions.NewMapper()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := m.Map(tc.req, tc.obs)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Map returned an error: %v", err)
			}
			if cmd.Method != tc.wantMethod {
				t.Errorf("method = %q, want %q", cmd.Method, tc.wantMethod)
			}
			if !slices.Equal(cmd.Keys, tc.wantKeys) {
				t.Errorf("keys = %v, want %v", cmd.Keys, tc.wantKeys)
			}
			if !slices.Equal(cmd.CLI, tc.wantCLI) {
				t.Errorf("equivalent CLI = %v, want %v", cmd.CLI, tc.wantCLI)
			}
			if cmd.Target != tc.req.Target {
				t.Errorf("target = %q, want %q", cmd.Target, tc.req.Target)
			}
		})
	}
}

// TestOnlyFourKinds is the constitution IV check: the write surface is closed.
func TestOnlyFourKinds(t *testing.T) {
	want := []actions.Kind{actions.KindApprove, actions.KindReject, actions.KindAnswer, actions.KindPrompt}
	if !slices.Equal(actions.AllKinds, want) {
		t.Fatalf("the set of write kinds changed: %v", actions.AllKinds)
	}
	for _, extra := range []actions.Kind{"rename", "focus", "split", "delete", "layout", ""} {
		if extra.Valid() {
			t.Errorf("%q must not be a valid kind", extra)
		}
	}
}

// TestMappingRefusesWhenUndeterminable is the "never guess" guarantee.
func TestMappingRefusesWhenUndeterminable(t *testing.T) {
	m := actions.NewMapper()

	cases := []struct {
		name string
		obs  actions.Observation
	}{
		{
			name: "unknown rule",
			obs: actions.Observation{AgentKind: "codex", AgentStatus: herdr.StatusBlocked,
				Rule: "some_future_dialog", RecognisedAgent: true},
		},
		{
			name: "rule deliberately refused",
			obs: actions.Observation{AgentKind: "codex", AgentStatus: herdr.StatusBlocked,
				Rule: "osc_title_blocked", RecognisedAgent: true},
		},
		{
			name: "no rule at all",
			obs: actions.Observation{AgentKind: "codex", AgentStatus: herdr.StatusBlocked,
				RecognisedAgent: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.Map(actions.Request{DecisionID: "d", Kind: actions.KindApprove, Target: "wA:p1"}, tc.obs)
			if !errors.Is(err, actions.ErrNoDeterministicKeys) {
				t.Fatalf("err = %v, want ErrNoDeterministicKeys", err)
			}
			var und *actions.UndeterminableError
			if !errors.As(err, &und) || und.Reason == "" {
				t.Errorf("refusal must carry a reason the user can act on, got %v", err)
			}
		})
	}
}

// TestRejectRefusedWhenOnlyApproveKnown checks the partial-mapping rule: a
// dialog we can confirm but not cancel must not offer a cancel.
func TestRejectRefusedWhenOnlyApproveKnown(t *testing.T) {
	m := actions.NewMapper()
	obs := actions.Observation{AgentKind: "agy", AgentStatus: herdr.StatusBlocked,
		Rule: "trust_prompt", RecognisedAgent: true}

	if _, err := m.Map(actions.Request{DecisionID: "d", Kind: actions.KindApprove, Target: "wB:p1"}, obs); err != nil {
		t.Fatalf("approve should be mappable: %v", err)
	}
	_, err := m.Map(actions.Request{DecisionID: "d", Kind: actions.KindReject, Target: "wB:p1"}, obs)
	if !errors.Is(err, actions.ErrNoDeterministicKeys) {
		t.Fatalf("reject err = %v, want ErrNoDeterministicKeys", err)
	}
}

// TestActingOnARunningAgentIsRefused guards against injecting a keystroke into
// work that already resumed.
func TestActingOnARunningAgentIsRefused(t *testing.T) {
	m := actions.NewMapper()
	obs := blockedObservation()
	obs.AgentStatus = herdr.StatusWorking

	_, err := m.Map(actions.Request{DecisionID: "d", Kind: actions.KindApprove, Target: "wA:p1"}, obs)
	if !errors.Is(err, actions.ErrNotBlocked) {
		t.Fatalf("err = %v, want ErrNotBlocked", err)
	}
}

// TestRequestValidation covers the shape of the one body the app may send.
func TestRequestValidation(t *testing.T) {
	valid := actions.Request{DecisionID: "d", Kind: actions.KindApprove, Target: "wA:p1"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a well-formed approve must validate: %v", err)
	}

	cases := map[string]struct {
		req  actions.Request
		want error
	}{
		"fifth kind":          {actions.Request{Kind: "rename", Target: "p"}, actions.ErrUnknownKind},
		"answer without text": {actions.Request{Kind: actions.KindAnswer, Target: "p"}, actions.ErrMissingText},
		"approve with text":   {actions.Request{Kind: actions.KindApprove, Target: "p", Text: "hi"}, actions.ErrUnexpectedText},
		"missing target":      {actions.Request{Kind: actions.KindApprove}, actions.ErrMissingTarget},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := tc.req.Validate(); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestKeymapEvidence asserts every seeded mapping carries the manifest text it
// was derived from. A mapping without evidence is a guess, and guesses are what
// constitution II forbids.
func TestKeymapEvidence(t *testing.T) {
	agents := []string{"codex", "claude", "agy", "copilot", "droid", "hermes", "kilo", "opencode", "muse"}
	rules := []string{
		"live_strong_blocker", "trust_directory", "startup_update", "weak_blocker",
		"bash_permission_prompt", "generic_permission_prompt", "live_blocked_form",
		"mcp_elicitation_prompt", "question_prompt", "trust_prompt",
		"selection_blocker", "execute_selection_blocker", "selection_menu_blocker",
		"dangerous_command_approval", "confirmation_prompt",
		"opencode_permission", "permission_required", "menu_overlay",
	}
	seeded := 0
	for _, agent := range agents {
		for _, rule := range rules {
			keys, _, ok := actions.LookupKeys(agent, rule)
			if !ok {
				continue // not seeded for this agent; nothing is claimed
			}
			seeded++
			if !keys.CanPress() && !keys.AcceptsTypedAnswer() {
				t.Errorf("%s/%s is seeded but offers no way to respond", agent, rule)
			}
			if keys.Evidence == "" {
				t.Errorf("%s/%s is seeded without citing manifest evidence", agent, rule)
			}
		}
	}
	if seeded == 0 {
		t.Fatal("no mappings were found at all; the key table is not reachable")
	}
}

// TestARuleNameFromOneAgentNeverLeaksIntoAnother is the regression test for a
// real bug: the lookup used to fall back to matching a rule by name alone,
// which handed Hermes' typed-answer keys to every other agent that happened to
// have a rule called "clarification_prompt".
func TestARuleNameFromOneAgentNeverLeaksIntoAnother(t *testing.T) {
	// Hermes' rule is real and mapped.
	keys, _, ok := actions.LookupKeys("hermes", "clarification_prompt")
	if !ok || !keys.AcceptsTypedAnswer() {
		t.Fatalf("hermes/clarification_prompt should be mapped: %+v ok=%v", keys, ok)
	}

	// The same rule id in another agent is a different dialog, and must not
	// inherit anything.
	for _, agent := range []string{"codex", "claude", "agy", "cursor", "amp"} {
		keys, _, ok := actions.LookupKeys(agent, "clarification_prompt")
		if ok || keys.CanPress() || keys.AcceptsTypedAnswer() {
			t.Errorf("%s/clarification_prompt inherited another agent's dialog: %+v", agent, keys)
		}
	}
}

// TestATypedAnswerDialogOffersNoBlindKeystroke: pressing Enter into an empty
// answer box submits nothing while looking like it did something.
func TestATypedAnswerDialogOffersNoBlindKeystroke(t *testing.T) {
	obs := actions.Observation{
		AgentKind: "hermes", AgentStatus: herdr.StatusBlocked,
		Rule: "clarification_prompt", RecognisedAgent: true,
	}
	m := actions.NewMapper()
	for _, kind := range []actions.Kind{actions.KindApprove, actions.KindReject} {
		if _, err := m.Map(actions.Request{DecisionID: "d", Kind: kind, Target: "wB:p1"}, obs); !errors.Is(err, actions.ErrNoDeterministicKeys) {
			t.Errorf("%s on a typed-answer dialog should be refused, got %v", kind, err)
		}
	}
}
