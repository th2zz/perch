package herdr

import (
	"context"
	"encoding/json"
	"fmt"
)

// Snapshot fetches the authoritative session state in one round trip. Every
// mirror rebuild and every reconciliation starts here.
func (c *Client) Snapshot(ctx context.Context) (*Snapshot, error) {
	var out struct {
		Type     string   `json:"type"`
		Snapshot Snapshot `json:"snapshot"`
	}
	if err := c.CallInto(ctx, MethodSessionSnapshot, nil, &out); err != nil {
		return nil, err
	}
	if out.Type != "session_snapshot" {
		return nil, fmt.Errorf("%s returned unexpected result type %q", MethodSessionSnapshot, out.Type)
	}
	return &out.Snapshot, nil
}

// GetPane returns one pane.
func (c *Client) GetPane(ctx context.Context, paneID string) (*Pane, error) {
	var out struct {
		Pane Pane `json:"pane"`
	}
	if err := c.CallInto(ctx, MethodPaneGet, map[string]string{"pane_id": paneID}, &out); err != nil {
		return nil, err
	}
	return &out.Pane, nil
}

// PaneGeometry reports how big a pane's terminal actually is, in cells.
//
// This is the number that makes a phone harmless. A pane has one terminal, and the program inside it
// lays its whole UI out for that shape; when a second client attaches and *changes* the shape, the
// program reflows — which is visible to whoever is sitting at the computer as their screen scrolling
// and then snapping. So the phone renders the pane's own geometry instead of imposing its own, and
// this is how it learns it.
//
// Measured on herdr 0.9.1 (2026-10-14): `pane.layout`'s rect is in cells, and it agrees with the
// terminal's own view — a pane whose `stty size` said `58 183` reports `width:183, height:58`, and the
// same 58 that `pane.get` reports as `scroll.viewport_rows`.
func (c *Client) PaneGeometry(ctx context.Context, paneID string) (cols, rows int, err error) {
	var out struct {
		Layout struct {
			Panes []struct {
				PaneID string `json:"pane_id"`
				Rect   struct {
					Width  int `json:"width"`
					Height int `json:"height"`
				} `json:"rect"`
			} `json:"panes"`
		} `json:"layout"`
	}
	if err := c.CallInto(ctx, MethodPaneLayout, map[string]string{"pane_id": paneID}, &out); err != nil {
		return 0, 0, err
	}
	for _, pane := range out.Layout.Panes {
		if pane.PaneID == paneID && pane.Rect.Width > 0 && pane.Rect.Height > 0 {
			return pane.Rect.Width, pane.Rect.Height, nil
		}
	}
	return 0, 0, fmt.Errorf("herdr's layout does not describe pane %s", paneID)
}

// PaneProcesses returns the foreground processes herdr sees in a pane.
//
// This is the link that makes reading a harness's own records possible without guessing: for
// codex the command line carries the session identifier, and for every harness the pid is
// enough to find the record file the process holds open.
func (c *Client) PaneProcesses(ctx context.Context, paneID string) ([]PaneProcess, error) {
	var out struct {
		// CallInto already unwraps the envelope's `result`, which is why this starts at
		// `process_info` and not one level higher. Getting that wrong decodes successfully and
		// leaves every field zero — the same trap the pane.read wrapper sprung earlier in this
		// project, and the reason a live check ran before the app was pointed at it.
		ProcessInfo struct {
			ForegroundProcesses []PaneProcess `json:"foreground_processes"`
		} `json:"process_info"`
	}
	if err := c.CallInto(ctx, MethodPaneProcessInfo, map[string]string{"pane_id": paneID}, &out); err != nil {
		return nil, err
	}
	return out.ProcessInfo.ForegroundProcesses, nil
}

// ReadPane reads recent output. Source is one of visible / recent /
// recent_unwrapped / detection; "recent" is the useful default for summaries.
//
// The result arrives wrapped: `{"type":"pane_read","read":{...}}`. Unwrapping it
// here rather than at every call site matters more than it looks — before this
// was noticed, every excerpt the product displayed was silently empty, because
// decoding a wrapper into the inner type succeeds and leaves every field zero.
func (c *Client) ReadPane(ctx context.Context, paneID, source string, lines int) (*PaneReadResult, error) {
	if source == "" {
		source = "recent"
	}
	params := map[string]any{"pane_id": paneID, "source": source, "strip_ansi": true, "format": "text"}
	if lines > 0 {
		params["lines"] = lines
	}
	return c.readPaneWith(ctx, params)
}

func (c *Client) ReadPaneStyled(ctx context.Context, paneID, source string, lines int) (*PaneReadResult, error) {
	if source == "" {
		source = "visible"
	}
	params := map[string]any{"pane_id": paneID, "source": source, "strip_ansi": false, "format": "ansi"}
	if lines > 0 {
		params["lines"] = lines
	}
	return c.readPaneWith(ctx, params)
}

// readPaneWith performs one pane.read and unwraps the envelope.
func (c *Client) readPaneWith(ctx context.Context, params map[string]any) (*PaneReadResult, error) {
	var out struct {
		Type string         `json:"type"`
		Read PaneReadResult `json:"read"`
	}
	if err := c.CallInto(ctx, MethodPaneRead, params, &out); err != nil {
		return nil, err
	}
	if out.Type != "pane_read" {
		return nil, fmt.Errorf("%s returned unexpected result type %q", MethodPaneRead, out.Type)
	}
	return &out.Read, nil
}

// ReadAgent reads recent output through the agent-scoped method, which is
// preferred when the target is a recognised agent.
func (c *Client) ReadAgent(ctx context.Context, target, source string, lines int) (*PaneReadResult, error) {
	if source == "" {
		source = "recent"
	}
	params := map[string]any{"target": target, "source": source, "strip_ansi": true, "format": "text"}
	if lines > 0 {
		params["lines"] = lines
	}
	var out struct {
		Type string         `json:"type"`
		Read PaneReadResult `json:"read"`
	}
	if err := c.CallInto(ctx, MethodAgentRead, params, &out); err != nil {
		return nil, err
	}
	if out.Type != "pane_read" {
		return nil, fmt.Errorf("%s returned unexpected result type %q", MethodAgentRead, out.Type)
	}
	return &out.Read, nil
}

// SendKeys presses keys on a target. Keys are herdr key names ("enter", "esc",
// "y"), not raw bytes — that is what makes the mapping reproducible by hand.
func (c *Client) SendKeys(ctx context.Context, target string, keys []string) error {
	_, err := c.Call(ctx, MethodAgentSendKeys, map[string]any{"target": target, "keys": keys})
	return err
}

// Prompt submits text to an agent. herdr handles bracketed-paste framing.
func (c *Client) Prompt(ctx context.Context, target, text string) error {
	_, err := c.Call(ctx, MethodAgentPrompt, map[string]any{"target": target, "text": text})
	return err
}

// SendText writes raw text into a pane that is not a recognised agent session.
// This is the escape-hatch variant of "deliver text the user wrote" (A5), not a
// fifth class of write.
func (c *Client) SendText(ctx context.Context, paneID, text string) error {
	_, err := c.Call(ctx, MethodPaneSendText, map[string]any{"pane_id": paneID, "text": text})
	return err
}

// SendKeysToPane presses keys on a pane rather than on an agent.
//
// The difference from SendKeys is a safety property herdr documents: pane input
// "addresses the terminal regardless of its current occupant", whereas agent input
// resolves the live agent and refuses if that agent no longer controls the pane.
// The escape hatch is the user operating a terminal, so it addresses the pane —
// and the caller restricts which keys may travel this way (actions.AllowedKey).
func (c *Client) SendKeysToPane(ctx context.Context, paneID string, keys []string) error {
	_, err := c.Call(ctx, MethodPaneSendKeys, map[string]any{"pane_id": paneID, "keys": keys})
	return err
}

// SendInput writes text and then presses keys as one request.
//
// This is the method the CLI's `pane run` is built from, and herdr's own
// documentation prefers it over the two separate calls "for commands":
//
//	"pane run honors live bracketed-paste mode and submits text plus Enter
//	 atomically. Prefer it over send-text plus send-keys Enter for commands; the
//	 separate send operations remain low-level and non-submitting."
//
// Bracketed paste is not a detail here. Without it, every newline inside a
// multi-line instruction is delivered as a newline *to the application*, which a
// TUI reads as Enter — so a multi-line answer arrives as several separate
// submissions. Spec FR-006 requires an instruction's characters to arrive intact,
// newlines included, and this is the method that can do that.
func (c *Client) SendInput(ctx context.Context, paneID, text string, keys []string) error {
	params := map[string]any{"pane_id": paneID}
	if text != "" {
		params["text"] = text
	}
	if len(keys) > 0 {
		params["keys"] = keys
	}
	_, err := c.Call(ctx, MethodPaneSendInput, params)
	return err
}

// AgentStatesAll subscribes to status changes for every pane given, plus the
// global lifecycle events needed to notice panes appearing and disappearing.
//
// Three lifecycle words, not two: pane.closed covers an explicit close, and
// pane.exited covers the process inside the pane ending — someone typing `exit`.
// herdr delivers the exit only as pane_exited (measured 2026-10-17, issue #124);
// a set without it misses every exit and the pane lingers until the backstop.
func AgentStatesSubscriptions(paneIDs []string) []Subscription {
	subs := make([]Subscription, 0, len(paneIDs)+4)
	subs = append(subs,
		Subscription{Type: EventPaneCreated},
		Subscription{Type: EventPaneClosed},
		Subscription{Type: EventPaneExited},
		Subscription{Type: EventPaneAgentDetected},
	)
	for _, id := range paneIDs {
		if id == "" {
			continue
		}
		subs = append(subs, Subscription{Type: EventPaneAgentStatusChanged, PaneID: id})
	}
	return subs
}

// StatusChange is the payload of pane.agent_status_changed.
type StatusChange struct {
	PaneID       string            `json:"pane_id"`
	WorkspaceID  string            `json:"workspace_id"`
	Agent        *string           `json:"agent"`
	DisplayAgent *string           `json:"display_agent"`
	Title        *string           `json:"title"`
	AgentStatus  AgentStatus       `json:"agent_status"`
	StateLabels  map[string]string `json:"state_labels"`
}

// DecodeStatusChange parses an event payload, tolerating the underscore-style
// envelope used by plugin hooks.
func DecodeStatusChange(raw json.RawMessage) (*StatusChange, error) {
	var sc StatusChange
	if err := json.Unmarshal(raw, &sc); err != nil {
		return nil, err
	}
	if sc.PaneID == "" {
		return nil, fmt.Errorf("status change without pane_id")
	}
	return &sc, nil
}
