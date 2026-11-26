package herdr

import (
	"context"
	"encoding/json"
	"strings"
)

// Machine is a saved SSH machine as reported by `herdr machine list`.
//
// Note for the record: herdr 0.9.1 exposes no `machine.*` socket method. The
// schema enumerates 103 methods and none of them cover machines, so the CLI is
// the only source. This is precisely the case the bridge-plugin contract keeps
// HERDR_BIN_PATH around for; the alternative — keeping our own host table —
// would create a second source of truth for state herdr already owns
// (constitution I).
type Machine struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Target  string `json:"target"`
	Session string `json:"session"`
	Enabled bool   `json:"enabled"`
}

// Machines lists the user's saved SSH machines. A host with none prints
// "No saved SSH machines." and is not an error.
//
// JSON first, table second. `machine list --json` was verified against herdr
// 0.9.1 on 2026-10-13 and returns `[{"id","label","target","session","enabled",
// "selected"}]`; a real shape beats a human-readable table we would be parsing
// with heuristics. The table parser stays because this is a CLI affordance, not a
// schema, and a future herdr may move the flag — in which case the machine list
// degrades to what it was before rather than disappearing.
func (c *CLI) Machines(ctx context.Context) ([]Machine, error) {
	if text, err := c.Run(ctx, GroupMachine, VerbList, FlagJSON); err == nil {
		if machines, ok := parseMachinesJSON(text); ok {
			return machines, nil
		}
	}
	text, err := c.Run(ctx, GroupMachine, VerbList)
	if err != nil {
		return nil, err
	}
	return ParseMachineList(text), nil
}

// parseMachinesJSON reads the documented JSON form. ok is false when the output
// is not JSON at all, which is the signal to fall back to the table.
func parseMachinesJSON(text string) ([]Machine, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "[") {
		return nil, false
	}
	var machines []Machine
	if err := json.Unmarshal([]byte(trimmed), &machines); err != nil {
		return nil, false
	}
	out := make([]Machine, 0, len(machines))
	for _, m := range machines {
		if m.Label == "" {
			continue
		}
		out = append(out, m)
	}
	return out, true
}

// ParseMachineList parses the human-readable table. It is exported so the
// parser has tests: this output is a CLI affordance, not a stable schema, so
// the bridge must degrade gracefully when the format shifts.
//
// The verified shape (0.9.1, tab-separated, no header) is
// `id  label  target  session  state` — so the label is the *second* field. An
// earlier version took the first field, which meant the app introduced the user's
// other computers by their internal id. Caught while wiring FR-016 to the real
// thing; the column order comes from that output, not from a guess.
func ParseMachineList(text string) []Machine {
	var out []Machine
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "no saved") ||
			strings.HasPrefix(lower, "label") ||
			strings.HasPrefix(lower, "machines") ||
			strings.HasPrefix(lower, "name") {
			continue
		}
		// Tolerate whitespace-aligned tables and simple "label -> target" lines.
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		m := Machine{Enabled: !strings.Contains(lower, "disabled")}
		switch {
		case len(fields) >= 5:
			m.ID, m.Label, m.Target, m.Session = fields[0], fields[1], fields[2], fields[3]
		case len(fields) >= 2:
			m.Label, m.Target = fields[0], fields[1]
		default:
			m.Label = fields[0]
		}
		out = append(out, m)
	}
	return out
}
