// Package latency measures the two promises this product makes about time.
//
// Spec SC-001 says 90% of "an agent is waiting" notifications reach the phone
// within five minutes and the median inside sixty seconds. That is the number the
// whole design is justified by, and without instrumentation it can only be
// guessed at from a stopwatch and a memory.
//
// Spec SC-007 says the queue is on screen within two seconds of a cold start.
// Same problem, same answer: the phone measures it and reports it, because the
// only clocks that can measure it are the phone's.
//
// The measurement is deliberately split by hop, and neither hop needs the two
// machines to agree on a clock:
//
//	queue  : from herdr saying "blocked" to the bridge handing the frame to the
//	         phone's socket. Both instants are on the bridge's clock.
//	shown  : from the phone receiving that frame to posting the notification.
//	         A duration the phone measures itself, so no clock sync is involved.
//
// Their sum is what the user experiences, minus the operating system's own
// delivery, which no code of ours can measure from inside the app.
package latency

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Measurement is one round trip through the product.
type Measurement struct {
	DecisionID string    `json:"decision_id"`
	At         time.Time `json:"at"`
	// QueueMillis is herdr's "blocked" to the frame leaving the bridge.
	QueueMillis int64 `json:"queue_ms"`
	// ShownMillis is the phone receiving the frame to posting the notification.
	ShownMillis int64 `json:"shown_ms"`
	// Reported is false until the phone has told us the second hop.
	Reported bool `json:"reported"`
}

// Total is what the user waits for.
func (m Measurement) Total() int64 { return m.QueueMillis + m.ShownMillis }

// Stats summarises what has been measured.
type Stats struct {
	Count int `json:"count"`
	// P50/P90 are totals, in milliseconds.
	P50 int64 `json:"p50_ms"`
	P90 int64 `json:"p90_ms"`
	Max int64 `json:"max_ms"`
	// QueueP90 isolates the half the bridge is responsible for, which is what to
	// look at when a notification is late: it says whether the delay happened
	// here or on the phone.
	QueueP90 int64 `json:"queue_p90_ms"`
}

// Recorder keeps a bounded history and persists it, so a measurement taken
// overnight survives a restart of the phone or the bridge.
type Recorder struct {
	mu      sync.Mutex
	path    string
	entries []Measurement
	startup []int64
	limit   int
}

// NewRecorder loads any previous measurements from path.
func NewRecorder(path string) *Recorder {
	r := &Recorder{path: path, limit: 500}
	if data, err := os.ReadFile(path); err == nil {
		var stored struct {
			Notifications []Measurement `json:"notifications"`
			Startup       []int64       `json:"startup"`
		}
		if err := json.Unmarshal(data, &stored); err == nil {
			r.entries, r.startup = stored.Notifications, stored.Startup
			return r
		}
		// A file written by the version before cold start was measured held a bare
		// array. Reading it rather than discarding it keeps whatever was already
		// measured.
		_ = json.Unmarshal(data, &r.entries)
	}
	return r
}

// Queue stamps the first hop and remembers it until the phone reports back.
func (r *Recorder) Queue(decisionID string, queueMillis int64) {
	if decisionID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// One entry per decision: a decision can raise several events as its state
	// is re-read, and counting each of them would inflate the sample.
	for i := range r.entries {
		if r.entries[i].DecisionID == decisionID {
			return
		}
	}
	r.entries = append(r.entries, Measurement{
		DecisionID:  decisionID,
		At:          time.Now(),
		QueueMillis: queueMillis,
	})
	r.trimLocked()
	r.persistLocked()
}

// Shown records the second hop, as reported by the phone.
func (r *Recorder) Shown(decisionID string, shownMillis int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.entries {
		if r.entries[i].DecisionID != decisionID {
			continue
		}
		r.entries[i].ShownMillis = shownMillis
		r.entries[i].Reported = true
		r.persistLocked()
		return true
	}
	return false
}

// Recent returns the newest measurements, newest first.
func (r *Recorder) Recent(n int) []Measurement {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Measurement, 0, n)
	for i := len(r.entries) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, r.entries[i])
	}
	return out
}

// Stats summarises the measurements the phone has confirmed.
//
// Unconfirmed entries are excluded rather than counted with a zero second hop:
// a notification that was never shown must not make the product look faster.
func (r *Recorder) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()

	totals := make([]int64, 0, len(r.entries))
	queues := make([]int64, 0, len(r.entries))
	var out Stats
	for _, entry := range r.entries {
		if !entry.Reported {
			continue
		}
		total := entry.Total()
		totals = append(totals, total)
		queues = append(queues, entry.QueueMillis)
		if total > out.Max {
			out.Max = total
		}
	}
	out.Count = len(totals)
	out.P50 = percentile(totals, 0.50)
	out.P90 = percentile(totals, 0.90)
	out.QueueP90 = percentile(queues, 0.90)
	return out
}

func (r *Recorder) trimLocked() {
	if len(r.entries) <= r.limit {
		return
	}
	r.entries = r.entries[len(r.entries)-r.limit:]
}

func (r *Recorder) persistLocked() {
	if r.path == "" {
		return
	}
	data, err := json.Marshal(struct {
		Notifications []Measurement `json:"notifications"`
		Startup       []int64       `json:"startup"`
	}{r.entries, r.startup})
	if err != nil {
		return
	}
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	temp := r.path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(temp, r.path)
}

// AddStartup records one cold start, in milliseconds, as the phone measured it.
func (r *Recorder) AddStartup(millis int64) {
	if millis <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startup = append(r.startup, millis)
	if len(r.startup) > r.limit {
		r.startup = r.startup[len(r.startup)-r.limit:]
	}
	r.persistLocked()
}

// StartupStats summarises the cold starts the phone has reported.
func (r *Recorder) StartupStats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out Stats
	out.Count = len(r.startup)
	for _, ms := range r.startup {
		if ms > out.Max {
			out.Max = ms
		}
	}
	out.P50 = percentile(r.startup, 0.50)
	out.P90 = percentile(r.startup, 0.90)
	return out
}

// percentile reports the smallest measured value at or below which the given
// fraction of samples fall.
//
// Two choices are deliberate. It uses nearest-rank rather than interpolating, so
// the answer is always a value that was actually measured — with the handful of
// samples a person generates in a day, an interpolated percentile would be a
// fiction with a decimal point. And it takes the floor of the rank rather than
// the ceiling: the ceiling would report the maximum and call it the 90th
// percentile, which is the direction that flatters the product. For a latency
// figure, the honest direction to be wrong in is against ourselves.
func percentile(values []int64, fraction float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	rank := ceil(fraction*float64(len(sorted))) - 1
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	if rank < 0 {
		rank = 0
	}
	return sorted[rank]
}

func ceil(value float64) int {
	truncated := int(value)
	if value > float64(truncated) {
		return truncated + 1
	}
	return truncated
}
