package core

import (
	"strings"
	"testing"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

// Every fixture here is a capture, not an invention.
//
// The three agents were started in a throwaway herdr session on this machine on
// 2026-10-13 and read with `pane read --source visible --format ansi --raw`, empty and
// then with a sentence typed at them without pressing Enter. The escape sequences are
// the terminal's, copied verbatim: for codex they are the whole point (its empty input
// line is a *dim* placeholder, and its typed text is not dim), and for agy there was
// nothing to copy but the marker.
const (
	// codex, empty.
	codexEmpty = "\x1b[0m\x1b[1m› \x1b[0m\x1b[2mAsk Codex to do anything\x1b[0m"
	// codex, after `pane send-text` without Enter.
	codexDraft = "\x1b[0m\x1b[1m› \x1b[0mpartial unsent draft"
	// agy, empty: the marker alone between two border rows, with its footer under it.
	agyEmpty = " ┃ preferred verbosity anytime in /config > Verbosity.\r\n" +
		"────────────────────────────────────────\r\n" +
		">\r\n" +
		"────────────────────────────────────────\r\n" +
		"? for shortcuts                     Gemini Flash · high"
	// agy, after `pane send-text` without Enter.
	agyDraft = " ┃ esc to dismiss\r\n" +
		"────────────────────────────────────────\r\n" +
		"> partial agy draft\r\n" +
		"────────────────────────────────────────\r\n" +
		"                                                    "
	// agy's startup dialog, captured while the agent was blocked. The selected option
	// uses the same `>` marker as the input line, which is exactly why this function
	// only looks when the agent is somewhere we would submit for the user.
	agyTrustDialog = "Do you trust the contents of this project?\r\n" +
		"\r\n" +
		"> Yes, I trust this folder\r\n" +
		"  No, exit\r\n" +
		"\r\n" +
		"  ↑/↓ Navigate · enter Confirm\r\n" +
		"                                     Gemini Flash · high"

	codexModelPicker = "\r\n\r\n\r\n  \x1b[0m\x1b[1mSelect Model and Effort\x1b[0m\r\n \r\n\r\n" +
		"\x1b[0m\x1b[1m\x1b[7m› 1. DeepSeek-Flash (current)  \x1b[0m\x1b[7mLatest frontier agentic coding model with image input.\x1b[0m\x1b[1m\x1b[7m                                   \x1b[0m\r\n" +
		"  2. DeepSeek-V4-Pro           \x1b[0m\x1b[2mMost capable frontier agentic coding model.\x1b[0m\r\n" +
		"\r\n" +
		"  \x1b[0m\x1b[1menter\x1b[0m\x1b[2m select · \x1b[0m\x1b[1mesc\x1b[0m\x1b[2m back                                                                                               \x1b[0m"

	// `/help`, sent the same way on the same machine: the agent's own completion list
	// swallowed the Enter, so the command is sitting in the input line — which is exactly
	// the state the guard exists to report, because a following send would append to it.
	codexCommandStranded = "› /help\r\n" +
		"\r\n" +
		"  DeepSeek-Flash high · Context 0% used"

	codexWorkingFurniture = "• Let me do that first. (42s • esc to interrupt)\r\n" +
		"  └ Tip: Press tab to queue a message when a task is running; otherwise it sends immediately (except !).\r\n" +
		"\r\n" +
		"\r\n" +
		"› Ask Codex to do anything\r\n" +
		"\r\n" +
		"  DeepSeek-Flash high · Configure Codex Session · Context 41% used · 6.8M used                                        Goal stalled (/goal resume)\r\n" +
		"  ← for agents · ? for shortcuts                                                                                    ⚠ 1 warning · f2 to view"

	agyIdleWithFooter = "  You can check project progress directly in README.md.\r\n" +
		"\r\n" +
		"───────────────────────────────────────\r\n" +
		">\r\n" +
		"───────────────────────────────────────\r\n" +
		"? for shortcuts                                                          Gemini Flash · high"
)

func TestUnsentInComposer(t *testing.T) {
	cases := []struct {
		name   string
		kind   string
		status herdr.AgentStatus
		styled string
		want   string
		why    string
	}{
		{
			name: "codex with a sentence waiting",
			kind: "codex", status: herdr.StatusIdle, styled: codexDraft,
			want: "partial unsent draft",
			why:  "the bug this exists for: the terminal is holding text that a send would append to",
		},
		{
			name: "agy with a sentence waiting",
			kind: "agy", status: herdr.StatusIdle, styled: agyDraft,
			want: "partial agy draft",
			why:  "the same case in an agy session",
		},
		{
			name: "agy done, so still the state we submit in",
			kind: "agy", status: herdr.StatusDone, styled: agyDraft,
			want: "partial agy draft",
			why:  "done means ready for input, so a prompt would still submit",
		},
		{
			name: "codex with an empty input line",
			kind: "codex", status: herdr.StatusIdle, styled: codexEmpty,
			want: "",
			why:  "the placeholder is drawn dim; the terminal is saying it is a hint, not text",
		},
		{
			name: "agy with an empty input line",
			kind: "agy", status: herdr.StatusIdle, styled: agyEmpty,
			want: "",
			why:  "the marker alone is an empty box",
		},
		{
			name: "a blocked agent is left alone",
			kind: "agy", status: herdr.StatusBlocked, styled: agyTrustDialog,
			want: "",
			why:  "a dialog's selected option uses the same marker; a prompt is not sent there anyway",
		},
		{
			name: "a working agent is left alone",
			kind: "codex", status: herdr.StatusWorking, styled: codexDraft,
			want: "",
			why:  "while working, that space can show queued work and transcript rows, and we do not submit",
		},
		{
			name: "an agent nobody has captured is left alone",
			kind: "gemini", status: herdr.StatusIdle, styled: agyDraft,
			want: "",
			why:  "adding an agent means capturing its live pane first; guessing is what this product does not do",
		},
		{
			name: "a shell is left alone",
			kind: "", status: herdr.StatusIdle, styled: "[user@host tmp]$ ls",
			want: "",
			why:  "text delivery to a shell does not submit, so nothing merges silently",
		},
		{
			name: "a pane with nothing recognisable is left alone",
			kind: "codex", status: herdr.StatusIdle, styled: "just some output\nand more of it",
			want: "",
			why:  "no input line on screen means nothing to say, rather than a guess",
		},
		{
			name: "an old prompt above the input line is not the input line",
			kind: "codex", status: herdr.StatusIdle,
			styled: "› partial unsent draft\r\n" + rep("filler line\r\n", 12) + "  ? for shortcuts",
			want:   "",
			why:    "the scan is bounded to the bottom of the screen, so an old prompt cannot be reported as waiting",
		},
		{
			name: "a menu is not an input line",
			kind: "codex", status: herdr.StatusIdle, styled: codexModelPicker,
			want: "",
			why:  "the selected option uses the same marker as the box, and herdr calls this pane idle — measured off the wire, escapes and all",
		},
		{
			name: "a command the agent's own menu swallowed is reported",
			kind: "codex", status: herdr.StatusIdle, styled: codexCommandStranded,
			want: "/help",
			why:  "this is the state a slash command can leave behind, and a send would be appended to it",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReadComposer(tc.kind, tc.status, tc.styled).Unsent
			if got != tc.want {
				t.Errorf("ReadComposer(%q, %q).Unsent = %q, want %q\n  %s", tc.kind, tc.status, got, tc.want, tc.why)
			}
		})
	}
}

func TestACommandInTheComposerIsReportedAsACommand(t *testing.T) {
	command := "\x1b[0m\x1b[1m› \x1b[0m/goal "
	got := ReadComposer("codex", herdr.StatusIdle, command)
	if got.Unsent != "/goal" {
		t.Fatalf("Unsent = %q, want %q", got.Unsent, "/goal")
	}
	if !got.Command {
		t.Error("a slash command is not reported as a command; the app would warn about the wrong thing")
	}

	sentence := ReadComposer("codex", herdr.StatusIdle, codexDraft)
	if sentence.Command {
		t.Error("a sentence is reported as a command; the app would refuse an ordinary send")
	}
}

// The styling is what separates a hint from a sentence, so the parser that reports it is
// worth pinning on its own: a reset that fails to clear dim would turn every empty input
// line into a warning, and a background colour misread as dim would hide a real draft.
// `style` is the mask of the line, N for normal intensity and D for dim.
func TestSplitStyled(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		visible string
		style   string
	}{
		{"plain", "half a sentence", "half a sentence", "NNNNNNNNNNNNNNN"},
		{
			"a dim placeholder is not typed text",
			"\x1b[1m› \x1b[0m\x1b[2mAsk Codex to do anything\x1b[0m",
			"› Ask Codex to do anything",
			"NN" + strings.Repeat("D", len("Ask Codex to do anything")),
		},
		{
			"dim off, then text: 22 is the other way to stop being dim",
			"\x1b[2mhint\x1b[22mabc",
			"hintabc",
			"DDDDNNN",
		},
		{
			"a bare escape-m is a reset",
			"\x1b[2mhint\x1b[mtext",
			"hinttext",
			"DDDDNNNN",
		},
		{
			"a truecolour background says nothing about who wrote it",
			"\x1b[38;5;12m> \x1b[48;2;41;41;41mabc\x1b[0m",
			"> abc",
			"NNNNN",
		},
		{
			"a cursor move is dropped rather than shown",
			"\x1b[2Jhalf",
			"half",
			"NNNN",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			visible, flags := parseStyled(tc.line)
			if got := string(visible); got != tc.visible {
				t.Errorf("visible = %q, want %q", got, tc.visible)
			}
			mask := ""
			for _, flag := range flags {
				if flag {
					mask += "D"
				} else {
					mask += "N"
				}
			}
			if mask != tc.style {
				t.Errorf("style = %q, want %q (%q)", mask, tc.style, tc.line)
			}
		})
	}
}

func rep(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

// A menu has to be *reported*, not just ignored.
//
// Silence about unsent text is right — the option rows are the agent's words — but the
// phone must not then keep typing into it. Measured consequence of the first version, which
// only stayed silent: a sentence sent while the model picker was open **advanced the picker
// to the next question** (the reasoning-level list). So the answer has to carry the fact
// back to the app, which refuses to send and says why.
func TestAMenuIsReportedRatherThanIgnored(t *testing.T) {
	got := ReadComposer("codex", herdr.StatusIdle, codexModelPicker)
	if !got.Menu {
		t.Errorf("the picker must be reported as a menu, got %+v", got)
	}
	if got.Unsent != "" {
		t.Errorf("a menu is not somebody's draft: %q", got.Unsent)
	}

	// And an ordinary input line is not a menu.
	if plain := ReadComposer("codex", herdr.StatusIdle, codexDraft); plain.Menu {
		t.Errorf("a sentence in the box is not a menu: %+v", plain)
	}
}

func TestTheFurnitureAndTheFooterAreMeasured(t *testing.T) {
	working := ReadComposer("codex", herdr.StatusWorking, codexWorkingFurniture)
	if working.ChromeLines != 4 {
		t.Errorf("codex furniture = %d lines, want 4 (box, blank, two footer rows)", working.ChromeLines)
	}
	want := "DeepSeek-Flash high · Configure Codex Session · Context 41% used · 6.8M used · Goal stalled (/goal resume)\n" +
		"← for agents · ? for shortcuts · ⚠ 1 warning · f2 to view"
	if working.Status != want {
		t.Errorf("codex footer:\n got %q\nwant %q", working.Status, want)
	}
	if working.Unsent != "" {
		t.Errorf("a dim placeholder is not somebody's draft: %q", working.Unsent)
	}

	agy := ReadComposer("agy", herdr.StatusIdle, agyIdleWithFooter)
	if agy.ChromeLines != 4 {
		t.Errorf("agy furniture = %d lines, want 4 (rule, box, rule, footer)", agy.ChromeLines)
	}
	if agy.Status != "? for shortcuts · Gemini Flash · high" {
		t.Errorf("agy footer = %q", agy.Status)
	}

	// The transcript itself is not furniture: the line above the box must survive.
	if strings.Contains(agy.Status, "README") || strings.Contains(working.Status, "queue a message") {
		t.Errorf("the footer swallowed something the agent said: %q / %q", agy.Status, working.Status)
	}
}

// claude's first-run wizard is not a prompt box, however much it looks like one.
//
// Captured from a live pane (2026-10-13) while herdr reported the agent as **idle**: the
// theme list marks its selection with the same `❯` as claude's prompt box, and its footer —
// "Syntax theme: Monokai Extended (ctrl+t to disable)" — carries none of the menu phrases
// the hint list looks for.
//
// Two sizes are tested, because they are excluded by two different things and it would be
// easy to believe the wrong one is doing the work. At this size the marker is thirteen lines
// above the bottom, so the twelve-row window already excludes it; the short-pane fixture
// below is the one that needs `composerNearEnd`.
//
// The probe (lab/probe-harness.sh) is what surfaced the screen in the first place: it printed
// claude's real terminal and reported NOT READY, which is exactly what it is for.
const claudeFirstRunWizard = " Welcome to Claude Code v2.1.296\r\n" +
	"\r\n" +
	" Let's get started.\r\n" +
	"\r\n" +
	" Choose the text style that looks best with your terminal\r\n" +
	" To change this later, run /theme\r\n" +
	"\r\n" +
	"     Auto (match terminal)\r\n" +
	" ❯ ✔ Dark mode\r\n" +
	"     Light mode\r\n" +
	"     Dark mode (colorblind-friendly)\r\n" +
	"     Light mode (colorblind-friendly)\r\n" +
	"     Dark mode (ANSI colors only)\r\n" +
	"     Light mode (ANSI colors only)\r\n" +
	"\r\n" +
	" ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\r\n" +
	"  1  function greet() {\r\n" +
	"  2 -  console.log(\"Hello, World!\");\r\n" +
	"  2 +  console.log(\"Hello, Claude!\");\r\n" +
	"  3  }\r\n" +
	" ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\r\n" +
	"  Syntax theme: Monokai Extended (ctrl+t to disable)"

func TestClaudeFirstRunWizardIsNotADraft(t *testing.T) {
	got := ReadComposer("claude", herdr.StatusIdle, claudeFirstRunWizard)
	if got.Unsent != "" {
		t.Errorf("the theme list is not somebody's draft: %q", got.Unsent)
	}
	if got.Menu {
		// Claiming a menu here would be guessing too: it has no hint row to prove it, and
		// saying nothing is the safe answer for a screen we merely do not recognise.
		t.Log("(recognised as a menu as well — harmless, but not required)")
	}
}

// The same list, on a pane short enough to bring it inside the search window.
//
// This is the fixture that proves `composerNearEnd` does something: the marker is ten lines
// above the bottom, so the twelve-row window includes it and only the near-the-end bound
// rejects it. Without that bound this screen — which herdr calls `idle` — is reported as
// somebody's unsent draft, and the app would ask the user to confirm appending to a theme.
const claudeWizardInAShortPane = " ❯ ✔ Dark mode\r\n" +
	"     Light mode\r\n" +
	"     Dark mode (colorblind-friendly)\r\n" +
	"     Light mode (colorblind-friendly)\r\n" +
	"     Dark mode (ANSI colors only)\r\n" +
	"     Light mode (ANSI colors only)\r\n" +
	" ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\r\n" +
	"  1  function greet() {\r\n" +
	"  2 -  console.log(\"Hello, World!\");\r\n" +
	"  3  }\r\n" +
	" ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\r\n" +
	"  Syntax theme: Monokai Extended (ctrl+t to disable)"

func TestAListNearTheBottomIsStillNotADraft(t *testing.T) {
	got := ReadComposer("claude", herdr.StatusIdle, claudeWizardInAShortPane)
	if got.Unsent != "" {
		t.Errorf("a list of themes is not somebody's draft: %q", got.Unsent)
	}
}

// claude's own screen, captured plain off a live pane (2026-10-13), empty box and with a
// draft. Two things here are only visible in the real thing:
//
//   - the empty box is the marker followed by a *reverse-video space* (no dim, unlike codex),
//     so "empty" has to mean "nothing but spaces" rather than "nothing but dim text";
//   - the harness's status is split around the box: "● high · /effort" above the top rule,
//     the permissions line below the bottom one. Reading only downwards loses the model.
const claudePromptBoxEmpty = "  ▐▛███▛█   Claude Code v2.1.296\r\n" +
	" ▝▜██████▀  deepseek-flash · API Usage Billing\r\n" +
	"  ▝▝   ▝▝   /tmp\r\n" +
	"                                                                        ● high · /effort\r\n" +
	"───────────────────────────────────────────────────────────────────────────────────────\r\n" +
	"\x1b[0m\x1b[7m❯ \x1b[0m\r\n" +
	"───────────────────────────────────────────────────────────────────────────────────────\r\n" +
	"  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents"

const claudePromptBoxDraft = "  ▐▛███▛█   Claude Code v2.1.296\r\n" +
	"                                                                        ● high · /effort\r\n" +
	"───────────────────────────────────────────────────────────────────────────────────────\r\n" +
	"❯ perch probe sentinel 123\r\n" +
	"───────────────────────────────────────────────────────────────────────────────────────\r\n" +
	"  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents"

func TestClaudeBoxIsReadAndItsStatusComesFromBothSides(t *testing.T) {
	empty := ReadComposer("claude", herdr.StatusIdle, claudePromptBoxEmpty)
	if empty.Unsent != "" || empty.Menu {
		t.Errorf("an empty box says nothing: %+v", empty)
	}
	// Four, not three: the rule *above* the box is furniture too, and the reading view drops
	// it with the rest. (The first version of this expectation said three, which is what a
	// test written from the code rather than from the screen looks like.)
	if empty.ChromeLines != 4 {
		t.Errorf("claude furniture = %d lines, want 4 (rule above, box, rule below, footer)", empty.ChromeLines)
	}
	if want := "● high · /effort\n⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents"; empty.Status != want {
		t.Errorf("claude status:\n got %q\nwant %q", empty.Status, want)
	}

	draft := ReadComposer("claude", herdr.StatusIdle, claudePromptBoxDraft)
	if draft.Unsent != "perch probe sentinel 123" {
		t.Errorf("claude draft = %q", draft.Unsent)
	}
}

// A question is not a box, and this is the only thing that says so.
//
// claude's startup questions draw their options with the same `❯` as its box, at the bottom
// of the screen, while herdr reports the pane as `idle` — captured: "❯ Yes, set auto mode as
// my default permission mode". The box is always bracketed by rules and a list is not, so a
// marker without its rules is reported as a menu: the app refuses to type, which is the
// answer that keeps a sentence out of somebody's settings dialog.
func TestAMarkerWithoutItsRulesIsNotABox(t *testing.T) {
	question := " Set auto mode as my default permission mode?\r\n" +
		"\r\n" +
		" ❯ Yes, set auto mode as my default permission mode\r\n" +
		"   No, keep asking me\r\n" +
		"\r\n" +
		"  Enter to confirm · Esc to cancel"

	got := ReadComposer("claude", herdr.StatusIdle, question)
	if got.Unsent != "" {
		t.Errorf("an option row is not somebody's draft: %q", got.Unsent)
	}
	if !got.Menu {
		t.Errorf("a list at the bottom must be reported as a menu, not ignored: %+v", got)
	}
}
