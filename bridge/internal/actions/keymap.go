package actions

// Key sequences for approving and rejecting, keyed by detection rule.
//
// Why rule ids and not screen scraping: herdr already classifies each agent's
// UI and tells us which rule matched (`herdr agent explain` → "rule:
// bash_permission_prompt"). That rule is direct evidence of which dialog is on
// screen, so the key sequence can be derived from it deterministically instead
// of guessing from raw output.
//
// The table is not the only source when the screen itself carries the dialog. Some
// harnesses report a coarser rule than what they draw (codex: osc_title_blocked,
// dsh: osc_title_working); [ParseDialog] reads the options, cursor and advertised
// keys from the visible text, and [LookupKeysForPrompt] plans the same sequence for
// every harness. A dialog without readable evidence falls back here and may still
// be refused.
//
// Every entry below cites the manifest text it was derived from. Entries that
// are deliberately absent are recorded in ruleRefusals with a reason: herdr may
// report "blocked" for a dialog whose controls we cannot drive safely, and the
// honest answer there is to hand the user the escape hatch (spec FR-015,
// contracts/herdr-usage.md §2).
//
// Manifests are read from ~/.local/state/herdr/agent-detection/<source>/<agent>.toml
// on the machine this was written, e.g. claude version 2026.09.28.1 and
// codex 2026.10.11.1. A newer manifest can introduce rules we do not know;
// those fall through to refusal, which is the safe direction.

// Keys is the approve/reject key sequence for one interaction.
//
// A nil slice means "not determinable for this interaction", which is different
// from an empty slice. The mapper refuses only the kind whose slice is nil, so
// e.g. rejecting a trust prompt can stay available while approving it does not.
type Keys struct {
	Approve []string
	Reject  []string
	// Answer is the key sequence that *submits a typed answer*, not a decision.
	//
	// It exists because herdr's own documentation is explicit that `agent.prompt`
	// is refused while an agent is blocked: the only way to answer a live dialog
	// is to write into it (`pane.send_text`) and then press whatever submits it.
	// Most dialogs are not like that — they want a keystroke, and text would be
	// garbage typed into a menu — so this is populated only where the manifest
	// says in so many words that the dialog expects typing.
	Answer   []string
	Evidence string
}

// CanPress reports whether this interaction has any deterministic keystroke
// response at all.
func (k Keys) CanPress() bool { return len(k.Approve) > 0 || len(k.Reject) > 0 }

// AcceptsTypedAnswer reports whether writing text into the dialog is supported
// by evidence rather than by hope.
func (k Keys) AcceptsTypedAnswer() bool { return len(k.Answer) > 0 }

// ruleKeys is keyed by "agent/rule".
var ruleKeys = map[string]Keys{
	// ---------------------------------------------------------------- codex
	"codex/live_strong_blocker": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `contains = ["press enter to confirm or esc to cancel"]`,
	},
	"codex/trust_directory": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `contains = ["Trust and continue"] / ["enter continue"]`,
	},
	"codex/startup_update": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `"Press enter to continue" ; regex = ['enter\s+continue\s+·\s+esc\s+skip']`,
	},
	"codex/weak_blocker": {
		Approve:  []string{"y"},
		Reject:   []string{"n"},
		Evidence: `contains = ["[y/n]"] / ["yes (y)"]`,
	},

	// --------------------------------------------------------------- claude
	// Bash approvals are the single most common decision, and the manifest
	// spells the resting layout out: "❯ 1. Yes / 2. Yes, and don't ask again
	// for: <cmd> / 3. No", with an "esc to cancel" footer.
	"claude/bash_permission_prompt": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `line_regex = ['^\s*❯?\s*1\.\s*yes\b' ... '3\.\s*no\b'], contains = ["do you want to proceed?"]`,
	},
	"claude/generic_permission_prompt": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `contains = ["do you want to proceed?", "esc to cancel"], options "1. yes" .. "3. no"`,
	},
	"claude/live_blocked_form": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `contains = ["esc to cancel"] + any(["enter to confirm"] | ["enter to select", "arrow keys to navigate"])`,
	},
	// Elicitation dialogs put the cursor on Accept/Decline with an "esc to
	// cancel" footer; the ❯ marker is the cursor, so Enter activates it.
	"claude/mcp_elicitation_prompt": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `line_regex = ['^\s*❯?\s*Accept\b' | '^\s*❯?\s*Decline\b'], contains = ["esc to cancel"]`,
	},

	// ------------------------------------------------------------------ agy
	// Footer rows are the evidence: "↑/↓ Navigate · enter Select · esc Skip".
	"agy/question_prompt": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `contains = ["navigate", "esc skip"] (footer lists "enter Select")`,
	},
	"agy/trust_prompt": {
		Approve:  []string{"enter"},
		Evidence: `contains = [..., "enter confirm"]`,
		// No cancel key is advertised for this dialog, so reject stays nil.
	},

	// --------------------------------------------------------------- others
	"copilot/selection_blocker": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `"enter to select", "esc to cancel"`,
	},
	"droid/execute_selection_blocker": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `"enter to select", "esc to cancel"`,
	},
	"droid/selection_menu_blocker": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `"enter select", "esc cancel"`,
	},
	"hermes/dangerous_command_approval": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `contains = ["enter confirm"]`,
	},
	// This rule matches a dialog that wants an answer *typed into it*
	// ("type your answer", "enter send"). A bare Enter here would submit an empty
	// answer and look like it had done something, so there is deliberately no
	// approve/reject mapping: what the dialog accepts is text, and the sequence
	// below is the one that sends it.
	"hermes/clarification_prompt": {
		Answer:   []string{"enter"},
		Evidence: `contains any ["type your answer", "hermes needs your"] with all any ["enter send", "enter to confirm"]`,
	},
	"hermes/confirmation_prompt": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `contains = ["enter to confirm"]`,
	},
	"kilo/opencode_permission": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `"enter confirm", "esc dismiss"`,
	},
	"opencode/permission_required": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `"enter confirm", "esc dismiss"`,
	},
	"muse/menu_overlay": {
		Approve:  []string{"enter"},
		Reject:   []string{"esc"},
		Evidence: `"enter confirm", "esc go back"`,
	},
	"cursor/approval_prompt": {
		Approve:  []string{"y"},
		Evidence: `contains = ["(y) (enter)"]`,
	},
	"cursor/write_file_approval": {
		Reject:   []string{"n"},
		Evidence: `contains = ["esc or n or p"]`,
	},
	"devin/permission_prompt": {
		Reject:   []string{"esc"},
		Evidence: `contains = ["esc cancel"]`,
	},
	"kimi/question_panel": {
		Reject:   []string{"esc"},
		Evidence: `contains = ["esc cancel"]`,
	},
	"kiro/question_panel": {
		Reject:   []string{"esc"},
		Evidence: `contains = ["esc to cancel"]`,
	},
	"maki/plan_complete_form": {
		Approve:  []string{"enter"},
		Evidence: `contains = ["enter confirm"]`,
	},
}

// ruleRefusals records interaction rules we deliberately will not drive, with
// the reason shown to the user. The list is not a backlog: each entry is a case
// where sending a key could do the wrong thing.
var ruleRefusals = map[string]string{
	"codex/osc_title_blocked": "Perch can see that a dialog is waiting, but not what it says, so it will not guess which key to press.",

	"claude/dynamic_workflow_prompt":  "This dialog lets you cancel but does not say how to confirm, so Perch cannot press it for you.",
	"claude/legacy_no_prompt_blocker": "Perch can see a question on screen but not which option the cursor is on.",
	// herdr 2026.10.11.1 added this one, and the rule's own footer *does* name its keys
	// ("enter to confirm · esc to cancel"). It is still refused, and the reason is the
	// product's oldest rule rather than a missing measurement: the footer says Enter confirms
	// whatever is highlighted, not that the highlighted option means yes — and this rule
	// matches a whole *form* (it is named for a session-name prompt) where "the highlighted
	// option" is not the same question from one screen to the next. Pressing that key on the
	// user's behalf is the maki case, which is why the table already refuses one dialog for
	// exactly this reason.
	"claude/live_blocked_form_named_session": "This dialog asks you to choose or type something, and the key it names confirms whatever is highlighted — not the same thing as saying yes. Open the session and press it yourself.",

	"agy/permission_prompt":        "Antigravity's permission dialog lets you navigate and amend, but does not say which key confirms.",
	"agy/legacy_permission_prompt": "This Antigravity permission dialog does not say which key confirms it.",

	"maki/permission_prompt": "In this dialog the confirm key denies the request rather than allowing it.",

	"qodercli/confirmation_or_input_blocker": "this rule matches several different Qoder dialogs (typed responses, yes/no confirmations, \"allow once or always?\"), so what it accepts cannot be determined from the rule alone",

	"gemini/apply_or_allow_change":       "This Gemini prompt has several variants with different defaults, so Perch will not pick one for you.",
	"grok/option_dialog_blocked":         "This Grok dialog does not say which key confirms it.",
	"grok/permission_hints_blocked":      "This Grok permission dialog does not say which key confirms it.",
	"grok/question_dialog_hints_blocked": "This Grok question does not say which key confirms it.",
	"grok/permission_scope_selector":     "This Grok dialog asks you to choose a scope. Picking one for you would be a guess.",

	"amp/approval_footer":        "This Amp dialog does not say which key confirms it.",
	"letta/command_approval":     "This Letta dialog does not say which key confirms it.",
	"letta/osc_title_blocked":    "Only the window title shows that something is waiting; the dialog itself is not visible.",
	"letta/osc_progress_blocked": "Only a progress marker shows that something is waiting; there is no dialog to answer.",

	"kiro/tool_approval":      "This Kiro dialog does not say which key confirms it.",
	"kiro/tool_approval_edit": "This Kiro dialog does not say which key confirms it.",
	"kiro/crew_approval":      "This Kiro dialog does not say which key confirms it.",

	"cline/tool_permission":        "This Cline dialog does not say which key confirms it.",
	"cline/inline_tool_permission": "This Cline dialog does not say which key confirms it.",
	"cline/inline_question":        "This Cline question expects typed input or a choice Perch cannot make for you.",

	"kimi/current_approval_panel": "This Kimi dialog does not say which key confirms it.",
	"kimi/legacy_approval_panel":  "This Kimi dialog does not say which key confirms it.",

	"qwen/waiting_for_confirmation": "This Qwen dialog does not say which key confirms it.",
	"qwen/tool_confirmation":        "This Qwen dialog does not say which key confirms it.",
	"qwen/question_dialog":          "This Qwen question does not say which key confirms it.",
	"qwen/folder_trust_dialog":      "This Qwen trust prompt does not say which key confirms it.",
	"qwen/osc_title_blocked":        "Only the window title shows that something is waiting; the dialog itself is not visible.",

	"muse/blocked_approval":        "This Muse dialog does not say which key confirms it.",
	"muse/pick_request_blocked":    "This Muse picker does not say which key confirms it.",
	"muse/workspace_trust_blocked": "This Muse trust prompt does not say which key confirms it.",

	// Title-only evidence: herdr can tell that a dialog is on screen because it
	// wrote something into the pane title, but the dialog's own text never
	// reached the screen we can see. These are the same shape as
	// codex/osc_title_blocked and there is no honest way to pick a key.
	"amp/osc_title_plugin_confirmation_blocked": "Only a window title shows that a plugin wants confirmation; the dialog itself is not visible.",
	"grok/osc_title_blocked":                    "Only a window title shows that something needs you; the dialog itself is not visible.",
	"hermes/osc_title_blocked":                  "Only a window title shows that something needs you; the dialog itself is not visible.",

	// Options are on screen but no confirm key is advertised, so pressing one
	// would be a guess about which option the cursor is resting on.
	"devin/workspace_trust_prompt": "Devin's trust dialog shows its options but never advertises which key confirms the highlighted one",

	// A prompt that wants a secret typed into it. Refusing is the product's
	// position, not a limitation: offering a button here would mean inviting the
	// user to send a password or token through the phone.
	"hermes/credential_prompt": "this dialog is asking for a password or key; Perch does not type secrets, and it should not be sent from a phone",
}

// LookupKeys resolves the key sequence for an observation.
//
// Return values: keys (maybe partially populated), a refusal reason, and
// whether anything at all was found. Both ok=false and a non-empty reason mean
// the mapper must refuse; the difference is only in what the user is told.
func LookupKeys(agentKind, rule string) (Keys, string, bool) {
	if rule == "" {
		return Keys{}, reasonNoRule, false
	}
	if k, found := ruleKeys[agentKind+"/"+rule]; found {
		return k, "", true
	}
	if reason, found := ruleRefusals[agentKind+"/"+rule]; found {
		return Keys{}, reason, false
	}
	return Keys{}, reasonUnknownDialog, false
}

// LookupKeysForPrompt resolves a dialog whose visible text is better evidence
// than herdr's rule id.
//
// herdr's rule is the first source of truth, and the table below still handles
// dialogs whose text cannot be read. But a rule can be coarser than the screen:
// codex says "Action Required" in the title and reports osc_title_blocked even
// while the full approval menu is visible, and dsh reports osc_title_working
// while blocked on an approval. In those cases the text itself carries the
// options, the selected row and the advertised keys, so the dialog parser is the
// honest source. When the text is not a complete dialog, the rule table and its
// refusals are used exactly as before.
func LookupKeysForPrompt(agentKind, rule, promptText string) (Keys, string, bool) {
	if dialog, ok := ParseDialog(promptText); ok {
		keys, reason := planDialog(dialog)
		return keys, reason, true
	}
	return LookupKeys(agentKind, rule)
}

// The reasons below end up on the user's screen — on a decision card and in the
// escape hatch — so they are written in the user's terms.
//
// That is a stronger constraint than it looks. The first versions said things like
// "detection rule some_unmapped_dialog is not in the bridge's key table", which is
// accurate and useless: it names three things the user has never heard of, to
// explain why a button is missing. A rendered screenshot is what made it obvious
// that data assembled inside the bridge is still copy.
const (
	// reasonNoRule is used when a dialog is on screen but unnamed.
	reasonNoRule = "Perch cannot tell what this dialog is asking. Open the session to read it and answer there."
	// reasonUnknownDialog is used when the dialog is named but not one we know.
	reasonUnknownDialog = "This is a dialog Perch has not seen before, so it will not guess which key to press. Open the session to read it and answer there."
)

// RuleStatus is what the bridge knows about a detection rule.
type RuleStatus int

const (
	// RuleUnknown means herdr can report this dialog but the bridge has no
	// opinion about it. Every one of these is a decision the user cannot make
	// from the phone, so they are a defect rather than a neutral state.
	RuleUnknown RuleStatus = iota
	// RuleMapped means a deterministic key sequence exists for at least one of
	// approve or reject.
	RuleMapped
	// RuleRefused means the bridge deliberately will not drive this dialog.
	RuleRefused
)

// Coverage reports whether a detection rule is accounted for.
//
// This exists so the product can answer a question that only gets harder over
// time: herdr ships agent manifests on its own schedule, and a new blocked
// dialog would otherwise reach users as a card with no working button. The
// coverage test in bridge/test turns that into a build failure instead.
func Coverage(agentKind, rule string) (RuleStatus, string) {
	if rule == "" {
		return RuleUnknown, "no rule was reported"
	}
	if keys, found := ruleKeys[agentKind+"/"+rule]; found {
		// An entry that can only accept a typed answer still counts as accounted
		// for: the user has a way to respond, and it is one the manifest supports.
		if keys.CanPress() || keys.AcceptsTypedAnswer() {
			return RuleMapped, ""
		}
		return RuleRefused, "the entry declares no keys and no typed answer"
	}
	if reason, found := ruleRefusals[agentKind+"/"+rule]; found {
		return RuleRefused, reason
	}
	return RuleUnknown, ""
}

// RefusedRules lists the rules the bridge deliberately will not drive, for
// diagnostics and for the coverage test's failure message.
func RefusedRules() map[string]string {
	out := make(map[string]string, len(ruleRefusals))
	for rule, reason := range ruleRefusals {
		out[rule] = reason
	}
	return out
}
