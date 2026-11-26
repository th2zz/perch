// Package herdr is a typed client for the herdr socket API.
//
// Wire shapes in this file are transcribed from the schema bundled with herdr
// 0.9.1 (`herdr api schema`), not from prose documentation. Field names and
// required-ness therefore match the running server rather than the website.
package herdr

import "encoding/json"

// AgentStatus is herdr's own classification of an agent pane. The product MUST
// NOT re-derive this (constitution I); it is consumed verbatim.
type AgentStatus string

const (
	StatusIdle    AgentStatus = "idle"
	StatusWorking AgentStatus = "working"
	StatusBlocked AgentStatus = "blocked"
	StatusDone    AgentStatus = "done"
	StatusUnknown AgentStatus = "unknown"
)

// allAgentStatuses is every agent status herdr defines that this package recognizes (D55 ④).
var allAgentStatuses = [...]AgentStatus{
	StatusIdle,
	StatusWorking,
	StatusBlocked,
	StatusDone,
	StatusUnknown,
}

// AllAgentStatuses returns a copy of every agent status herdr defines that this package recognizes (D55 ④).
func AllAgentStatuses() []AgentStatus {
	out := make([]AgentStatus, len(allAgentStatuses))
	copy(out, allAgentStatuses[:])
	return out
}

// NeedsDecision reports whether herdr is telling us a human is required.
//
// Only "blocked" qualifies. "unknown" is explicitly NOT treated as blocked or
// as done — spec's edge case requires unknown to stay honest.
func (s AgentStatus) NeedsDecision() bool { return s == StatusBlocked }

// AgentSession ties a pane to the vendor's own session identity.
type AgentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"` // "id" | "path"
	Value  string `json:"value"`
}

// ID is the harness's own name for its session, when that is what herdr reported.
//
// herdr sends two kinds and they are not interchangeable: "id" is an identity (codex's rollout
// id, which is also the name of its record file), and "path" is a location. Only the first can be
// handed to a reader as "this is which session"; a path read as an id would send the reader
// looking for a session named after a file.
func (s *AgentSession) ID() string {
	if s == nil || s.Kind != "id" {
		return ""
	}
	return s.Value
}

// Workspace mirrors herdr's WorkspaceInfo.
type Workspace struct {
	WorkspaceID string            `json:"workspace_id"`
	Number      uint              `json:"number"`
	Label       string            `json:"label"`
	Focused     bool              `json:"focused"`
	PaneCount   uint              `json:"pane_count"`
	TabCount    uint              `json:"tab_count"`
	ActiveTabID string            `json:"active_tab_id"`
	AgentStatus AgentStatus       `json:"agent_status"`
	Tokens      map[string]string `json:"tokens,omitempty"`
}

// Tab mirrors herdr's TabInfo.
type Tab struct {
	TabID       string      `json:"tab_id"`
	WorkspaceID string      `json:"workspace_id"`
	Number      uint        `json:"number"`
	Label       string      `json:"label"`
	Focused     bool        `json:"focused"`
	PaneCount   uint        `json:"pane_count"`
	AgentStatus AgentStatus `json:"agent_status"`
}

// Pane mirrors herdr's PaneInfo. Pointer fields are optional on the wire and
// MUST stay distinguishable from empty strings.
type Pane struct {
	PaneID                string            `json:"pane_id"`
	TerminalID            string            `json:"terminal_id"`
	WorkspaceID           string            `json:"workspace_id"`
	TabID                 string            `json:"tab_id"`
	Focused               bool              `json:"focused"`
	Agent                 *string           `json:"agent"`
	DisplayAgent          *string           `json:"display_agent"`
	AgentStatus           AgentStatus       `json:"agent_status"`
	AgentSession          *AgentSession     `json:"agent_session,omitempty"`
	Cwd                   *string           `json:"cwd"`
	ForegroundCwd         *string           `json:"foreground_cwd"`
	Title                 *string           `json:"title"`
	TerminalTitle         *string           `json:"terminal_title"`
	TerminalTitleStripped *string           `json:"terminal_title_stripped"`
	StateLabels           map[string]string `json:"state_labels,omitempty"`
	Tokens                map[string]string `json:"tokens,omitempty"`
	Revision              uint64            `json:"revision"`
}

// Agent mirrors herdr's AgentInfo.
type Agent struct {
	Agent             *string           `json:"agent"`
	DisplayAgent      *string           `json:"display_agent"`
	Name              *string           `json:"name"`
	AgentStatus       AgentStatus       `json:"agent_status"`
	PaneID            string            `json:"pane_id"`
	TabID             string            `json:"tab_id"`
	WorkspaceID       string            `json:"workspace_id"`
	Focused           bool              `json:"focused"`
	Cwd               *string           `json:"cwd"`
	ForegroundCwd     *string           `json:"foreground_cwd"`
	Title             *string           `json:"title"`
	TerminalTitle     *string           `json:"terminal_title"`
	StateLabels       map[string]string `json:"state_labels,omitempty"`
	Revision          uint64            `json:"revision"`
	StateChangeSeq    uint64            `json:"state_change_seq"`
	InteractiveReady  bool              `json:"interactive_ready"`
	LaunchPending     bool              `json:"launch_pending"`
	AgentSession      *AgentSession     `json:"agent_session,omitempty"`
	ScreenDetectSkipd bool              `json:"screen_detection_skipped"`
}

// Snapshot is the result of session.snapshot: one authoritative read of the
// whole session. The client's mirror is always rebuilt from this, never by
// replaying events on top of stale state (data-model.md §3).
type Snapshot struct {
	Version    string            `json:"version"`
	Protocol   uint32            `json:"protocol"`
	Workspaces []Workspace       `json:"workspaces"`
	Tabs       []Tab             `json:"tabs"`
	Panes      []Pane            `json:"panes"`
	Layouts    []json.RawMessage `json:"layouts"`
	Agents     []Agent           `json:"agents"`
}

// PaneReadResult mirrors herdr's PaneReadResult / agent.read result.
type PaneReadResult struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Source      string `json:"source"`
	Format      string `json:"format"`
	Text        string `json:"text"`
	Revision    uint64 `json:"revision"`
	Truncated   bool   `json:"truncated"`
}

// PaneProcess is one foreground process of a pane, as `pane.process_info` reports it.
type PaneProcess struct {
	Argv []string `json:"argv"`
	PID  int      `json:"pid"`
	Cwd  string   `json:"cwd"`
}

// Pong is the result of the `ping` method; it is the cheapest authoritative
// statement of server version and protocol.
type Pong struct {
	Type     string `json:"type"`
	Version  string `json:"version"`
	Protocol uint32 `json:"protocol"`
}

// Str returns the pointed-to string, or "" when unset.
func Str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
