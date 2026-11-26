package queue

import (
	"time"

	"github.com/th2zz/perch/bridge/internal/actions"
	"github.com/th2zz/perch/bridge/internal/state"
)

// BaselineSeconds is the explicit, user-visible baseline used to turn "you
// decided from your phone" into "you saved time".
//
// Spec FR-013 requires the estimate to come from a stated baseline rather than
// a black box, and it names the number: walking to the computer and completing
// the same decision takes a median of 90 seconds. The app shows this constant
// next to the total. If the baseline is ever changed, previously recorded
// seconds are not rewritten — ValueRecord stores its own figure.
const BaselineSeconds = 90

// ValueRecord is the only product-owned datum that survives cache clearing
// (data-model.md §2): it is attribution, not state, so it cannot be rebuilt
// from herdr.
type ValueRecord struct {
	DecisionID   string       `json:"decision_id"`
	DecidedAt    time.Time    `json:"decided_at"`
	Kind         actions.Kind `json:"kind"`
	OnDevice     bool         `json:"on_device"`
	SecondsSaved int          `json:"seconds_saved"`
}

// ValueStats is the weekly summary shown to the user.
type ValueStats struct {
	Decisions       int `json:"decisions"`
	OnDevice        int `json:"not_at_computer"`
	SecondsSaved    int `json:"seconds_saved"`
	BaselineSeconds int `json:"baseline_seconds"`
}

// RecordValue appends an attribution record.
func (q *Queue) RecordValue(rec ValueRecord) {
	if rec.DecidedAt.IsZero() {
		rec.DecidedAt = time.Now()
	}
	if rec.SecondsSaved == 0 && rec.OnDevice {
		rec.SecondsSaved = BaselineSeconds
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.values = append(q.values, rec)
	q.persistValuesLocked()
}

// ValueStatsSince summarises recorded decisions after a cutoff.
func (q *Queue) ValueStatsSince(since time.Time) ValueStats {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := ValueStats{BaselineSeconds: BaselineSeconds}
	for _, v := range q.values {
		if v.DecidedAt.Before(since) {
			continue
		}
		out.Decisions++
		if v.OnDevice {
			out.OnDevice++
		}
		out.SecondsSaved += v.SecondsSaved
	}
	return out
}

func (q *Queue) loadValues() {
	if q.st == nil {
		return
	}
	var values []ValueRecord
	if err := q.st.ReadJSON(state.FileValue, &values); err == nil {
		q.values = values
	}
}

func (q *Queue) persistValuesLocked() {
	if q.st == nil {
		return
	}
	// Bound the file: a year of decisions is plenty for a weekly summary and
	// keeps the state directory small on embedded hosts.
	const keep = 5000
	values := q.values
	if len(values) > keep {
		values = values[len(values)-keep:]
	}
	_ = q.st.WriteJSON(state.FileValue, values)
}
