package herdr

import (
	"path/filepath"
	"strings"
)


// Program is the herdr executable's name, used when we have to run it ourselves.
const Program = "herdr"

// Socket methods.
const (
	MethodPing            = "ping"
	MethodSessionSnapshot = "session.snapshot"
	MethodPaneGet         = "pane.get"
	MethodPaneLayout      = "pane.layout"
	MethodPaneProcessInfo = "pane.process_info"
	MethodPaneRead        = "pane.read"
	MethodPaneSendKeys    = "pane.send_keys"
	MethodPaneSendText    = "pane.send_text"
	MethodPaneSendInput   = "pane.send_input"
	MethodPaneRename      = "pane.rename"
	MethodPaneClose       = "pane.close"
	MethodWorkspaceCreate = "workspace.create"
	MethodAgentRead       = "agent.read"
	MethodAgentSendKeys   = "agent.send_keys"
	MethodAgentPrompt     = "agent.prompt"
	MethodAgentStart      = "agent.start"
	MethodTabCreate       = "tab.create"
	MethodWorkspaceRename = "workspace.rename"
	MethodWorkspaceClose  = "workspace.close"
	MethodEventsSubscribe = "events.subscribe"
)

// Event types this bridge subscribes to.
//
// There is no unsubscribe call to declare: herdr ends a subscription when the connection carrying it
// closes, and the reconciler drops the connection rather than asking nicely.
//
// **Two spellings, one meaning.** The socket names an event with a dot (`pane.created`); the plugin
// hook herdr runs for the same event spells it with an underscore (`pane_created`). Which one arrives
// depends on which door it came through, not on what happened — so both spellings are declared here,
// and the reconciler asks what happened (the predicates below) instead of spelling either form.
// Measured on herdr 0.9.1, 2026-10-15.
const (
	EventPaneCreated            = "pane.created"
	EventPaneClosed             = "pane.closed"
	EventPaneExited             = "pane.exited"
	EventPaneAgentDetected      = "pane.agent_detected"
	EventPaneAgentStatusChanged = "pane.agent_status_changed"
	EventEventsLost             = "events.lost"

	// The hook spelling of the same six events. The `Hook` suffix is load-bearing: boundary_test.go
	// bans the value of every constant that ends in it, the same way it bans the dotted ones.
	//
	// pane_exited is also the spelling the *subscription stream* delivers for this event, even though
	// the schema names the subscription pane.exited (measured 2026-10-17, issue #124: exiting the shell
	// inside a pane emits pane_exited, never pane_closed).
	EventPaneCreatedHook            = "pane_created"
	EventPaneClosedHook             = "pane_closed"
	EventPaneExitedHook             = "pane_exited"
	EventPaneAgentDetectedHook      = "pane_agent_detected"
	EventPaneAgentStatusChangedHook = "pane_agent_status_changed"
	EventEventsLostHook             = "events_lost"
)

// StatusChangeEvent reports whether an event kind means "an agent's status changed".
func StatusChangeEvent(kind string) bool {
	return kind == EventPaneAgentStatusChanged || kind == EventPaneAgentStatusChangedHook
}

// LifecycleEvent reports whether an event kind says the pane inventory may have changed: a pane
// appearing or disappearing, or an agent taking one over.
//
// An exit is a disappearance: herdr removes the pane and says pane_exited (#124). Subscribe to it
// too, or a pane whose process ended waits for the periodic reconcile instead.
func LifecycleEvent(kind string) bool {
	switch kind {
	case EventPaneCreated, EventPaneCreatedHook,
		EventPaneClosed, EventPaneClosedHook,
		EventPaneExited, EventPaneExitedHook,
		EventPaneAgentDetected, EventPaneAgentDetectedHook:
		return true
	}
	return false
}

// EventsLostEvent reports whether an event kind says a subscriber fell behind, which forces a full
// re-read rather than a resumption.
func EventsLostEvent(kind string) bool {
	return kind == EventEventsLost || kind == EventEventsLostHook
}

// CLI groups: the subcommand namespace, and the thing `--machine` switches machines for.
const (
	GroupMachine   = "machine"
	GroupAPI       = "api"
	GroupPlugin    = "plugin"
	GroupPane      = "pane"
	GroupWorkspace = "workspace"
	GroupTab       = "tab"
	GroupAgent     = "agent"
	GroupTerminal  = "terminal"
	GroupSession   = "session"
)

// CLI verbs: the leaf command inside a group, or a top-level command when used alone.
const (
	VerbStatus   = "status"
	VerbObserve  = "observe"
	VerbList     = "list"
	VerbSnapshot = "snapshot"
	VerbExplain  = "explain"
	VerbCreate   = "create"
	VerbRename   = "rename"
	VerbClose    = "close"
	VerbStart    = "start"
	VerbSendKeys = "send-keys"
	VerbSendText = "send-text"
	VerbRun      = "run"
	VerbPrompt   = "prompt"
	VerbRead     = "read"
	VerbUnlink   = "unlink"
	VerbControl  = "control"
)

// CLI flags.
const (
	FlagMachine   = "--machine"
	FlagClear     = "--clear"
	FlagCwd       = "--cwd"
	FlagEnv       = "--env"
	FlagFormat    = "--format"
	FlagFocus     = "--focus"
	FlagKind      = "--kind"
	FlagLabel     = "--label"
	FlagLines     = "--lines"
	FlagNoFocus   = "--no-focus"
	FlagPane      = "--pane"
	FlagCols      = "--cols"
	FlagRows      = "--rows"
	FlagTimeout   = "--timeout"
	FlagWorkspace = "--workspace"
	FlagJSON      = "--json"
)

// The environment herdr sets on a process it spawns: which pane we are inside, where its socket is,
// where a plugin keeps state, and the JSON of the event that woke a hook process. HERDR_SOCKET_PATH
// and HERDR_BIN_PATH are declared in transport.go, next to the code that reads them.
const (
	EnvSessionFlag     = "HERDR_ENV"
	EnvPluginStateDir  = "HERDR_PLUGIN_STATE_DIR"
	EnvPluginEvent     = "HERDR_PLUGIN_EVENT"
	EnvPluginEventJSON = "HERDR_PLUGIN_EVENT_JSON"
)

// sessionScoped names the variables that say *which pane this process is inside*, as exact names or
// prefixes. A terminal the phone opens must not inherit them: an inherited HERDR_ENV makes herdr
// itself refuse to run ("already inside a herdr session"). HERDR_BIN_PATH is deliberately absent —
// it says where herdr's binary is, not where we are, so a shell keeps it.
var sessionScoped = []string{EnvSessionFlag, "HERDR_PANE", "HERDR_TAB", "HERDR_WORKSPACE", EnvSocketPath}

// IsSessionEnv reports whether a variable name belongs to that set.
func IsSessionEnv(name string) bool {
	for _, prefix := range sessionScoped {
		if name == prefix || strings.HasPrefix(name, prefix+"_") {
			return true
		}
	}
	return false
}

// Separator joins two commands inside one display sentence.
const Separator = "&&"

// Where herdr keeps things on disk. Only herdr decides this layout; when it moves, it moves here.
func ConfigDir(home string) string { return filepath.Join(home, ".config", Program) }

// SocketPath is the default endpoint under the config directory.
func SocketPath(home string) string { return filepath.Join(ConfigDir(home), Program+".sock") }

// PluginsRegistryPath is herdr's registry of linked plugins (`plugins.json`).
func PluginsRegistryPath(home string) string { return filepath.Join(ConfigDir(home), "plugins.json") }

// PluginsDir is the directory herdr links plugins into.
func PluginsDir(home string) string { return filepath.Join(ConfigDir(home), "plugins") }

// PluginStateDir is the state directory herdr creates for one linked plugin.
func PluginStateDir(home, pluginID string) string {
	return filepath.Join(home, ".local", "state", Program, "plugins", pluginID)
}

// DetectionDirs are the two directories herdr reads agent detection rules from: the state copy it
// ships and the user's own overrides.
func DetectionDirs(home string) []string {
	return []string{
		filepath.Join(home, ".local", "state", Program, "agent-detection"),
		filepath.Join(ConfigDir(home), "agent-detection"),
	}
}
