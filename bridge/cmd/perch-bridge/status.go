package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/th2zz/perch/bridge/internal/core"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

func cmdStatus(ctx context.Context, args []string) error {
	c := mustCore(true)
	ctx, cancel := withTimeout(ctx, 20*time.Second)
	defer cancel()

	fmt.Printf("perch-bridge %s\n", version)
	fmt.Printf("state dir    : %s\n", c.Store().Dir())
	fmt.Printf("herdr socket : %s\n", c.Client().Path())

	if compat, err := herdr.CheckCompat(ctx, c.Client()); err != nil {
		fmt.Printf("herdr        : unreachable (%v)\n", err)
	} else {
		verdict := "compatible"
		if !compat.OK {
			verdict = "INCOMPATIBLE — " + compat.Reason
		}
		fmt.Printf("herdr        : %s protocol %d (%s)\n", compat.Version, compat.Protocol, verdict)
	}

	if p, err := c.Store().PairingInfo(); err == nil && p.Token != "" {
		fmt.Printf("paired since : %s\n", p.CreatedAt.Format(time.RFC3339))
	} else {
		fmt.Printf("pairing      : no token yet (run: perch-bridge pair)\n")
	}

	m, err := c.RefreshOnce(ctx)
	if err != nil {
		fmt.Printf("session      : cannot read (%v)\n", err)
		return nil
	}

	blocked := 0
	for _, p := range m.Panes {
		if p.AgentStatus.NeedsDecision() {
			blocked++
		}
	}
	fmt.Printf("session      : %d workspaces, %d panes, %d agents, %d waiting on you\n",
		len(m.Workspaces), len(m.Panes), len(m.Agents), blocked)

	pending := c.Queue().List(false)
	fmt.Printf("queue        : %d pending\n", len(pending))

	// How much the phone is asking for. A healthy phone is quiet; a number far above a
	// handful per minute is the shape of a loop, and seeing it here is how such a loop
	// gets diagnosed in seconds instead of being felt as a warm laptop.
	last := c.LastClientSeen()
	stale := time.Duration(0)
	if last.IsZero() {
		if seen, age, ok := c.PersistedClientSeen(); ok {
			last, stale = seen, age
		}
	}
	switch {
	case last.IsZero():
		fmt.Printf("phone        : nothing heard from it yet\n")
	default:
		lastMinute, lastTen := c.ClientRequestRate()
		record := ""
		if stale > 0 {
			record = fmt.Sprintf(" (recorded %s ago by the running bridge)", core.HumanAgo(stale))
		}
		fmt.Printf("phone        : last request %s ago%s; %d in the last minute, %d in the last ten\n",
			core.HumanAgo(time.Since(last)), record, lastMinute, lastTen)
	}
	for _, d := range pending {
		fmt.Printf("  - %s in %s — %s (%s)\n", d.AgentKind, d.WorkspaceLabel, oneLine(d.PromptText), d.ID)
	}

	// The one number the whole design is justified by, reported whether or not it
	// flatters the product.
	stats, recent := c.Latency()
	fmt.Println()
	// Two independent promises, so two independent sections. The first version
	// returned early when there were no notifications yet, which silently hid the
	// cold-start figures — and "no decisions handled yet" is the normal state of a
	// freshly installed app, exactly when someone is reading this to see whether it
	// is fast.
	switch stats.Count {
	case 0:
		fmt.Printf("notifications: nothing measured yet\n")
		fmt.Printf("  (each decision the phone actually shows is timed, so this fills in on its own)\n")
	default:
		fmt.Printf("notifications: %d measured\n", stats.Count)
		fmt.Printf("  median     : %s\n", millis(stats.P50))
		fmt.Printf("  90th pct   : %s   (target: under 5 minutes)\n", millis(stats.P90))
		fmt.Printf("  worst      : %s\n", millis(stats.Max))
		fmt.Printf("  bridge side: %s at the 90th pct, so the rest is the phone\n", millis(stats.QueueP90))
		for i, m := range recent {
			if i >= 3 {
				break
			}
			fmt.Printf("  recent     : %s total (%s bridge + %s phone)\n",
				millis(m.Total()), millis(m.QueueMillis), millis(m.ShownMillis))
		}
	}

	// The other promise about time: how long until the app is usable.
	//
	// Named "app open" rather than "cold start" because that is what it is: the app
	// now measures from the moment the user's tap turned into the screen, since the
	// watching service keeps the process alive and "since the process started" was
	// measuring the user's own delay instead (it read 9,874 ms on the first real
	// trial, against a target of two seconds, for an app that appeared at once).
	// A genuine cold start is still counted correctly — that case is chosen by the
	// app, see StartupMeasurement.
	startup := c.ColdStart()
	fmt.Println()
	if startup.Count == 0 {
		fmt.Printf("app open     : nothing measured yet\n")
	} else {
		fmt.Printf("app open     : %d measured\n", startup.Count)
		fmt.Printf("  median     : %s   (target: under 2 seconds)\n", millis(startup.P50))
		fmt.Printf("  90th pct   : %s\n", millis(startup.P90))
		fmt.Printf("  worst      : %s\n", millis(startup.Max))
	}
	return nil
}

// millis renders a duration for the terminal without pulling in a unit library.
func millis(ms int64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 60_000:
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	default:
		return fmt.Sprintf("%.1fmin", float64(ms)/60_000)
	}
}

func cmdSnapshot(ctx context.Context, args []string) error {
	c := mustCore(true)
	ctx, cancel := withTimeout(ctx, 20*time.Second)
	defer cancel()

	m, err := c.RefreshOnce(ctx)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{
		"mirror":    m,
		"decisions": c.Queue().List(true),
	})
}

// oneLine trims an excerpt for terminal display.
func oneLine(s string) string {
	out := make([]rune, 0, 80)
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		out = append(out, r)
	}
	text := string(out)
	if len(text) > 80 {
		text = text[:80] + "…"
	}
	return text
}
