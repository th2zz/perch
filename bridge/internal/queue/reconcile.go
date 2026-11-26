package queue

import (
	"fmt"
	"time"

	"github.com/th2zz/perch/bridge/internal/actions"
	"github.com/th2zz/perch/bridge/internal/herdr"
)

// Observe applies one status change and returns what happened.
//
// Transitions, in full:
//
//	blocked  + no pending entry  -> create a Pending decision
//	blocked  + pending entry     -> no-op (it is the same request; herdr may
//	                                re-emit the state on its own schedule)
//	unblocked + pending entry    -> HandledElsewhere: the agent resumed without
//	                                us, most likely because the user answered on
//	                                the computer (spec US2 scenario 3)
//	anything  + terminal entry   -> no-op; a later blocked state mints a new
//	                                decision, which is what a second approval
//	                                prompt in the same pane actually is
func (q *Queue) Observe(obs Observation) Event {
	at := obs.ObservedAt
	if at.IsZero() {
		at = time.Now()
	}
	pane := obs.Change.PaneID
	if pane == "" {
		return Event{}
	}
	key := paneKey(obs.HostID, pane)

	q.mu.Lock()
	defer q.mu.Unlock()

	curID, hasPending := q.byPane[key]
	cur := q.byID[curID]

	if !obs.Change.AgentStatus.NeedsDecision() {
		// The dialog is gone, so anything this pane was "just handled" for is over:
		// the next time it blocks is a new request, and it must be allowed to appear.
		// This is what makes the suppression below safe to extend to snapshots.
		delete(q.handledAt, key)
		if hasPending && cur != nil && !cur.State.Terminal() {
			cur.State = StateHandledElsewhere
			cur.StateReason = "the agent resumed before you acted — most likely handled on the computer"
			cur.UpdatedAt = at
			delete(q.byPane, key)
			q.persistLocked()
			resolved := *cur
			return Event{Resolved: &resolved}
		}
		return Event{}
	}

	if hasPending && cur != nil && !cur.State.Terminal() {
		// Same request; refresh the excerpt if we now know more, but keep the
		// identity stable so the user's in-flight tap still lands.
		if cur.Rule == "" && obs.Rule != "" {
			cur.Rule = obs.Rule
			q.applyMappingLocked(cur)
		}
		if cur.PromptText == "" && obs.PromptText != "" {
			cur.PromptText = obs.PromptText
			// The selected option, not only the rule, can decide what Approve
			// means (Claude's folder-trust question), so a later excerpt has to
			// update the capabilities as well.
			q.applyMappingLocked(cur)
		}
		cur.UpdatedAt = at
		return Event{}
	}

	// A blocked reading for a pane the user just handled — with the pane never having
	// been seen *unblocked* in between — is the same dialog, not a second request.
	//
	// This used to suppress only event readings, reasoning that a snapshot is truth and
	// a genuinely new prompt must not be hidden. Half right: what tells "the same dialog,
	// still up while the agent reacts" from "a new request" is not where the reading came
	// from, it is whether the pane left the blocked state in between — and the marker is
	// cleared exactly when it does. Without this, approving a dialog whose agent takes a
	// moment to move mints a second card, and the second tap presses Enter again.
	if handled, ok := q.handledAt[key]; ok && at.Before(handled.Add(q.suppress)) {
		return Event{}
	}

	d := &Decision{
		ID:             q.newID(obs.HostID, pane, at),
		HostID:         obs.HostID,
		PaneID:         pane,
		WorkspaceID:    obs.Change.WorkspaceID,
		WorkspaceLabel: obs.WorkspaceLabel,
		AgentKind:      herdr.Str(obs.Change.Agent),
		DisplayAgent:   herdr.Str(obs.Change.DisplayAgent),
		Title:          herdr.Str(obs.Change.Title),
		Rule:           obs.Rule,
		PromptText:     obs.PromptText,
		SuggestedKind:  actions.KindApprove,
		CreatedAt:      at,
		UpdatedAt:      at,
		State:          StatePending,
	}
	q.applyMappingLocked(d)
	q.byID[d.ID] = d
	q.byPane[key] = d.ID
	q.persistLocked()
	created := *d
	return Event{Created: &created}
}

// Claim takes exclusive responsibility for carrying out a decision's command.
//
// Every write path goes through here before it touches herdr, and the ordering is
// the point: the command is what presses a key in somebody's terminal, so exactly
// one caller must be doing it. A second caller is told "already being handled"
// instead of being allowed to repeat the action.
//
// The claim is released by [Release] when an attempt fails, so a user whose laptop
// was asleep can tap again. It is *not* released on success: the decision is
// terminal then, and a third tap must not resurrect it.
//
// **Known limit, stated rather than hidden:** these claims live in memory, so a bridge
// killed between running the command and recording the result can be asked to run it
// once more — the classic at-least-once residue of a crash. The alternative was
// considered and rejected for now: persisting the claim would leave the user with a
// card they cannot act on and a sentence they can do nothing about, and herdr exposes
// no idempotency key that would let us settle the question ourselves. The window is
// the time between the herdr write and a local file write.
func (q *Queue) Claim(id string, now time.Time) (*Decision, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	d, ok := q.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownDecision, id)
	}
	if d.State.Terminal() {
		return nil, fmt.Errorf("%w: this request is already %s", ErrNotPending, d.State)
	}
	q.pruneClaimedLocked(now)
	if _, busy := q.inFlight[id]; busy {
		return nil, fmt.Errorf("%w: %s", ErrInFlight, id)
	}
	q.inFlight[id] = now

	cp := *d
	return &cp, nil
}

// Release gives up a claim, so the request can be attempted again.
func (q *Queue) Release(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.inFlight, id)
}

// pruneClaimedLocked drops claims old enough to be a dead attempt rather than a
// running one — a bridge killed mid-command must not leave a card the user can
// never act on again.
func (q *Queue) pruneClaimedLocked(now time.Time) {
	for id, at := range q.inFlight {
		if now.Sub(at) > claimTTL {
			delete(q.inFlight, id)
		}
	}
}

// applyMappingLocked records whether the one-tap actions can actually be
// carried out. Consulted at creation and again whenever the rule becomes known,
// so the card never offers a button that would be refused.
func (q *Queue) applyMappingLocked(d *Decision) {
	keys, reason, ok := actions.LookupKeysForPrompt(d.AgentKind, d.Rule, d.PromptText)
	recognised := d.AgentKind != ""

	d.Approvable = recognised && len(keys.Approve) > 0
	d.Rejectable = recognised && len(keys.Reject) > 0
	d.Answerable = recognised && keys.AcceptsTypedAnswer()
	d.UnmappableWhy = ""
	if !recognised {
		d.UnmappableWhy = "herdr does not detect an agent in this session"
		return
	}
	if !ok && reason != "" {
		d.UnmappableWhy = reason
		return
	}
	if !d.Approvable && reason != "" {
		// A partially mappable dialog can still explain why one direction is
		// missing; the trust prompt with an unreadable cursor is that case.
		d.UnmappableWhy = reason
	}
	if !d.Approvable && !d.Rejectable && d.UnmappableWhy == "" {
		d.UnmappableWhy = reason
	}
}

// MarkHandledLocally records that the user acted from the phone. It is called
// after the herdr command succeeded, so a failed action leaves the card
// actionable.
func (q *Queue) MarkHandledLocally(id string, at time.Time) (*Decision, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	d, ok := q.byID[id]
	if !ok || d.State.Terminal() {
		return nil, false
	}
	if at.IsZero() {
		at = time.Now()
	}
	d.State = StateHandledLocally
	d.StateReason = "you handled this from your phone"
	d.UpdatedAt = at
	key := paneKey(d.HostID, d.PaneID)
	if q.byPane[key] == d.ID {
		delete(q.byPane, key)
	}
	q.handledAt[key] = at
	q.persistLocked()
	cp := *d
	return &cp, true
}

// SetPromptText fills in the excerpt after the fact. It exists because reading
// pane output is slower than emitting the event, and a notification must not
// wait on it.
func (q *Queue) SetPromptText(id, text string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	d, ok := q.byID[id]
	if !ok || d.PromptText != "" {
		return
	}
	d.PromptText = text
	// The prompt can change which key means yes (see applyMappingLocked), so the
	// card's capabilities are recomputed with the text it now carries.
	q.applyMappingLocked(d)
	d.UpdatedAt = time.Now()
	q.persistLocked()
}

// MarkUnmappable records that a tap could not be translated into keys, so the
// app stops offering the button and points at the escape hatch instead.
func (q *Queue) MarkUnmappable(id, reason string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	d, ok := q.byID[id]
	if !ok {
		return
	}
	d.Approvable = false
	d.Rejectable = false
	d.UnmappableWhy = reason
	d.UpdatedAt = time.Now()
	q.persistLocked()
}

// Prune expires stale pending decisions and drops long-terminal ones.
func (q *Queue) Prune(now time.Time) []Decision {
	q.mu.Lock()
	defer q.mu.Unlock()
	var expired []Decision
	for id, d := range q.byID {
		if d.State != StatePending {
			continue
		}
		if now.Sub(d.CreatedAt) < q.ttl {
			continue
		}
		d.State = StateExpired
		d.StateReason = "this request sat unanswered past the " + q.ttl.String() + " window"
		d.UpdatedAt = now
		if key := paneKey(d.HostID, d.PaneID); q.byPane[key] == id {
			delete(q.byPane, key)
		}
		expired = append(expired, *d)
	}
	if len(expired) > 0 {
		q.persistLocked()
	}
	return expired
}

// ReconcileResult reports what an authoritative pass changed.
type ReconcileResult struct {
	// Created are requests the events had missed.
	Created []Decision
	// Resolved are requests that are no longer answerable because the agent
	// moved on or the session ended.
	Resolved []Decision
}

func (q *Queue) Reconcile(
	panes []herdr.Pane,
	hostID string,
	labels map[string]string,
	now time.Time,
) ReconcileResult {
	return q.ReconcileInventory(Inventory{Panes: panes, HostID: hostID, Labels: labels, At: now})
}

// Inventory is one machine's pane list, with whatever else is already known about
// the dialogs on it.
//
// It exists as a struct because the two callers know different amounts: the local
// path learns a rule from the event stream, while a polled machine has to ask
// herdr before it can say what a dialog wants. Facts lets the second caller hand in
// what it found, so a card is born complete instead of appearing once with no
// buttons and again with them.
type Inventory struct {
	Panes  []herdr.Pane
	HostID string
	Labels map[string]string
	At     time.Time
	// Facts returns the detection rule and excerpt for a pane, or zero values when
	// nothing is known. Optional.
	Facts func(herdr.Pane) (rule, excerpt string)
}

// ReconcileInventory is Reconcile with the optional facts.
func (q *Queue) ReconcileInventory(inv Inventory) ReconcileResult {
	var result ReconcileResult
	panes, hostID, labels, now := inv.Panes, inv.HostID, inv.Labels, inv.At

	// Direction one: anything pending whose pane is gone, or whose agent is no
	// longer waiting, is no longer a live request.
	present := make(map[string]herdr.AgentStatus, len(panes))
	for _, p := range panes {
		present[p.PaneID] = p.AgentStatus
	}
	result.Resolved = q.resolveUnanswerable(hostID, present, now)
	// A pane that is present and no longer blocked has left its dialog, so whatever we
	// suppressed for it is over — and the next time it blocks, that is a new request.
	// This has to happen here as well as on the event path: `Reconcile` only ever hands
	// *blocked* panes to Observe, so a dialog closing is invisible to it otherwise.
	q.clearSameDialogMarkers(hostID, panes)

	// Direction two: anything waiting that the queue does not know about yet.
	for _, p := range panes {
		if !p.AgentStatus.NeedsDecision() {
			continue
		}
		var rule, excerpt string
		if inv.Facts != nil {
			rule, excerpt = inv.Facts(p)
		}
		ev := q.Observe(Observation{
			Change: herdr.StatusChange{
				PaneID:       p.PaneID,
				WorkspaceID:  p.WorkspaceID,
				Agent:        p.Agent,
				DisplayAgent: p.DisplayAgent,
				Title:        p.Title,
				AgentStatus:  p.AgentStatus,
				StateLabels:  p.StateLabels,
			},
			HostID:         hostID,
			WorkspaceLabel: labels[p.WorkspaceID],
			Rule:           rule,
			PromptText:     excerpt,
			ObservedAt:     now,
			Authoritative:  true,
		})
		if ev.Created != nil {
			result.Created = append(result.Created, *ev.Created)
		}
	}
	return result
}

// clearSameDialogMarkers forgets "just handled here" for panes that are present and
// not blocked.
//
// A pane that has *vanished* keeps its marker on purpose: disappearing is not the same
// as closing, and herdr is the authority on which happened.
func (q *Queue) clearSameDialogMarkers(hostID string, panes []herdr.Pane) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, pane := range panes {
		if pane.AgentStatus.NeedsDecision() {
			continue
		}
		delete(q.handledAt, paneKey(hostID, pane.PaneID))
	}
}

func (q *Queue) resolveUnanswerable(
	hostID string,
	present map[string]herdr.AgentStatus,
	now time.Time,
) []Decision {
	q.mu.Lock()
	defer q.mu.Unlock()

	var resolved []Decision
	for id, d := range q.byID {
		if d.State != StatePending {
			continue
		}
		if normalizeHostID(d.HostID) != normalizeHostID(hostID) {
			continue
		}
		status, exists := present[d.PaneID]
		if exists && status.NeedsDecision() {
			continue
		}

		if exists {
			d.State = StateHandledElsewhere
			d.StateReason = "the agent resumed before you acted — most likely handled on the computer"
		} else {
			d.State = StateHandledElsewhere
			d.StateReason = "that session ended before you answered"
		}
		d.UpdatedAt = now
		if key := paneKey(d.HostID, d.PaneID); q.byPane[key] == id {
			delete(q.byPane, key)
		}
		resolved = append(resolved, *d)
	}
	if len(resolved) > 0 {
		q.persistLocked()
	}
	return resolved
}
