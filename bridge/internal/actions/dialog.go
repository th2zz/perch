package actions

import "strings"

// Dialog is the part of a harness's on-screen question that the product can
// actually act on: the options, which one the cursor is on, and the keys the
// harness itself advertises for confirming and cancelling.
//
// Why this exists. Approval used to be a table keyed by herdr's detection rule
// (`claude/bash_permission_prompt` → Enter). That works while the rule describes
// the dialog, and fails the moment herdr classifies the same visible dialog as
// title-only or working: codex reports `osc_title_blocked`, dsh reports
// `osc_title_working`, and the table has no honest entry for either, so the phone
// got a notification with no Approve or Reject. The dialog is right there on
// screen, in the same shape every harness uses: options, a cursor, a footer.
//
// So the rule table stays as the fallback for dialogs whose text is not readable,
// and this parser handles the ones that are. It is deliberately about *shape*,
// not about harness names: any harness that draws numbered or marked options and
// advertises Enter/Esc can be planned the same way.
type Dialog struct {
	Options  []DialogOption
	Selected int // index into Options, or -1 when no cursor could be read
	Confirm  []string
	Cancel   []string
	Evidence string
}

// DialogOption is one row the person can choose.
type DialogOption struct {
	Label string
	Role  OptionRole
}

// OptionRole is the intent of an option, read from its own words rather than from
// a per-agent table.
type OptionRole int

const (
	// OptionOther is a real option that is neither yes nor no: "Chat about this",
	// a model name, a colour. It still occupies a row, so navigation counts it.
	OptionOther OptionRole = iota
	OptionAccept
	OptionReject
)

// ParseDialog reads a visible selection dialog out of pane text.
//
// The bar for "this is a dialog" is deliberately high: a confirm/cancel footer and
// at least one accept and one reject option. A permission prompt whose footer only
// says "Esc to cancel" does not qualify; it is still handled by the rule table.
// That is what keeps this from turning every sentence ending in "No" into a button.
func ParseDialog(text string) (Dialog, bool) {
	lines := strings.Split(text, "\n")
	footerAt, confirm, cancel := findDialogFooter(lines)
	if footerAt < 0 {
		return Dialog{}, false
	}

	start := footerAt - 24
	if start < 0 {
		start = 0
	}
	dialog := Dialog{Selected: -1, Confirm: confirm, Cancel: cancel}
	for i := start; i < footerAt; i++ {
		raw := strings.TrimSpace(lines[i])
		if raw == "" {
			continue
		}
		marked := false
		for _, marker := range []string{"❯", "›", ">"} {
			if strings.HasPrefix(raw, marker) {
				marked = true
				raw = strings.TrimSpace(strings.TrimLeft(raw, "❯›>"))
				break
			}
		}
		label, numbered := stripOptionNumber(raw)
		if label == "" || strings.HasSuffix(label, "?") {
			// "Allow this operation?" is the question above the options, not an
			// option. A dialog's own question is never a choice.
			continue
		}
		if !marked && !numbered && len(label) > 80 {
			// A long sentence beginning "Allow this operation…" is the dialog's
			// explanation, not a row. Real unmarked options are short labels (Claude's
			// "Yes, I trust this folder" is the longest one measured).
			continue
		}
		role := optionRole(label)
		if role == OptionOther && !marked && !numbered {
			// Long prose starting with "Yes, ..." is a sentence, not a row.
			// Options are either marked by the cursor or numbered.
			continue
		}
		if role == OptionOther && !marked {
			// An unmarked numbered row that is neither yes nor no ("4. Chat about
			// this") is still a row the cursor may have to pass.
			if !numbered {
				continue
			}
		}
		dialog.Options = append(dialog.Options, DialogOption{Label: label, Role: role})
		if marked {
			dialog.Selected = len(dialog.Options) - 1
		}
	}

	if len(dialog.Options) < 2 ||
		!hasRole(dialog.Options, OptionAccept) || !hasRole(dialog.Options, OptionReject) {
		return Dialog{}, false
	}
	if dialog.Selected >= 0 {
		dialog.Evidence = "visible dialog: " + dialog.Options[dialog.Selected].Label
	} else {
		dialog.Evidence = "visible dialog: cursor not readable"
	}
	return dialog, true
}

// planDialog turns a parsed dialog into the keys Approve and Reject mean.
//
// Reject always uses the advertised cancel key rather than navigating to a "No"
// row: every dialog this parser accepts advertises Esc, and Esc cannot accidentally
// confirm the highlighted option. Approve picks the first accepting row and walks
// the cursor to it, so the same plan works whether Yes is first (most dialogs) or
// second (Claude's home-folder trust question).
func planDialog(dialog Dialog) (Keys, string) {
	reject := append([]string(nil), dialog.Cancel...)
	keys := Keys{Reject: reject}
	target := -1
	for i, option := range dialog.Options {
		if option.Role == OptionAccept {
			target = i
			break
		}
	}
	if target < 0 {
		return keys, "this dialog has no option Perch can identify as yes; open the session to answer it."
	}
	if dialog.Selected < 0 {
		return keys, "Perch can see the options but not which one is selected; open the session to answer it."
	}
	if dialog.Selected == target {
		keys.Approve = append([]string(nil), dialog.Confirm...)
		keys.Evidence = dialog.Evidence + "; cursor already on yes"
		return keys, ""
	}
	step := "down"
	delta := target - dialog.Selected
	if delta < 0 {
		step = "up"
		delta = -delta
	}
	keys.Approve = make([]string, 0, delta+len(dialog.Confirm))
	for i := 0; i < delta; i++ {
		keys.Approve = append(keys.Approve, step)
	}
	keys.Approve = append(keys.Approve, dialog.Confirm...)
	keys.Evidence = dialog.Evidence + "; cursor moved " + step + " to yes"
	return keys, ""
}

// findDialogFooter finds the last footer that advertises both a confirm and a
// cancel key, and returns the keys it names.
//
// Only the bottom few non-empty lines are considered. A footer is the harness's
// own claim about how the dialog is operated, which is why it is evidence rather
// than a guess.
func findDialogFooter(lines []string) (int, []string, []string) {
	checked := 0
	for i := len(lines) - 1; i >= 0; i-- {
		lower := strings.ToLower(strings.TrimSpace(lines[i]))
		if lower == "" {
			continue
		}
		checked++
		if checked > 8 {
			break
		}
		confirm, cancel := footerKeys(lower)
		if confirm && cancel {
			return i, []string{"enter"}, []string{"esc"}
		}
	}
	return -1, nil, nil
}

func footerKeys(lower string) (confirm, cancel bool) {
	confirm = strings.Contains(lower, "enter to confirm") ||
		strings.Contains(lower, "enter confirm") ||
		strings.Contains(lower, "enter to select") ||
		strings.Contains(lower, "enter select") ||
		strings.Contains(lower, "enter continue") ||
		strings.Contains(lower, "enter to continue")
	if !strings.Contains(lower, "esc") {
		return false, false
	}
	cancel = strings.Contains(lower, "cancel") ||
		strings.Contains(lower, "reject") ||
		strings.Contains(lower, "skip") ||
		strings.Contains(lower, "exit")
	return confirm, cancel
}

// stripOptionNumber removes a leading "1.", "1)" or "1:" and reports whether one
// was there. The number is not used for selection: arrow keys are the one
// navigation every dialog this parser accepts advertises.
func stripOptionNumber(s string) (string, bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return s, false
	}
	if i < len(s) && (s[i] == '.' || s[i] == ')' || s[i] == ':') {
		return strings.TrimSpace(s[i+1:]), true
	}
	return s, false
}

// optionRole classifies by the first word of the label. Prefix matching is
// intentional: "Yes, and don't ask again for …" is still yes, and "No, exit" is
// still no, while a sentence that merely contains the word "no" is not a row.
func optionRole(label string) OptionRole {
	lower := strings.ToLower(strings.TrimSpace(label))
	for _, prefix := range []string{
		"yes", "allow", "approve", "accept", "proceed", "trust", "continue", "permit",
	} {
		if strings.HasPrefix(lower, prefix) {
			return OptionAccept
		}
	}
	for _, prefix := range []string{
		"no", "reject", "decline", "deny", "exit", "cancel", "stop", "skip",
	} {
		if strings.HasPrefix(lower, prefix) {
			return OptionReject
		}
	}
	return OptionOther
}

func hasRole(options []DialogOption, role OptionRole) bool {
	for _, option := range options {
		if option.Role == role {
			return true
		}
	}
	return false
}
