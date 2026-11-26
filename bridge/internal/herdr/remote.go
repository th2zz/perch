package herdr

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Reaching the user's *other* machines.
//
// herdr owns this: a saved SSH machine is reachable as `herdr --machine <label>
// <command>`, which ssh's in and proxies the same socket API the local client
// uses. So the shapes below are the same shapes as the local path — only the
// transport differs — and that is the whole reason the bridge can aggregate
// several computers without inventing a protocol of its own.
//
// Two facts, both measured on 2026-10-13 against herdr 0.9.1:
//
//   - `herdr --machine <label> api snapshot` returns the identical
//     `{"result":{"type":"session_snapshot","snapshot":{…}}}` envelope the local
//     socket returns, so one decoder serves both.
//   - It costs about 3.4 seconds per call (against ~0.5s for a bare ssh), because
//     each invocation starts a fresh SSH connection and remote API bridge. That
//     number is why remote hosts are read on a slow background cadence and never
//     on the app's request path: a phone refresh must not wait on somebody else's
//     laptop.
//
// `--machine` refuses local management commands (validate_machine_command in
// src/cli/target.rs), which suits us: everything this product does is `pane.*` or
// `agent.*`, and both are on the allowed list.

// RemoteSnapshot reads a whole session from a saved SSH machine.
func (c *CLI) RemoteSnapshot(ctx context.Context, machine string) (*Snapshot, error) {
	text, err := c.Run(ctx, FlagMachine, machine, GroupAPI, VerbSnapshot)
	if err != nil {
		return nil, err
	}
	var out struct {
		Result struct {
			Type     string   `json:"type"`
			Snapshot Snapshot `json:"snapshot"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("machine %s: could not read the session list: %w", machine, err)
	}
	if out.Result.Type != "session_snapshot" {
		return nil, fmt.Errorf("machine %s: unexpected result type %q", machine, out.Result.Type)
	}
	return &out.Result.Snapshot, nil
}

// RunMachine runs one command against a saved SSH machine.
//
// This is how a decision reaches another computer: the caller passes the
// equivalent command line the app already shows the user, so "what the button
// does" and "what the screen says it does" cannot drift (constitution II).
func (c *CLI) RunMachine(ctx context.Context, machine string, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("machine %s: nothing to run", machine)
	}
	_, err := c.Run(ctx, append([]string{FlagMachine, machine}, args...)...)
	return err
}

// ExplainAt is `agent explain` against either this machine or a saved one, so the
// decision mapper can be fed the same facts wherever the agent runs.
func (c *CLI) ExplainAt(ctx context.Context, machine, target string) (*Detection, error) {
	if machine == "" {
		return c.Explain(ctx, target)
	}
	text, err := c.Run(ctx, FlagMachine, machine, GroupAgent, VerbExplain, target)
	if err != nil {
		return nil, err
	}
	return ParseExplain(text), nil
}

// ReadPaneTextAt reads recent output from a pane on a saved machine.
//
// Local reads deliberately do not come through here: they already have the socket
// path in the API client, and the CLI prints this one as plain text rather than
// JSON. Passing an empty machine is a caller bug, not a fallback.
func (c *CLI) ReadPaneTextAt(ctx context.Context, machine, paneID string, lines int) (string, error) {
	if machine == "" {
		return "", fmt.Errorf("ReadPaneTextAt needs a machine; local reads go through the socket client")
	}
	args := []string{FlagMachine, machine, GroupPane, VerbRead, paneID, FlagFormat, "text"}
	if lines > 0 {
		args = append(args, FlagLines, fmt.Sprint(lines))
	}
	text, err := c.Run(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(text, "\n"), nil
}
