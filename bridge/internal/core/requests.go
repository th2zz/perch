package core

import (
	"sync"
	"time"

	"github.com/th2zz/perch/bridge/internal/state"
)

// How much the phone is asking for.
//
// This exists because of a bug that was found by reading code and could have been
// found by watching a number: the app refreshed whenever it heard "something changed",
// and answering a read announced a change, so the two chased each other for ever —
// every lap a full read of herdr on the user's machine, with nobody touching the phone.
// The symptom on a laptop is "warm and busy"; the symptom in numbers is this count.
//
// It is also the honest way to answer a question a user will ask during a trial: "is
// this thing talking to my computer constantly?" So the bridge keeps a coarse
// per-minute tally — ten buckets, no timestamps, no history — and `status` reports it.
type requestTally struct {
	mu      sync.Mutex
	buckets [requestWindowMinutes]int64
	minutes [requestWindowMinutes]int64 // which minute each bucket counts
}

const requestWindowMinutes = 10

// note records one authenticated request at the given time.
func (t *requestTally) note(now time.Time) {
	minute := now.Unix() / 60
	t.mu.Lock()
	defer t.mu.Unlock()
	slot := int(minute % requestWindowMinutes)
	if t.minutes[slot] != minute {
		// This slot belongs to an older minute; the window has come round again.
		t.minutes[slot] = minute
		t.buckets[slot] = 0
	}
	t.buckets[slot]++
}

// rate returns how many requests arrived in the last minute and the last ten.
//
// A bucket older than the window counts as zero rather than being dropped, so the
// numbers simply go quiet when the phone stops asking — which is what a gauge should do.
func (t *requestTally) rate(now time.Time) (lastMinute, lastTen int64) {
	minute := now.Unix() / 60
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := 0; i < requestWindowMinutes; i++ {
		if t.minutes[i] < minute-int64(requestWindowMinutes)+1 || t.minutes[i] > minute {
			continue
		}
		lastTen += t.buckets[i]
		if t.minutes[i] == minute {
			lastMinute += t.buckets[i]
		}
	}
	return lastMinute, lastTen
}

// NoteClientRequest counts one authenticated request from the phone.
func (c *Core) NoteClientRequest() {
	now := time.Now()
	c.requests.note(now)
	c.persistClientState(now)
}

// ClientRequestRate reports the phone's request rate over the last minute and the
// last ten, for `status` and the trial report.
func (c *Core) ClientRequestRate() (lastMinute, lastTen int64) {
	lastMinute, lastTen = c.requests.rate(time.Now())
	if lastTen > 0 {
		return lastMinute, lastTen
	}
	// Nothing in memory: this is a one-shot process (`doctor`, `status`), and the only
	// honest source is what the daemon last wrote down.
	persisted := c.readClientState()
	return persisted.LastMinute, persisted.LastTen
}

// clientState is what a one-shot command needs to know about the phone, written by the
// daemon because that is the process the phone actually talks to.
//
// This exists because the check-up used to answer a question it could not see: "last
// request from the app" lived in the daemon's memory, so `perch-bridge doctor` — a
// separate process, and the command people actually run — always reported "no request
// from the app yet", including after a successful trial. A diagnostic that is wrong in
// the optimistic direction is the worst kind.
type clientState struct {
	LastRequestUnix int64 `json:"last_request_unix"`
	LastMinute      int64 `json:"last_minute"`
	LastTen         int64 `json:"last_ten"`
	WrittenUnix     int64 `json:"written_unix"`
}

// clientWriteInterval bounds how often this is written to disk.
//
// It is deliberately not every request: the state this exists to detect is a *loop*,
// and a file write per request would turn a burst of network traffic into a burst of
// disk traffic — the diagnostic making the illness worse.
const clientWriteInterval = 10 * time.Second

func (c *Core) persistClientState(now time.Time) {
	c.clientMu.Lock()
	if now.Unix()-c.clientWritten < int64(clientWriteInterval.Seconds()) {
		c.clientMu.Unlock()
		return
	}
	c.clientWritten = now.Unix()
	c.clientMu.Unlock()

	lastMinute, lastTen := c.requests.rate(now)
	if err := c.store.WriteJSON(state.FileClient, clientState{
		LastRequestUnix: now.Unix(),
		LastMinute:      lastMinute,
		LastTen:         lastTen,
		WrittenUnix:     now.Unix(),
	}); err != nil {
		c.log.Debug("could not record when the phone last asked", "err", err)
	}
}

func (c *Core) readClientState() clientState {
	var out clientState
	_ = c.store.ReadJSON(state.FileClient, &out)
	return out
}

// PersistedClientSeen reports when the phone last talked to the daemon, as recorded on
// disk, plus how stale that record is. Used by one-shot commands.
func (c *Core) PersistedClientSeen() (time.Time, time.Duration, bool) {
	state := c.readClientState()
	if state.LastRequestUnix == 0 {
		return time.Time{}, 0, false
	}
	seen := time.Unix(state.LastRequestUnix, 0)
	written := time.Unix(state.WrittenUnix, 0)
	return seen, time.Since(written), true
}
