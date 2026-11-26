package core

import (
	"testing"
	"time"
)

// TestTheRequestTallyCountsMinutesAndForgetsThem.
//
// The gauge exists to make a loop visible as a number. That only works if it counts
// what happened in the window and goes quiet when the phone stops asking — a counter
// that never forgets would show yesterday's trial as today's loop.
func TestTheRequestTallyCountsMinutesAndForgetsThem(t *testing.T) {
	var tally requestTally
	start := time.Unix(1_700_000_000, 0) // a whole minute boundary, so the maths is plain

	for i := 0; i < 5; i++ {
		tally.note(start.Add(time.Duration(i) * time.Second))
	}
	lastMinute, lastTen := tally.rate(start.Add(5 * time.Second))
	if lastMinute != 5 || lastTen != 5 {
		t.Errorf("after five requests in one minute: %d/%d, want 5/5", lastMinute, lastTen)
	}

	// Two minutes later: still inside the ten-minute window, but not the last minute.
	tally.note(start.Add(2 * time.Minute))
	lastMinute, lastTen = tally.rate(start.Add(2*time.Minute + time.Second))
	if lastMinute != 1 {
		t.Errorf("last minute = %d, want only the newest request", lastMinute)
	}
	if lastTen != 6 {
		t.Errorf("last ten minutes = %d, want all six", lastTen)
	}

	// Nine minutes after the last request it is still inside the window — a ten-minute
	// window that dropped it would be a nine-minute window.
	if _, lastTen = tally.rate(start.Add(11 * time.Minute)); lastTen != 1 {
		t.Errorf("nine minutes later the request should still be counted, got %d", lastTen)
	}
	// Twelve minutes after it, everything has aged out rather than accumulating.
	lastMinute, lastTen = tally.rate(start.Add(12 * time.Minute))
	if lastMinute != 0 || lastTen != 0 {
		t.Errorf("twelve minutes later: %d/%d, want 0/0", lastMinute, lastTen)
	}
}
