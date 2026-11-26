package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/th2zz/perch/bridge/internal/herdr"
	"github.com/th2zz/perch/bridge/internal/records"
)

// Usage reads what the harness itself knows about a session: its model, what it has spent,
// whether its goal is stuck.
//
// On demand, not on every poll. Finding the session's record means asking herdr which process
// is in the pane and then reading the end of a file that can be tens of megabytes (measured:
// 51 MB after two days on this machine), which is fine once and wasteful every 0.8 seconds.
func (c *Core) Usage(ctx context.Context, hostID, paneID string) (records.Facts, error) {
	if paneID == "" {
		return records.Facts{}, errors.New("which session? a pane id is required")
	}
	if hostID != "" && hostID != "local" {
		// Another machine's records are its own; the chain that finds them lives on that
		// machine. Reporting nothing is the honest answer until a host can be asked.
		return records.Facts{}, nil
	}
	// Which process this session is lives in one place, shared with the conversation reader, so
	// the two panels can never describe two different sessions (see [Core.sessionWhere]).
	kind, where, err := c.sessionWhere(ctx, paneID)
	if err != nil {
		return records.Facts{}, err
	}
	if facts, ok := records.Read(kind, homeDir(), where); ok {
		return facts, nil
	}
	// Nothing to report, and the panel still deserves to know *why*: not-readable is a
	// different sentence from nothing-recorded-yet.
	return records.Facts{Harness: kind, Readable: false}, nil
}

// homeDir is where the harnesses keep their records.
//
// Cleaned, because it is also the "home folder" the phone compares a chosen folder against: an
// environment spelling it "/home//me" and a folder listing that spells it "/home/me" are the same
// place, and the trust warning must not depend on which spelling arrived.
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Clean(home)
}

// Terminal opens a live terminal for a pane.
//
// The bridge is a client of herdr here, not an owner: the pane belongs to the machine and to the
// person sitting at it, and this only attaches a screen. That is what makes closing the phone's
// terminal safe, and what makes a dropped connection a non-event.
func (c *Core) Terminal(ctx context.Context, hostID, paneID string, cols, rows int) (*herdr.TerminalStream, error) {
	if paneID == "" {
		return nil, errors.New("which terminal? a pane id is required")
	}
	machine := ""
	if hostID != "" && hostID != "local" {
		machine = hostID
	}
	if machine == "" && (cols <= 0 || rows <= 0) {
		return c.cli.TerminalView(ctx, paneID, c.api)
	}
	return c.cli.TerminalStream(ctx, paneID, machine, cols, rows)
}
