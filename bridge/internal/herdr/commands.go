package herdr

import (
	"sort"
	"strings"
)


// RenamePaneCommand is `herdr pane rename <pane> <label>`, or `--clear` when the label is empty.
func RenamePaneCommand(paneID, label string) []string {
	if label == "" {
		return []string{GroupPane, VerbRename, paneID, FlagClear}
	}
	return []string{GroupPane, VerbRename, paneID, label}
}

// RenameWorkspaceCommand is `herdr workspace rename <workspace> <label>`.
func RenameWorkspaceCommand(workspaceID, label string) []string {
	return []string{GroupWorkspace, VerbRename, workspaceID, label}
}

// ClosePaneCommand is `herdr pane close <pane>`.
func ClosePaneCommand(paneID string) []string {
	return []string{GroupPane, VerbClose, paneID}
}

// CloseWorkspaceCommand is `herdr workspace close <workspace>`.
//
// No `--close-group`: a group is a herdr idea about workspaces opened together, and closing more than the user
// pointed at is the kind of surprise this product refuses to spring on somebody.
func CloseWorkspaceCommand(workspaceID string) []string {
	return []string{GroupWorkspace, VerbClose, workspaceID}
}

func CreateWorkspaceCommand(cwd, label string, env map[string]string, focus bool) []string {
	out := []string{GroupWorkspace, VerbCreate, FlagCwd, cwd, FlagLabel, label}
	out = append(out, envFlags(env)...)
	if focus {
		return append(out, FlagFocus)
	}
	return append(out, FlagNoFocus)
}

func CreateTabCommand(workspaceID, cwd, label string, env map[string]string, focus bool) []string {
	out := []string{
		GroupTab, VerbCreate, FlagWorkspace, workspaceID, FlagCwd, cwd, FlagLabel, label,
	}
	out = append(out, envFlags(env)...)
	if focus {
		return append(out, FlagFocus)
	}
	return append(out, FlagNoFocus)
}

func envFlags(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		out = append(out, FlagEnv, key+"="+env[key])
	}
	return out
}

func StartAgentCommand(name, kind, paneID string, args []string) []string {
	out := []string{GroupAgent, VerbStart, name, FlagKind, kind, FlagPane, paneID}
	if len(args) > 0 {
		out = append(out, "--")
		out = append(out, args...)
	}
	return out
}

// SendKeysCommand is `herdr pane send-keys <pane> <keys…>`, the escape hatch's key bar.
func SendKeysCommand(paneID string, keys []string) []string {
	return append([]string{GroupPane, VerbSendKeys, paneID}, keys...)
}

// SendTextCommand is `herdr pane send-text <pane> <text>`: write text without submitting it.
func SendTextCommand(paneID, text string) []string {
	return []string{GroupPane, VerbSendText, paneID, text}
}

// RunCommand is `herdr pane run <pane> <text>`: write text and submit it with Enter, atomically.
func RunCommand(paneID, text string) []string {
	return []string{GroupPane, VerbRun, paneID, text}
}

// AgentSendKeysCommand is `herdr agent send-keys <target> <keys…>`.
//
// Distinct from [SendKeysCommand], which writes to a pane: this one goes through the agent-scoped call, which
// is what herdr documents for answering a dialog deliberately.
func AgentSendKeysCommand(target string, keys []string) []string {
	return append([]string{GroupAgent, VerbSendKeys, target}, keys...)
}

// AgentPromptCommand is `herdr agent prompt <target> <text>`.
func AgentPromptCommand(target, text string) []string {
	return []string{GroupAgent, VerbPrompt, target, text}
}

// Then joins command lines into the one sentence a person reads, using the separator the app displays.
func Then(cmds ...[]string) []string {
	var out []string
	for i, cmd := range cmds {
		if i > 0 {
			out = append(out, Separator)
		}
		out = append(out, cmd...)
	}
	return out
}

// WithProgram prefixes argv with the program name, for the `equivalent_command` field: the command line a
// person could paste into a shell to do the same thing by hand.
//
// Every command in a [Then] sentence gets the prefix, not just the first: `herdr pane send-text wA:p1 hi &&
// pane send-keys wA:p1 enter` is a sentence whose second half is `pane: command not found` when somebody
// pastes it. [RemoteArgs] drops the extra copies the same way it drops the leading one, so the SSH path is
// unchanged.
func WithProgram(args []string) []string {
	out := make([]string, 0, len(args)+1)
	out = append(out, Program)
	for _, arg := range args {
		out = append(out, arg)
		if arg == Separator {
			out = append(out, Program)
		}
	}
	return out
}

// RemoteArgs turns a display sentence back into the argv lists to run on a saved SSH machine.
//
// The program name is dropped because [CLI] supplies it, and [Separator] splits two commands. The caller
// passes only sentences this package built with [Then] — never a string parsed back out of user input — so
// this is a restatement of structure, not a parse.
func RemoteArgs(sentence []string) [][]string {
	var out [][]string
	var cur []string
	for _, arg := range sentence {
		switch arg {
		case Program:
			continue
		case Separator:
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

// Format renders one of the above the way a person would type it.
func Format(args []string) string {
	return Program + " " + strings.Join(args, " ")
}

func FormatShell(args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, Program)
	for _, arg := range args {
		parts = append(parts, ShellQuote(arg))
	}
	return strings.Join(parts, " ")
}

// ShellQuote renders one argv element so a shell reads it back as that same element.
func ShellQuote(arg string) string {
	if arg == "" {
		return "''"
	}
	if !strings.ContainsAny(arg, " \t\n'\"\\$`&|;<>()*?[]{}!#~") {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}

// QuoteArgs renders one argv list as a shell command line without a program prefix.
func QuoteArgs(args []string) string {
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = ShellQuote(arg)
	}
	return strings.Join(parts, " ")
}
