package herdr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)


// maxLabel bounds a name a person can type on a phone.
//
// Not a herdr limit — a product one. A terminal label is drawn in a list row on a 400-pixel screen, and a
// label of a thousand characters is a way to make the list unreadable rather than a way to be expressive.
// Control characters are refused for the same reason they are refused everywhere else in this bridge: a
// newline in a label is a line the terminal will draw that nobody wrote.
const maxLabel = 80

// CleanLabel trims a label and refuses one that cannot be shown.
func CleanLabel(label string) (string, error) {
	clean := strings.TrimSpace(label)
	if clean == "" {
		return "", fmt.Errorf("a name cannot be empty")
	}
	if len([]rune(clean)) > maxLabel {
		return "", fmt.Errorf("a name can be at most %d characters", maxLabel)
	}
	for _, r := range clean {
		if r == '\n' || r == '\r' || r == '\t' || unicode.IsControl(r) {
			return "", fmt.Errorf("a name cannot contain control characters")
		}
	}
	return clean, nil
}

// RenamePane names a session. An empty label clears the name (herdr's own `--clear`).
func (c *Client) RenamePane(ctx context.Context, paneID, label string) error {
	if paneID == "" {
		return fmt.Errorf("which session? a pane id is required")
	}
	// herdr takes a nullable label, and null is how a name is cleared. Sent as an explicit nil rather than an
	// empty string because those are different requests: "" would be a name of zero length.
	var value any
	if label == "" {
		value = nil
	} else {
		value = label
	}
	_, err := c.Call(ctx, MethodPaneRename, map[string]any{"pane_id": paneID, "label": value})
	return err
}

// RenameWorkspace names a project.
func (c *Client) RenameWorkspace(ctx context.Context, workspaceID, label string) error {
	if workspaceID == "" {
		return fmt.Errorf("which project? a workspace id is required")
	}
	// herdr requires the label here (there is no null), and `CleanLabel` has already refused an empty one.
	_, err := c.Call(ctx, MethodWorkspaceRename, map[string]string{
		"workspace_id": workspaceID,
		"label":        label,
	})
	return err
}

// ClosePane ends a session: the program running in it stops and the pane goes away.
//
// Destructive in the way a terminal is destructive — if an agent is in the middle of something, that work
// stops — which is why the product asks before doing it and why the app shows what the agent was doing.
// There is no undo: herdr has no "reopen", and the process is gone.
func (c *Client) ClosePane(ctx context.Context, paneID string) error {
	if paneID == "" {
		return fmt.Errorf("which session? a pane id is required")
	}
	_, err := c.Call(ctx, MethodPaneClose, map[string]string{"pane_id": paneID})
	return err
}

// CloseWorkspace closes a project and everything in it — every pane, every agent.
//
// `close_group` is left at its default (false): a "group" is a herdr concept for workspaces that were opened
// together, and closing things beyond what the user pointed at is exactly the kind of surprise this product
// refuses to spring on somebody.
func (c *Client) CloseWorkspace(ctx context.Context, workspaceID string) error {
	if workspaceID == "" {
		return fmt.Errorf("which project? a workspace id is required")
	}
	_, err := c.Call(ctx, MethodWorkspaceClose, map[string]string{"workspace_id": workspaceID})
	return err
}

type CreatedWorkspace struct {
	Workspace Workspace
	Tab       Tab
	Pane      Pane
}

type CreatedTab struct {
	Tab  Tab
	Pane Pane
}

func (c *Client) CreateWorkspace(
	ctx context.Context,
	label, cwd string,
	env map[string]string,
	focus bool,
) (CreatedWorkspace, error) {
	var out struct {
		Workspace Workspace `json:"workspace"`
		Tab       Tab       `json:"tab"`
		RootPane  Pane      `json:"root_pane"`
	}
	params := map[string]any{"label": label, "focus": focus}
	if cwd != "" {
		params["cwd"] = cwd
	}
	if len(env) > 0 {
		params["env"] = env
	}
	if err := c.CallInto(ctx, MethodWorkspaceCreate, params, &out); err != nil {
		return CreatedWorkspace{}, err
	}
	if out.Workspace.WorkspaceID == "" || out.RootPane.PaneID == "" {
		return CreatedWorkspace{}, fmt.Errorf("herdr created a workspace without a pane")
	}
	return CreatedWorkspace{Workspace: out.Workspace, Tab: out.Tab, Pane: out.RootPane}, nil
}

func (c *Client) CreateTab(
	ctx context.Context,
	workspaceID, label, cwd string,
	env map[string]string,
	focus bool,
) (CreatedTab, error) {
	var out struct {
		Tab      Tab  `json:"tab"`
		RootPane Pane `json:"root_pane"`
	}
	params := map[string]any{"workspace_id": workspaceID, "label": label, "focus": focus}
	if cwd != "" {
		params["cwd"] = cwd
	}
	if len(env) > 0 {
		params["env"] = env
	}
	if err := c.CallInto(ctx, MethodTabCreate, params, &out); err != nil {
		return CreatedTab{}, err
	}
	if out.Tab.TabID == "" || out.RootPane.PaneID == "" {
		return CreatedTab{}, fmt.Errorf("herdr created a tab without a pane")
	}
	return CreatedTab{Tab: out.Tab, Pane: out.RootPane}, nil
}

func (c *Client) StartAgent(
	ctx context.Context,
	name, kind, paneID string,
	args []string,
	wait time.Duration,
) (Agent, error) {
	deadline := time.Now().Add(wait)
	busy := 0
	for {
		var out struct {
			Agent Agent `json:"agent"`
		}
		params := map[string]any{
			"name": name, "kind": kind, "pane_id": paneID, "args": args, "timeout_ms": 30000,
		}
		err := c.CallInto(ctx, MethodAgentStart, params, &out)
		if err == nil {
			return out.Agent, nil
		}
		var herr *Error
		if errors.As(err, &herr) && herr.Code == "agent_pane_busy" && time.Now().Before(deadline) {
			busy++
			if busy == 4 {
				_ = c.SendKeysToPane(ctx, paneID, []string{"ctrl+c"})
			}
			select {
			case <-ctx.Done():
				return Agent{}, ctx.Err()
			case <-time.After(250 * time.Millisecond):
				continue
			}
		}
		return Agent{}, err
	}
}
