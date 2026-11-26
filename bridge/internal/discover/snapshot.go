// Package discover builds the read-only mirror the app renders.
//
// Nothing in this package writes to herdr or to the host's configuration: it
// only reshapes data herdr already produced. That is what makes spec US1's
// "install and nothing changed" claim checkable rather than aspirational
// (constitution III).
package discover

import (
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

// Host is a machine the user's agents run on.
//
// host_id reuses herdr's identity: the machine profile label for SSH machines,
// and the literal "local" for the machine running this bridge. We deliberately
// do not mint a parallel ID space (data-model.md §1).
type Host struct {
	HostID string `json:"host_id"`
	Label  string `json:"label"`
	Kind   string `json:"kind"`   // "local" | "ssh"
	Status string `json:"status"` // one of the Status* constants below
	// Reason says why an unreachable host is unreachable, in the user's terms, and
	// Fix is the next step (spec FR-015). A host that has never been read keeps
	// Status "unknown" and an empty Reason: "we have not looked yet" is a different
	// statement from "we looked and it did not answer".
	Reason  string `json:"reason,omitempty"`
	Fix     string `json:"fix,omitempty"`
	Enabled bool   `json:"enabled"`
}

// LocalHostID is the identifier of the machine the bridge runs on.
const LocalHostID = "local"

// What a host's Status can be.
//
// Named rather than written inline because the phone decides from these strings
// what to call a computer and whether to offer its projects: the app compares
// against the same words, in another language, with no type check across the
// boundary. They are pinned on both sides by
// `contracts/wire-vocabulary.json`.
const (
	// StatusReachable means the last read of this machine succeeded.
	StatusReachable = "reachable"
	// StatusUnreachable means we looked and it did not answer.
	StatusUnreachable = "unreachable"
	// StatusUnknown means we have not looked yet, which is a different statement
	// from "we looked and it failed".
	StatusUnknown = "unknown"
)

// Placed* are herdr's own records with the machine they came from attached.
//
// The host is not part of herdr's data — a pane lives inside one server, which
// has no reason to name itself — so the bridge stamps it as the record is filed.
// Embedding rather than copying keeps herdr's field definitions in exactly one
// place (the schema transcription in internal/herdr/types.go).
type PlacedWorkspace struct {
	herdr.Workspace
	HostID string `json:"host_id"`
}

type PlacedTab struct {
	herdr.Tab
	HostID string `json:"host_id"`
}

type PlacedPane struct {
	herdr.Pane
	HostID string `json:"host_id"`
}

type PlacedAgent struct {
	herdr.Agent
	HostID string `json:"host_id"`
}

// Mirror is a point-in-time image of everything the app shows.
type Mirror struct {
	Hosts      []Host            `json:"hosts"`
	Version    string            `json:"herdr_version"`
	Protocol   uint32            `json:"protocol"`
	TakenAt    time.Time         `json:"taken_at"`
	Workspaces []PlacedWorkspace `json:"workspaces"`
	Tabs       []PlacedTab       `json:"tabs"`
	Panes      []PlacedPane      `json:"panes"`
	Agents     []PlacedAgent     `json:"agents"`
}

// Placed wraps one host's snapshot with the identity the app needs.
type Placed struct {
	Host    Host
	Snap    *herdr.Snapshot
	TakenAt time.Time
}

func Build(placed []Placed, takenAt time.Time) *Mirror {
	m := &Mirror{
		Hosts:      make([]Host, 0, len(placed)),
		TakenAt:    takenAt,
		Workspaces: []PlacedWorkspace{},
		Tabs:       []PlacedTab{},
		Panes:      []PlacedPane{},
		Agents:     []PlacedAgent{},
	}
	for _, p := range placed {
		m.Hosts = append(m.Hosts, p.Host)
		if p.Snap == nil {
			continue
		}
		if p.Host.HostID == LocalHostID {
			m.Version = p.Snap.Version
			m.Protocol = p.Snap.Protocol
		}
		for _, w := range p.Snap.Workspaces {
			m.Workspaces = append(m.Workspaces, PlacedWorkspace{Workspace: w, HostID: p.Host.HostID})
		}
		for _, t := range p.Snap.Tabs {
			m.Tabs = append(m.Tabs, PlacedTab{Tab: t, HostID: p.Host.HostID})
		}
		for _, pane := range p.Snap.Panes {
			m.Panes = append(m.Panes, PlacedPane{Pane: pane, HostID: p.Host.HostID})
		}
		for _, a := range p.Snap.Agents {
			m.Agents = append(m.Agents, PlacedAgent{Agent: a, HostID: p.Host.HostID})
		}
	}
	return m
}
