package herdr

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"
)

// What one call to herdr costs, measured rather than assumed.
//
// This exists because the answer was surprising and it changes design decisions: on this machine
// (herdr 0.x, 2026-10-14) every socket call lands in one of two buckets — about 1 ms or about
// 101 ms — which reads as herdr serving requests on a 100 ms tick. Nothing about Perch's code is
// in that number: it is the same for `pane.get` and for `pane.process_info`, and it is why the
// bridge should ask herdr as few times per screen refresh as it can, and why anything that can be
// answered from the mirror the bridge already keeps should be.
//
// Run it on a machine with herdr up:
//
//	PERCH_LIVE_PROBE=1 go test ./internal/herdr/ -run TestLiveHerdrCallCost -v
//
// No assertion: a budget here would be a budget for somebody else's program, and it would go red
// the day herdr got faster. It reports, and the numbers go into the decisions record.
func TestLiveHerdrCallCost(t *testing.T) {
	if os.Getenv("PERCH_LIVE_PROBE") == "" {
		t.Skip("set PERCH_LIVE_PROBE=1")
	}
	pane := os.Getenv("PERCH_LIVE_PANE")
	if pane == "" {
		pane = "wA:p1"
	}
	client := NewClient("")
	ctx := context.Background()

	for _, call := range []struct {
		name string
		run  func() int
	}{
		{"pane.get", func() int {
			got, err := client.GetPane(ctx, pane)
			if err != nil {
				t.Logf("pane.get failed: %v", err)
			}
			return len(got.PaneID)
		}},
		{"pane.process_info", func() int {
			procs, err := client.PaneProcesses(ctx, pane)
			if err != nil {
				t.Logf("pane.process_info failed: %v", err)
			}
			return len(procs)
		}},
	} {
		// Warm up, so the first call's connection setup is not reported as the call's cost.
		call.run()
		var taken []time.Duration
		for i := 0; i < 8; i++ {
			start := time.Now()
			call.run()
			taken = append(taken, time.Since(start))
		}
		sorted := append([]time.Duration(nil), taken...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		fmt.Printf("%-18s min %.0fms  median %.0fms  max %.0fms   %v\n",
			call.name,
			float64(sorted[0].Microseconds())/1000,
			float64(sorted[len(sorted)/2].Microseconds())/1000,
			float64(sorted[len(sorted)-1].Microseconds())/1000,
			taken,
		)
	}
}
