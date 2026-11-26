package core

import (
	"regexp"
	"strings"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

// composerRows is how much of the bottom of the screen the styled read asks for, and
// how far up the scan for the input line goes.
//
// One number for both, because they are the same question: twelve rows is more than
// any of these agents puts between the input line and the end of the screen (a footer
// and a border, at most) and small enough that a line of transcript further up cannot
// be mistaken for it.
const composerRows = 12

// composerNearEnd is how far above the bottom of the screen the input line may sit.
//
// Every one of these harnesses draws the box at the bottom and its footer under it, and
// that is the whole of the reasoning — but it took a measurement to trust it: codex,
// mid-task with a *queued* message, which puts more furniture under a box than anything
// else this product has seen, keeps its box 3 lines above the end (captured 2026-10-13).
//
// The bound matters for lists. claude's first-run theme picker marks its selection with the
// same character as its prompt box (`❯ ✔ Dark mode`, captured live, and herdr calls that
// pane `idle`), and on a tall pane the twelve-row window already excludes it — thirteen
// lines up. On a *short* pane the same list lands inside that window, and then only this
// bound says "that is a list, not a box". Eight lines is generous for a footer and narrow
// enough to exclude a list of options.
const composerNearEnd = 8

type Composer struct {
	// Unsent is text a person typed into the input line and has not submitted. A send
	// would be appended to it, which is the whole reason this file exists.
	Unsent string
	Command bool
	// Menu is true when the bottom of the screen is a *menu* rather than an input line —
	// codex's model picker, a slash-command list, an options dialog. The phone must not
	// type into one: typing there drives the menu (measured: a sentence sent while the
	// model picker was open advanced it to the reasoning-level picker), and herdr reports
	// these panes as `idle`, so nothing else in the system knows.
	Menu bool
	Status string
	// ChromeLines is how many trailing lines of the session's text are the harness's own
	// furniture rather than what it said: the input box, the rules around it, the footer.
	// The conversation view drops them; the terminal view keeps everything, because that
	// view's whole promise is that it is the screen.
	ChromeLines int
}

func ReadComposer(kind string, status herdr.AgentStatus, styled string) Composer {
	// The states where sending appends to whatever is in the box. "unknown" is
	// excluded on purpose: a prompt to an unrecognised pane goes through
	// `pane.send_text`, which does not submit, so nothing merges silently there and
	// the screen shows the text the user typed at the prompt.
	// The furniture below an answer is worth reading while an agent works too (that is when
	// its footer shows the useful part), so this gate is wider than the one for unsent text
	// below. Blocked is excluded either way: a dialog replaces the box, and the "footer"
	// would be a dialog's key hints.
	if status != herdr.StatusIdle && status != herdr.StatusDone && status != herdr.StatusWorking {
		return Composer{}
	}
	entry, known := composers[kind]
	if !known {
		return Composer{}
	}

	lines := strings.Split(styled, "\n")
	// The input line is drawn at the bottom, so the search starts there.
	if len(lines) > composerRows {
		lines = lines[len(lines)-composerRows:]
	}
	visible := make([]string, len(lines))
	dims := make([][]bool, len(lines))
	for i, line := range lines {
		runes, dim := parseStyled(line)
		visible[i] = string(runes)
		dims[i] = dim
	}
	// A menu is not an input line, whatever its options look like.
	//
	// The phrases are the ones these agents put under a menu, and they are taken from
	// herdr's own manifests, which use exactly this trick: claude's prompt-box rule
	// refuses to match a box whose bottom says "enter to select", "esc to cancel" or
	// "tab/arrow keys".
	for _, line := range visible {
		lower := strings.ToLower(line)
		for _, hint := range menuHints {
			if strings.Contains(lower, hint) {
				// A menu, not a box: say so, and claim nothing about unsent text — the
				// option rows are the agent's words, not the user's.
				//
				// Nothing is stripped from the text either: a menu is not furniture, it is
				// the thing the session is waiting for the person to answer, and hiding it
				// would hide the only part of the screen that matters in that moment.
				return Composer{Menu: true}
			}
		}
	}
	// The box is at the bottom of the screen, so the search starts there and does not go
	// far: a marker line the width of a list away from the bottom is a list (composerNearEnd
	// carries the two captures that decided this).
	first := len(lines) - composerNearEnd
	if first < 0 {
		first = 0
	}
	for i := len(lines) - 1; i >= first; i-- {
		shown := []rune(visible[i])
		dim := dims[i]
		begin := 0
		for begin < len(shown) && (shown[begin] == ' ' || shown[begin] == '\t' || shown[begin] == '\r') {
			begin++
		}
		if begin == len(shown) {
			continue
		}
		if shown[begin] != entry.marker {
			continue
		}
		// Everything after the marker, and whether any of it was drawn in normal
		// intensity rather than dim.
		content := shown[begin+1:]
		boxHoldsText := anyTyped(content, dim[begin+1:])

		// The harness's furniture starts here (and one line higher when it drew a rule
		// above the box, as agy does), and runs to the end of the screen. The app drops
		// those lines from the reading view and keeps them in the terminal view.
		furniture := i
		ruled := furniture > 0 && isBorder(visible[furniture-1])
		if ruled {
			furniture--
		}
		// A bordered harness always draws its rule; a marker without one is a list — claude's
		// startup questions mark their selected option with the same `❯` and put it at the
		// bottom of the screen, where herdr calls the pane `idle` (captured: "❯ Yes, set auto
		// mode as my default permission mode"). Saying "a menu is open" is the safe answer:
		// the app refuses to type, and the keys are one tap away.
		if entry.bordered && !ruled {
			return Composer{Menu: true}
		}
		out := Composer{
			ChromeLines: len(lines) - furniture,
			// Below the *box*, not below the rule that opens it.
			Status: footerLines(visible[i+1:]),
		}
		// A bordered harness puts half of its status *above* the box: claude draws
		// "● high · /effort" on the line over the top rule (captured), so a footer read only
		// from below would lose the model and the reasoning effort. For a harness with no
		// rules this line is ordinary output, which is why it is only read here.
		if ruled && furniture > 0 {
			if above := footerLines(visible[furniture-1 : furniture]); above != "" {
				out.Status = above + "\n" + out.Status
			}
		}
		// Unsent text is only claimed where a send would submit it — see the gate at the
		// top: while an agent works, that space can also hold queued messages and the
		// footer's own rows, and a warning about text nobody typed is worse than silence.
		if boxHoldsText && (status == herdr.StatusIdle || status == herdr.StatusDone) {
			out.Unsent = strings.TrimSpace(string(content))
			out.Command = strings.HasPrefix(out.Unsent, "/")
		}
		return out
	}
	return Composer{}
}

// footerLines is the harness's own status line, in a shape a phone can read.
//
// Two things happen here and both are mechanical rather than clever. Blank lines and
// rules are dropped: they are the drawing, not the words. And runs of two or more spaces
// become a separator, because a harness lays its footer out across a 200-column window —
// the model and the context usage on the left, the warning on the right — and a phone is
// forty characters wide. Without that, the right-hand half of every footer would wrap
// into the middle of the left-hand half.
func footerLines(lines []string) string {
	var out []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" || isBorder(line) {
			continue
		}
		out = append(out, wideGap.ReplaceAllString(strings.TrimSpace(line), " · "))
	}
	return strings.Join(out, "\n")
}

// wideGap matches the runs of spaces a harness uses to align columns across a wide pane.
var wideGap = regexp.MustCompile(`\s{2,}`)

// isBorder reports whether a line is nothing but drawing — the rule a harness puts
// around its input box, in box-drawing characters or in ASCII.
func isBorder(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}
	for _, r := range trimmed {
		switch r {
		case '─', '━', '═', '-', '=', '_':
		default:
			return false
		}
	}
	return true
}

// anyTyped reports whether any of a composer's content came from a person rather than
// from the agent.
//
// Spaces never count, and neither does anything drawn dim: codex answers an empty
// input line with a dim "Ask Codex to do anything" (measured on a live pane), which is
// the terminal's way of saying "this is a hint, not your text".
func anyTyped(content []rune, dim []bool) bool {
	for i, r := range content {
		if r == ' ' || r == '\t' || r == '\r' {
			continue
		}
		if !dim[i] {
			return true
		}
	}
	return false
}

// menuHints are the phrases that mean "the bottom of this screen is a menu".
//
// Every one of them was seen on a live pane on this machine, or is copied from the
// `not` conditions in herdr's own detection manifests for these agents — the same
// authority this file reads the input line from.
var menuHints = []string{
	"enter select",
	"enter to select",
	"esc back",
	"esc to cancel",
	"tab amend",
	"arrow keys",
	"↑/↓",
}

// composer is how one agent draws its own input line.
type composer struct {
	// marker is the character at the start of the line the user types into.
	marker rune
	// bordered is true when the harness draws rules above and below the box.
	//
	// Measured, not assumed: claude and agy both bracket their box with a rule
	// (`──────` over `❯`/`>` over `──────`, captured live on 2026-10-13), and codex does not
	// (its box is a bare `›` line under the transcript). It is worth knowing because it
	// answers a question the marker alone cannot: claude's *startup questions* draw their
	// options with the very same `❯` and sit at the bottom of the screen, so a marker with no
	// rule above it is a list rather than a box — and typing into a list drives it.
	bordered bool
}

// composers is the table, and every entry is a capture rather than a taste.
//
// The three agents here are the ones this machine has run and can be read from. An
// agent that is missing is missing on purpose: adding one means capturing a live pane
// first, empty and with a draft, because the difference between "the box is empty"
// and "the box has text" is exactly what must not be guessed.
var composers = map[string]composer{
	// Border-wrapped box, empty is the marker alone:
	//   ────────────────────────
	//   >
	//   ────────────────────────
	//   ? for shortcuts                       Gemini Flash · high
	"agy": {marker: '>', bordered: true},

	// Same shape, different glyph (the manifest names it in `live_prompt_box`).
	"claude": {marker: '❯', bordered: true},

	"codex": {marker: '›'},
}

func parseStyled(line string) (visible []rune, dimFlags []bool) {
	dim := false
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		if runes[i] == 0x1b {
			consumed, next := applySGR(runes[i:], dim)
			if consumed > 0 {
				dim = next
				i += consumed - 1
				continue
			}
			// An escape sequence that is not an SGR (a cursor move, a title): skip it
			// so it does not end up in the text the user is shown.
			if skipped := skipEscape(runes[i:]); skipped > 0 {
				i += skipped - 1
			}
			continue
		}
		visible = append(visible, runes[i])
		dimFlags = append(dimFlags, dim)
	}
	return visible, dimFlags
}

// applySGR consumes an SGR sequence at the start of runes, if that is what it is.
func applySGR(runes []rune, dim bool) (consumed int, next bool) {
	if len(runes) < 3 || runes[1] != '[' {
		return 0, dim
	}
	for i := 2; i < len(runes); i++ {
		if runes[i] == 'm' {
			return i + 1, applyParams(runes[2:i], dim)
		}
		if !strings.ContainsRune("0123456789;", runes[i]) {
			return 0, dim
		}
	}
	return 0, dim
}

// applyParams folds one SGR parameter list into the dim flag.
//
// The parameters are read as a sequence rather than as independent flags, because
// `38`, `48` and `58` are followed by arguments of their own — `48;2;r;g;b` is a
// 24-bit background colour, and its "2" means "truecolour", not "dim". Only a `2`
// standing on its own selects faint text, and a bare `ESC[m` is a reset.
func applyParams(params []rune, dim bool) bool {
	if len(params) == 0 {
		return false
	}
	fields := strings.Split(string(params), ";")
	for i := 0; i < len(fields); i++ {
		switch strings.TrimSpace(fields[i]) {
		case "", "0":
			dim = false
		case "2":
			dim = true
		case "22":
			dim = false
		case "38", "48", "58":
			// Colour: skip this parameter's arguments (`5;n` or `2;r;g;b`).
			i = skipColourArgs(fields, i)
		}
	}
	return dim
}

// skipColourArgs returns the index of the last parameter belonging to a 38/48/58
// colour that starts at index i.
func skipColourArgs(fields []string, i int) int {
	switch strings.TrimSpace(fieldAt(fields, i+1)) {
	case "5":
		return i + 2
	case "2":
		return i + 4
	default:
		return i
	}
}

func fieldAt(fields []string, i int) string {
	if i < 0 || i >= len(fields) {
		return ""
	}
	return fields[i]
}

// skipEscape measures a non-SGR escape sequence so it can be dropped.
func skipEscape(runes []rune) int {
	if len(runes) < 2 {
		return 0
	}
	switch runes[1] {
	case ']': // OSC ... BEL  or  OSC ... ESC \
		for i := 2; i < len(runes); i++ {
			if runes[i] == 0x07 {
				return i + 1
			}
			if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '\\' {
				return i + 2
			}
		}
		return len(runes)
	default:
		// CSI/SS3 and other short forms: consume to the final byte.
		for i := 2; i < len(runes); i++ {
			if runes[i] >= 0x40 && runes[i] <= 0x7e {
				return i + 1
			}
		}
		return len(runes)
	}
}
