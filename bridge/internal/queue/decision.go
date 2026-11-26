// Package queue derives the attention queue from herdr's own state.
//
// The queue is a *derived view*, not a second source of truth (constitution I).
// Every entry is rebuildable from a session snapshot, which is why the app can
// clear its cache without losing anything and why the bridge persists this only
// as a cache (data-model.md §2).
//
// The one thing the queue must never do is lose a request. That is why
// transitions are driven both by events and by periodic reconciliation: a
// missed event shows up as a pane herdr reports as blocked with no queue entry,
// and that creates one.
package queue

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/th2zz/perch/bridge/internal/actions"
	"github.com/th2zz/perch/bridge/internal/herdr"
	"github.com/th2zz/perch/bridge/internal/state"
)

// State is the lifecycle of a decision request.
type State string

// ErrNotPending means the request was already resolved (possibly on the
// computer) before the action arrived.
var ErrNotPending = errors.New("decision request is no longer pending")

// ErrUnknownDecision means the bridge has never heard of the request the phone
// is answering.
//
// This is worth its own error rather than a generic failure: it is the normal
// consequence of a phone that has been asleep, or of a bridge that was restarted
// while the app held an old view. The app should refresh, not report a bug.
var ErrUnknownDecision = errors.New("no such decision request")

// ErrInFlight means this request's command is already being carried out.
//
// Its own error because the right answer is different from every other refusal:
// nothing is wrong and nothing needs fixing — the user's earlier tap is on its way.
// Telling them "this request is no longer open" (the stale answer) would be a lie
// about a request that is open and being answered right now.
var ErrInFlight = errors.New("this request is already being handled")

// claimTTL bounds how long a claim is believed. It exists so a bridge that dies
// mid-command cannot leave a card nobody can ever act on again.
const claimTTL = 90 * time.Second

const (
	// StatePending means the agent is waiting and the user has not acted.
	StatePending State = "pending"
	// StateHandledLocally means the user acted from the phone.
	StateHandledLocally State = "handled_locally"
	// StateHandledElsewhere means the agent resumed without us — almost always
	// because the user answered on the computer. Spec US2 scenario 3: the phone
	// must not ask again.
	StateHandledElsewhere State = "handled_elsewhere"
	// StateExpired means it sat unanswered past the TTL.
	StateExpired State = "expired"
)

// Terminal reports whether the state is final.
func (s State) Terminal() bool { return s != StatePending }

// Decision is one request for the user's attention.
type Decision struct {
	ID             string `json:"id"`
	HostID         string `json:"host_id"`
	PaneID         string `json:"pane_id"`
	WorkspaceID    string `json:"workspace_id"`
	WorkspaceLabel string `json:"workspace_label"`
	TabID          string `json:"tab_id"`
	AgentKind      string `json:"agent_kind"`
	DisplayAgent   string `json:"display_agent"`
	Title          string `json:"title"`
	// Rule is herdr's detection rule at the moment the request was seen. It is
	// what makes approve/reject mappable at all.
	Rule string `json:"rule"`
	// PromptText is a short excerpt of what the agent is asking. It is shown on
	// the card and never leaves the host in a push payload.
	PromptText string `json:"prompt_text"`
	// SuggestedKind is the one-tap default. Approve for a blocked agent.
	SuggestedKind actions.Kind `json:"suggested_kind"`
	// Approvable/Rejectable say whether a deterministic key sequence exists.
	// When false the app must offer the escape hatch instead of a button that
	// would fail (contracts/herdr-usage.md §2).
	Approvable bool `json:"approvable"`
	Rejectable bool `json:"rejectable"`
	// Answerable says whether a typed reply can be delivered to this dialog.
	//
	// It is separate from Approvable/Rejectable because the mechanism and the
	// evidence differ: answering means writing text into the dialog, which only
	// makes sense where the dialog is waiting for text. Most dialogs are menus,
	// and offering "Answer" there would type a sentence into a menu.
	Answerable    bool   `json:"answerable"`
	UnmappableWhy string `json:"unmappable_reason,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	State     State     `json:"state"`
	// StateReason explains a terminal state in the user's terms.
	StateReason string `json:"state_reason,omitempty"`
}

// Observation is what the bridge knows when a pane changes status.
type Observation struct {
	Change         herdr.StatusChange
	HostID         string
	WorkspaceLabel string
	Rule           string
	PromptText     string
	ObservedAt     time.Time
	// Authoritative marks a reading that came from a session snapshot rather
	// than from an event. Authoritative readings bypass the duplicate-event
	// guard, because a snapshot is herdr's current truth rather than a queued
	// frame that may already be stale.
	Authoritative bool
}

// Event reports what a transition did.
type Event struct {
	Created  *Decision
	Resolved *Decision
}

// Queue holds pending and recently resolved decisions.
type Queue struct {
	mu  sync.Mutex
	st  *state.Store
	ttl time.Duration

	byID   map[string]*Decision
	byPane map[string]string // host-scoped pane key -> decision id while pending
	values []ValueRecord
	seq    uint64

	// inFlight remembers decisions whose command is being executed right now.
	//
	// It exists because "read the state, do something slow, then set the state" is
	// not a decision procedure: two callers can both read `pending`, both run the
	// herdr command, and press Enter twice into somebody's dialog. The window is not
	// theoretical — the notification action and the card in the app are two entry
	// points for the same request, and a phone can deliver the tap to both.
	//
	// Entries carry a timestamp rather than being removed only on success, so a
	// process that dies mid-flight cannot wedge a request for ever.
	inFlight map[string]time.Time

	// handledAt remembers when a pane was last resolved from this phone.
	//
	// It exists because herdr's event stream can deliver a status change that
	// was already superseded: the user taps Approve, we send the key, and a
	// queued "blocked" frame arrives afterwards. Without this guard that frame
	// mints a second card, and a second tap would press Enter into an agent
	// that had already moved on. Only the event path is suppressed, never the
	// authoritative snapshot path, so a genuine second request still appears.
	handledAt map[string]time.Time
	suppress  time.Duration
}

// Options configures the queue.
type Options struct {
	// TTL is how long an unanswered request stays actionable. Defaults to 30
	// minutes: long enough to finish a meeting, short enough that a stale card
	// cannot be tapped by accident much later.
	TTL time.Duration
	// SuppressAfterHandled is how long a blocked reading for a pane is ignored after
	// the user resolved that pane from the phone — from an event *or* from a snapshot,
	// since both describe the same question.
	//
	// Defaults to a minute, which is longer than it used to be and safe for the same
	// reason it is now applied to snapshots: the marker is cleared the moment the pane
	// is seen *unblocked*, so a genuinely new request (which requires the old dialog to
	// close first) is never hidden. What the window bounds is the other case — an
	// approval that did not take effect — so the user gets a card back rather than a
	// permanently suppressed one.
	SuppressAfterHandled time.Duration
}

// New builds a queue and loads any cached decisions. A corrupt or missing cache
// is not an error: the cache is disposable by design.
func New(st *state.Store, opts Options) *Queue {
	if opts.TTL <= 0 {
		opts.TTL = 30 * time.Minute
	}
	if opts.SuppressAfterHandled <= 0 {
		opts.SuppressAfterHandled = time.Minute
	}
	q := &Queue{
		st:        st,
		ttl:       opts.TTL,
		byID:      map[string]*Decision{},
		byPane:    map[string]string{},
		handledAt: map[string]time.Time{},
		inFlight:  map[string]time.Time{},
		suppress:  opts.SuppressAfterHandled,
	}
	if st != nil {
		var cached []Decision
		if err := st.ReadJSON(state.FileDecisions, &cached); err == nil {
			for i := range cached {
				d := cached[i]
				if d.State == StatePending {
					// The mapping is derived data, and a bridge upgrade can change what
					// the same dialog means. Recompute it on load so a card cached under
					// the old key table does not keep a missing or wrong button.
					q.applyMappingLocked(&d)
					q.byPane[paneKey(d.HostID, d.PaneID)] = d.ID
				}
				q.byID[d.ID] = &d
			}
		}
	}
	q.loadValues()
	return q
}

// TTL returns the configured expiry.
func (q *Queue) TTL() time.Duration { return q.ttl }

// Get returns one decision.
func (q *Queue) Get(id string) (*Decision, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	d, ok := q.byID[id]
	if !ok {
		return nil, false
	}
	cp := *d
	return &cp, true
}

// List returns decisions, newest first. includeClosed also returns terminal
// entries, which the app uses for its "already handled elsewhere" note.
func (q *Queue) List(includeClosed bool) []Decision {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.listLocked(includeClosed)
}

func (q *Queue) listLocked(includeClosed bool) []Decision {
	out := make([]Decision, 0, len(q.byID))
	for _, d := range q.byID {
		if !includeClosed && d.State.Terminal() {
			continue
		}
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (q *Queue) newID(hostID, paneID string, at time.Time) string {
	q.seq++
	if hostID == "" {
		hostID = "local"
	}
	return fmt.Sprintf("%s/%s@%d.%d", hostID, paneID, at.Unix(), q.seq)
}

// paneKey scopes a pane id to the machine it belongs to. Everything that
// remembers something about a pane — the pending index, the
// "just handled here" suppression — goes through this, so one computer's
// `w1:p1` can never answer for another's.
func paneKey(hostID, paneID string) string {
	return normalizeHostID(hostID) + "\x00" + paneID
}

// normalizeHostID treats an absent host as this machine. Every caller that has
// no host in hand (a decision stored before FR-016, a one-host test) means the
// local machine, and saying so in one place keeps the two id spaces from
// drifting apart.
func normalizeHostID(hostID string) string {
	if hostID == "" {
		return "local"
	}
	return hostID
}

func (q *Queue) persistLocked() {
	if q.st == nil {
		return
	}
	// Keep the file bounded by age *and* by count.
	//
	// Age alone is not a bound. An agent that asks for approval in a loop — a test
	// suite retrying, an agent stuck on a permission it keeps re-requesting — produces
	// a terminal decision per lap, and the whole file is rewritten on every change. At
	// one per second that is eighty-six thousand entries within the day, each write
	// serialising all of them: megabytes of writes per hour to keep a history nobody
	// reads. Copying the write, not the read, is what made this worth bounding.
	all := q.listLocked(true)
	cutoff := time.Now().Add(-24 * time.Hour)
	trimmed := make([]Decision, 0, len(all))
	for _, d := range all {
		if d.State.Terminal() && d.UpdatedAt.Before(cutoff) {
			continue
		}
		trimmed = append(trimmed, d)
	}
	if len(trimmed) > maxPersistedDecisions {
		// Pending requests are never dropped: they are live work, and losing one from
		// disk would change its identity under a tap that is already in flight. Only
		// the closed history is bounded, newest first.
		kept := make([]Decision, 0, maxPersistedDecisions)
		var closed []Decision
		for _, d := range trimmed {
			if d.State.Terminal() {
				closed = append(closed, d)
				continue
			}
			kept = append(kept, d)
		}
		sort.SliceStable(closed, func(i, j int) bool {
			return closed[i].CreatedAt.After(closed[j].CreatedAt)
		})
		if room := maxPersistedDecisions - len(kept); room > 0 && len(closed) > room {
			closed = closed[:room]
		}
		trimmed = append(kept, closed...)
	}
	_ = q.st.WriteJSON(state.FileDecisions, trimmed)
}

// maxPersistedDecisions bounds the queue file. Pending requests are never dropped
// (there are as many as there are waiting agents), and this is far more closed history
// than any screen shows.
const maxPersistedDecisions = 200
