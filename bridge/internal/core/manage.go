package core

import (
	"context"
	"fmt"

	"github.com/th2zz/perch/bridge/internal/herdr"
)


// Action is what a person asked for, in the product's vocabulary.
type Action string

const (
	ActionRename Action = "rename"
	ActionClose  Action = "close"
)

// Target is what they asked it about. "session" is a pane with an agent in it; "project" is a workspace.
type Target string

const (
	TargetSession Target = "session"
	TargetProject Target = "project"
)

// Equivalent is what happened, as one herdr command, for the app to keep and show.
//
// The same idea as the decision path's equivalence string: the product promises that every change it makes is
// one command somebody could have typed, and the way to keep that promise honest is to write the command down
// and hand it over.
type Equivalent struct {
	Equivalent string `json:"equivalent"`
	Target     Target `json:"target"`
	ID         string `json:"id"`
	Label      string `json:"label,omitempty"`
}

// RenameSession names a session.
//
// An empty label clears the name back to whatever the program in the pane calls itself — herdr's own
// semantics, and the only way to undo a bad name, so it is allowed through rather than rejected as "empty".
func (c *Core) RenameSession(ctx context.Context, hostID, paneID, label string) (*Equivalent, error) {
	if paneID == "" {
		return nil, fmt.Errorf("which session? a pane id is required")
	}
	clean := ""
	if label != "" {
		var err error
		if clean, err = herdr.CleanLabel(label); err != nil {
			return nil, err
		}
	}
	machine, err := c.hostMachine(ctx, hostID)
	if err != nil {
		return nil, err
	}
	argv := herdr.RenamePaneCommand(paneID, clean)
	equivalent := herdr.Format(argv)
	if machine != "" {
		if err := c.cli.RunMachine(ctx, machine, argv...); err != nil {
			return nil, fmt.Errorf("herdr refused the name on %s: %w", machine, err)
		}
	} else if err := c.api.RenamePane(ctx, paneID, clean); err != nil {
		return nil, fmt.Errorf("herdr refused the name: %w", err)
	}
	return &Equivalent{Equivalent: equivalent, Target: TargetSession, ID: paneID, Label: clean}, nil
}

// RenameProject names a project (a workspace).
func (c *Core) RenameProject(ctx context.Context, hostID, workspaceID, label string) (*Equivalent, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("which project? a workspace id is required")
	}
	// A project always has a name — clearing one would leave a row in the list that says nothing, which is
	// worse than a name somebody does not like.
	clean, err := herdr.CleanLabel(label)
	if err != nil {
		return nil, err
	}
	machine, err := c.hostMachine(ctx, hostID)
	if err != nil {
		return nil, err
	}
	argv := herdr.RenameWorkspaceCommand(workspaceID, clean)
	equivalent := herdr.Format(argv)
	if machine != "" {
		if err := c.cli.RunMachine(ctx, machine, argv...); err != nil {
			return nil, fmt.Errorf("herdr refused the name on %s: %w", machine, err)
		}
	} else if err := c.api.RenameWorkspace(ctx, workspaceID, clean); err != nil {
		return nil, fmt.Errorf("herdr refused the name: %w", err)
	}
	return &Equivalent{Equivalent: equivalent, Target: TargetProject, ID: workspaceID, Label: clean}, nil
}

// CloseSession ends one session: the program in the pane stops and the pane goes away.
//
// Nothing is confirmed here, and that is deliberate rather than lax. The phone is the only place this can be
// asked from, the wording says what happens, and the app asks when the agent is in the middle of something.
// A second confirmation in the bridge would be a dialog nobody sees, which teaches people to click through
// dialogs.
func (c *Core) CloseSession(ctx context.Context, hostID, paneID string) (*Equivalent, error) {
	if paneID == "" {
		return nil, fmt.Errorf("which session? a pane id is required")
	}
	machine, err := c.hostMachine(ctx, hostID)
	if err != nil {
		return nil, err
	}
	argv := herdr.ClosePaneCommand(paneID)
	equivalent := herdr.Format(argv)
	if machine != "" {
		if err := c.cli.RunMachine(ctx, machine, argv...); err != nil {
			return nil, fmt.Errorf("herdr refused to close it on %s: %w", machine, err)
		}
	} else if err := c.api.ClosePane(ctx, paneID); err != nil {
		return nil, fmt.Errorf("herdr refused to close it: %w", err)
	}
	return &Equivalent{Equivalent: equivalent, Target: TargetSession, ID: paneID}, nil
}

// CloseProject closes a project and every session in it.
//
// `confirmed` is not a formality: it is the difference between "the user saw what would go and said yes" and
// "a screen had a bug". The app sets it from a dialog that lists what is about to be lost; anything that
// reaches here without it is refused with the reason, in the user's words.
func (c *Core) CloseProject(ctx context.Context, hostID, workspaceID string, confirmed bool) (*Equivalent, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("which project? a workspace id is required")
	}
	if !confirmed {
		return nil, fmt.Errorf("closing a project ends every session in it and cannot be undone; " +
			"confirm it from the phone")
	}
	machine, err := c.hostMachine(ctx, hostID)
	if err != nil {
		return nil, err
	}
	argv := herdr.CloseWorkspaceCommand(workspaceID)
	equivalent := herdr.Format(argv)
	if machine != "" {
		if err := c.cli.RunMachine(ctx, machine, argv...); err != nil {
			return nil, fmt.Errorf("herdr refused to close it on %s: %w", machine, err)
		}
	} else if err := c.api.CloseWorkspace(ctx, workspaceID); err != nil {
		return nil, fmt.Errorf("herdr refused to close it: %w", err)
	}
	return &Equivalent{Equivalent: equivalent, Target: TargetProject, ID: workspaceID}, nil
}
