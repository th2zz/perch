package latency

import (
	"path/filepath"
	"testing"
)

func newRecorder(t *testing.T) *Recorder {
	t.Helper()
	return NewRecorder(filepath.Join(t.TempDir(), "latency.json"))
}

// TestOnlyConfirmedMeasurementsCount is the honesty rule: a notification the
// phone never confirmed must not be scored as a zero-second second hop, which
// would make the product look faster than it is.
func TestOnlyConfirmedMeasurementsCount(t *testing.T) {
	r := newRecorder(t)
	r.Queue("d1", 500)
	r.Queue("d2", 400)
	r.Shown("d1", 300)

	stats := r.Stats()
	if stats.Count != 1 {
		t.Fatalf("count = %d, want 1 confirmed measurement", stats.Count)
	}
	if stats.P50 != 800 {
		t.Errorf("p50 = %d, want 800 (500 bridge + 300 phone)", stats.P50)
	}
}

// TestTheSameDecisionIsMeasuredOnce: a decision raises several events as its
// state is re-read, and counting each would quietly inflate the sample.
func TestTheSameDecisionIsMeasuredOnce(t *testing.T) {
	r := newRecorder(t)
	r.Queue("d1", 100)
	r.Queue("d1", 200)
	r.Queue("d1", 300)
	r.Shown("d1", 50)

	stats := r.Stats()
	if stats.Count != 1 {
		t.Fatalf("count = %d, want 1", stats.Count)
	}
	if stats.P50 != 150 {
		t.Errorf("p50 = %d, want the first measurement (100 + 50)", stats.P50)
	}
}

func TestPercentilesUseValuesThatWereActuallyMeasured(t *testing.T) {
	r := newRecorder(t)
	// Ten total latencies: 100..1000 ms.
	for i := 0; i < 10; i++ {
		id := string(rune('a' + i))
		r.Queue(id, int64((i+1)*100))
		r.Shown(id, 0)
	}
	stats := r.Stats()

	if stats.Count != 10 {
		t.Fatalf("count = %d, want 10", stats.Count)
	}
	if stats.Max != 1000 {
		t.Errorf("max = %d, want 1000", stats.Max)
	}
	// Nearest-rank, so the answer is a number that was measured rather than an
	// interpolation between two that were.
	if stats.P50 != 500 {
		t.Errorf("p50 = %d, want 500", stats.P50)
	}
	if stats.P90 != 900 {
		t.Errorf("p90 = %d, want 900", stats.P90)
	}
}

func TestAnUnconfirmedReportIsNotInvented(t *testing.T) {
	r := newRecorder(t)
	if r.Shown("never-seen", 100) {
		t.Error("reporting for an unknown decision should say so rather than pretending")
	}
	if stats := r.Stats(); stats.Count != 0 {
		t.Errorf("nothing should be counted: %+v", stats)
	}
}

func TestMeasurementsSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "latency.json")
	first := NewRecorder(path)
	first.Queue("d1", 250)
	first.Shown("d1", 150)

	// The bridge restarts all the time — an update, a reboot, a crash. Losing the
	// evidence each time would make the number depend on uptime.
	second := NewRecorder(path)
	stats := second.Stats()
	if stats.Count != 1 || stats.P50 != 400 {
		t.Fatalf("after reload: %+v, want one measurement of 400ms", stats)
	}
}

func TestTheQueueIsStillOpenWhenThePhoneIsSlow(t *testing.T) {
	r := newRecorder(t)
	r.Queue("d1", 10)
	r.Queue("d2", 20)
	r.Shown("d2", 30)

	recent := r.Recent(5)
	if len(recent) != 2 {
		t.Fatalf("both decisions should be listed, newest first: %+v", recent)
	}
	if recent[0].DecisionID != "d2" {
		t.Errorf("newest first, got %s", recent[0].DecisionID)
	}
	if recent[1].Reported {
		t.Error("d1 has not been confirmed yet")
	}
	// The bridge-side figure comes from confirmed samples only, so an
	// unconfirmed sample cannot skew the attribution of a late notification.
	if stats := r.Stats(); stats.QueueP90 != 20 {
		t.Errorf("the bridge-side figure should come from confirmed samples only, got %d", stats.QueueP90)
	}
}

func TestEmptyRecorderIsSilentRatherThanZero(t *testing.T) {
	stats := newRecorder(t).Stats()
	if stats.Count != 0 || stats.P50 != 0 || stats.P90 != 0 {
		t.Errorf("an empty recorder must not imply a perfect score: %+v", stats)
	}
}

// TestColdStartIsMeasuredSeparately: the two promises about time are different
// questions, and averaging them together would answer neither.
func TestColdStartIsMeasuredSeparately(t *testing.T) {
	r := newRecorder(t)
	r.Queue("d1", 200)
	r.Shown("d1", 100)

	for _, ms := range []int64{1400, 900, 1100, 2400} {
		r.AddStartup(ms)
	}

	startup := r.StartupStats()
	if startup.Count != 4 {
		t.Fatalf("count = %d, want 4", startup.Count)
	}
	if startup.Max != 2400 {
		t.Errorf("max = %d", startup.Max)
	}
	// Ordered: 900, 1100, 1400, 2400 → the 90th percentile is 2400, which is the
	// point of reporting it: one bad start should be visible, not averaged away.
	if startup.P90 != 2400 {
		t.Errorf("p90 = %d, want 2400", startup.P90)
	}

	// And it must not have disturbed the notification figures.
	if notifications := r.Stats(); notifications.Count != 1 || notifications.P50 != 300 {
		t.Errorf("notification stats changed: %+v", notifications)
	}
}

func TestAnImplausibleStartupFigureIsIgnored(t *testing.T) {
	r := newRecorder(t)
	r.AddStartup(0)
	r.AddStartup(-5)
	if stats := r.StartupStats(); stats.Count != 0 {
		t.Errorf("a zero or negative start is not a measurement: %+v", stats)
	}
}

func TestBothSeriesSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "latency.json")
	first := NewRecorder(path)
	first.Queue("d1", 250)
	first.Shown("d1", 150)
	first.AddStartup(1200)

	second := NewRecorder(path)
	if got := second.StartupStats(); got.Count != 1 || got.P50 != 1200 {
		t.Errorf("cold start lost across a restart: %+v", got)
	}
	if got := second.Stats(); got.Count != 1 || got.P50 != 400 {
		t.Errorf("notification figures lost across a restart: %+v", got)
	}
}
