package queue

import (
	"strings"
	"testing"
	"time"

	"github.com/th2zz/perch/bridge/internal/actions"
	"github.com/th2zz/perch/bridge/internal/herdr"
)

func obs(pane string, status herdr.AgentStatus, rule string, at time.Time) Observation {
	return obsFor("codex", pane, status, rule, at)
}

// obsFor names the agent, because a rule belongs to one agent: matching by rule
// name alone is exactly the guessing the product forbids.
func obsFor(agentKind, pane string, status herdr.AgentStatus, rule string, at time.Time) Observation {
	agent := agentKind
	return Observation{
		Change: herdr.StatusChange{
			PaneID:      pane,
			WorkspaceID: "wA",
			Agent:       &agent,
			AgentStatus: status,
		},
		HostID:         "local",
		WorkspaceLabel: "project",
		Rule:           rule,
		ObservedAt:     at,
	}
}

// TestBlockedCreatesOneDecisionAndRepeatsDoNot covers herdr re-emitting the
// same state: a second card for the same request would be a defect.
func TestBlockedCreatesOneDecisionAndRepeatsDoNot(t *testing.T) {
	q := New(nil, Options{})
	now := time.Now()

	first := q.Observe(obs("wA:p1", herdr.StatusBlocked, "live_strong_blocker", now))
	if first.Created == nil {
		t.Fatal("a blocked agent must create a decision")
	}
	second := q.Observe(obs("wA:p1", herdr.StatusBlocked, "live_strong_blocker", now.Add(time.Second)))
	if second.Created != nil || second.Resolved != nil {
		t.Fatalf("a repeated blocked state must be a no-op, got %+v", second)
	}
	if got := len(q.List(false)); got != 1 {
		t.Fatalf("pending decisions = %d, want 1", got)
	}
}

// TestResumedAgentMarksHandledElsewhere is spec US2 scenario 3.
func TestResumedAgentMarksHandledElsewhere(t *testing.T) {
	q := New(nil, Options{})
	now := time.Now()
	created := q.Observe(obsFor("claude", "wA:p1", herdr.StatusBlocked, "bash_permission_prompt", now)).Created

	res := q.Observe(obs("wA:p1", herdr.StatusWorking, "", now.Add(30*time.Second)))
	if res.Resolved == nil {
		t.Fatal("the agent resumed without us; the card must resolve")
	}
	if res.Resolved.State != StateHandledElsewhere {
		t.Errorf("state = %q, want handled_elsewhere", res.Resolved.State)
	}
	if res.Resolved.StateReason == "" {
		t.Error("the user must be told why the request disappeared")
	}
	if res.Resolved.ID != created.ID {
		t.Errorf("resolved the wrong request: %s vs %s", res.Resolved.ID, created.ID)
	}
	if got := len(q.List(false)); got != 0 {
		t.Errorf("pending decisions = %d, want 0", got)
	}
}

// TestSecondApprovalPromptMintsANewDecision: a pane can block again later, and
// that is a new request, not the old one.
func TestSecondApprovalPromptMintsANewDecision(t *testing.T) {
	q := New(nil, Options{})
	now := time.Now()
	first := q.Observe(obsFor("claude", "wA:p1", herdr.StatusBlocked, "bash_permission_prompt", now)).Created
	q.Observe(obs("wA:p1", herdr.StatusWorking, "", now.Add(time.Second)))

	again := q.Observe(obsFor("claude", "wA:p1", herdr.StatusBlocked, "bash_permission_prompt", now.Add(2*time.Second)))
	if again.Created == nil {
		t.Fatal("a second approval prompt must create a second decision")
	}
	if again.Created.ID == first.ID {
		t.Error("the new request must have its own id")
	}
}

// TestMappabilityIsRecordedSoTheAppNeverOffersADeadButton.
func TestMappabilityIsRecorded(t *testing.T) {
	q := New(nil, Options{})
	now := time.Now()

	known := q.Observe(obsFor("claude", "wA:p1", herdr.StatusBlocked, "bash_permission_prompt", now)).Created
	if !known.Approvable || !known.Rejectable {
		t.Errorf("claude/bash_permission_prompt has both keys: %+v", known)
	}
	if known.AgentKind != "claude" {
		t.Errorf("agent kind lost: %q", known.AgentKind)
	}
	if known.Answerable {
		t.Error("claude/bash_permission_prompt is a menu, not a question that takes an answer")
	}

	unknownRule := q.Observe(obs("wB:p1", herdr.StatusBlocked, "some_new_dialog", now)).Created
	if unknownRule.Approvable || unknownRule.Rejectable {
		t.Error("an unmappable prompt must not offer buttons")
	}
	if unknownRule.UnmappableWhy == "" {
		t.Error("the card must explain why instead of failing silently")
	}
}

func withPrompt(o Observation, prompt string) Observation {
	o.PromptText = prompt
	return o
}

// The home-folder trust prompt is mappable, but only because the queue knows the
// cursor is on No. This is the exact card that used to exit Claude when approved.
func TestClaudeHomeTrustStaysApprovableForTheRightReason(t *testing.T) {
	q := New(nil, Options{})
	now := time.Now()
	home := "\n Quick safety check: Is this a project you created or one you trust?\n\n" +
		" ❯ No, exit\n   Yes, I trust this folder\n\n Enter to confirm · Esc to cancel"

	created := q.Observe(withPrompt(
		obsFor("claude", "wA:p1", herdr.StatusBlocked, "live_blocked_form", now), home,
	)).Created
	if created == nil {
		t.Fatal("a blocked pane must create a card")
	}
	if !created.Approvable || !created.Rejectable {
		t.Errorf("the trust prompt offers both directions: %+v", created)
	}
}

// If the excerpt arrives after the card (the event is faster than the pane read),
// recomputing the mapping must be able to take Approve away again. Otherwise the
// card keeps a button that would press Enter into an unreadable question.
func TestLatePromptTextCanRevokeABlindApprove(t *testing.T) {
	q := New(nil, Options{})
	now := time.Now()
	created := q.Observe(obsFor("claude", "wA:p1", herdr.StatusBlocked, "live_blocked_form", now)).Created
	if created == nil || !created.Approvable {
		t.Fatalf("the initial card should be approvable before the excerpt arrives: %+v", created)
	}

	// Both options, no cursor marker: recognisable, but which one Enter would pick
	// is unknowable.
	q.SetPromptText(created.ID, "\n Quick safety check: Is this a project you created or one you trust?\n"+
		"\n   No, exit\n   Yes, I trust this folder\n\n Enter to confirm · Esc to cancel")

	updated, ok := q.Get(created.ID)
	if !ok {
		t.Fatal("the card disappeared")
	}
	if updated.Approvable {
		t.Error("Approve must be withdrawn when the cursor cannot be read")
	}
	if !updated.Rejectable {
		t.Error("Esc is still a safe way to leave the question")
	}
	if updated.UnmappableWhy == "" {
		t.Error("the card must say why Approve is gone")
	}
}

// TestPruneExpiresStaleRequests: a card left for an hour must not stay tappable.
func TestPruneExpiresStaleRequests(t *testing.T) {
	q := New(nil, Options{TTL: time.Minute})
	now := time.Now()
	q.Observe(obs("wA:p1", herdr.StatusBlocked, "live_strong_blocker", now))

	if expired := q.Prune(now.Add(30 * time.Second)); len(expired) != 0 {
		t.Fatalf("nothing should expire before the TTL: %+v", expired)
	}
	expired := q.Prune(now.Add(2 * time.Minute))
	if len(expired) != 1 || expired[0].State != StateExpired {
		t.Fatalf("expected one expiry, got %+v", expired)
	}
	if len(q.List(false)) != 0 {
		t.Error("expired requests must leave the pending list")
	}
}

// TestReconcileCatchesAMissedEvent is the backstop: herdr says the pane is
// blocked, the queue has nothing, so a request appears anyway.
func TestReconcileCatchesAMissedEvent(t *testing.T) {
	q := New(nil, Options{})
	agent := "claude"
	panes := []herdr.Pane{{
		PaneID:      "wC:p1",
		WorkspaceID: "wC",
		Agent:       &agent,
		AgentStatus: herdr.StatusBlocked,
	}}
	created := q.Reconcile(panes, "local", map[string]string{"wC": "cli-tools"}, time.Now())
	if len(created.Created) != 1 {
		t.Fatalf("reconcile must create the missing request, got %d", len(created.Created))
	}
	if created.Created[0].WorkspaceLabel != "cli-tools" {
		t.Errorf("workspace label lost: %q", created.Created[0].WorkspaceLabel)
	}
	if again := q.Reconcile(panes, "local", nil, time.Now()); len(again.Created) != 0 {
		t.Errorf("reconcile is not idempotent: %+v", again)
	}
}

// TestReconcileResolvesTheOtherDirection is the bug a verification run found:
// when herdr reports a state through a lifecycle event and a snapshot rather than
// a per-pane status change, the create path fired and the resolve path did not.
// The card stayed on the phone asking for something already done.
func TestReconcileResolvesTheOtherDirection(t *testing.T) {
	q := New(nil, Options{})
	now := time.Now()
	agent := "codex"
	blocked := []herdr.Pane{{
		PaneID:      "wA:p1",
		WorkspaceID: "wA",
		Agent:       &agent,
		AgentStatus: herdr.StatusBlocked,
	}}
	q.Reconcile(blocked, "local", nil, now)
	if pending := q.List(false); len(pending) != 1 {
		t.Fatalf("setup: pending = %d, want 1", len(pending))
	}

	// The agent moved on. An authoritative read must close the card.
	working := []herdr.Pane{{
		PaneID:      "wA:p1",
		WorkspaceID: "wA",
		Agent:       &agent,
		AgentStatus: herdr.StatusWorking,
	}}
	result := q.Reconcile(working, "local", nil, now.Add(time.Minute))
	if len(result.Resolved) != 1 {
		t.Fatalf("resolved = %d, want 1", len(result.Resolved))
	}
	if result.Resolved[0].State != StateHandledElsewhere {
		t.Errorf("state = %q, want handled_elsewhere", result.Resolved[0].State)
	}
	if len(q.List(false)) != 0 {
		t.Error("the card must leave the pending list")
	}
}

// TestReconcileClosesRequestsForVanishedSessions: a pane that closed can never
// be answered, so the card must stop asking.
func TestReconcileClosesRequestsForVanishedSessions(t *testing.T) {
	q := New(nil, Options{})
	now := time.Now()
	agent := "claude"
	q.Reconcile([]herdr.Pane{{
		PaneID:      "wB:p9",
		WorkspaceID: "wB",
		Agent:       &agent,
		AgentStatus: herdr.StatusBlocked,
	}}, "local", nil, now)

	result := q.Reconcile([]herdr.Pane{}, "local", nil, now.Add(time.Minute))
	if len(result.Resolved) != 1 {
		t.Fatalf("resolved = %d, want 1", len(result.Resolved))
	}
	if reason := result.Resolved[0].StateReason; reason == "" ||
		!strings.Contains(reason, "session ended") {
		t.Errorf("the reason must explain a vanished session, got %q", reason)
	}
}

// TestReconcileLeavesResolvedRequestsAlone keeps the resolve direction from
// resurrecting or re-closing things that are already settled.
func TestReconcileLeavesResolvedRequestsAlone(t *testing.T) {
	q := New(nil, Options{})
	now := time.Now()
	created := q.Observe(obs("wA:p1", herdr.StatusBlocked, "live_strong_blocker", now)).Created
	q.MarkHandledLocally(created.ID, now)

	result := q.Reconcile([]herdr.Pane{}, "local", nil, now.Add(time.Minute))
	if len(result.Resolved) != 0 || len(result.Created) != 0 {
		t.Fatalf("a settled queue must be untouched: %+v", result)
	}
}

// TestMarkHandledLocallyKeepsTheRequestClosed.
func TestMarkHandledLocallyKeepsTheRequestClosed(t *testing.T) {
	q := New(nil, Options{})
	now := time.Now()
	created := q.Observe(obs("wA:p1", herdr.StatusBlocked, "live_strong_blocker", now)).Created

	handled, ok := q.MarkHandledLocally(created.ID, now)
	if !ok || handled.State != StateHandledLocally {
		t.Fatalf("handled = %+v, ok = %v", handled, ok)
	}
	// A duplicate frame that was already in flight when the user tapped must not
	// bring the card back: a second tap would send another keypress.
	if ev := q.Observe(obs("wA:p1", herdr.StatusBlocked, "live_strong_blocker", now.Add(time.Second))); ev.Created != nil {
		t.Error("a duplicate blocked event right after a local decision must be suppressed")
	}
}

// TestAStillBlockedPaneDoesNotMintASecondCard.
//
// **This reverses an earlier decision, on evidence.** The guard used to apply only to
// events: a *snapshot* showing the pane still blocked would mint a fresh card, on the
// reasoning that "a snapshot is herdr's current truth, so it wins". Two things made
// that wrong:
//
//   - It is inconsistent. The same fact — this pane is still blocked — was suppressed
//     when it arrived as an event and honoured when it arrived as a snapshot.
//   - It is dangerous in the ordinary case. An agent takes a moment to react to the
//     Enter we sent; a snapshot in that window (and the app pulls one on every event)
//     shows "still blocked" and produces a second card. A second tap then presses Enter
//     into a dialog that was already answered — with a *new* decision id, so the
//     per-decision claim cannot help.
//
// What actually distinguishes "the same dialog" from "a new request" is not where the
// reading came from: it is whether the pane was ever seen *unblocked* in between, and
// the queue clears the marker that moment. So the guard now applies to both readings.
func TestAStillBlockedPaneDoesNotMintASecondCard(t *testing.T) {
	q := New(nil, Options{SuppressAfterHandled: time.Minute})
	now := time.Now()
	created := q.Observe(obs("wA:p1", herdr.StatusBlocked, "live_strong_blocker", now)).Created
	q.MarkHandledLocally(created.ID, now)

	agent := "codex"
	stillBlocked := []herdr.Pane{{
		PaneID: "wA:p1", WorkspaceID: "wA", Agent: &agent, AgentStatus: herdr.StatusBlocked,
	}}
	// The agent has not moved yet: same dialog, so no card — from either path.
	if reopened := q.Reconcile(stillBlocked, "local", nil, now.Add(2*time.Second)); len(reopened.Created) != 0 {
		t.Errorf("a still-blocked pane minted %d new cards", len(reopened.Created))
	}
	if ev := q.Observe(obs("wA:p1", herdr.StatusBlocked, "live_strong_blocker", now.Add(3*time.Second))); ev.Created != nil {
		t.Error("a blocked event for the same dialog minted a card")
	}
}

// TestANewRequestAfterTheDialogClosesAppearsImmediately is the other half, and the
// reason the rule above is safe: a genuinely new request requires the old dialog to
// close first, which is exactly what clears the marker.
func TestANewRequestAfterTheDialogClosesAppearsImmediately(t *testing.T) {
	q := New(nil, Options{SuppressAfterHandled: time.Minute})
	now := time.Now()
	created := q.Observe(obs("wA:p1", herdr.StatusBlocked, "live_strong_blocker", now)).Created
	q.MarkHandledLocally(created.ID, now)

	agent := "codex"
	pane := func(status herdr.AgentStatus) []herdr.Pane {
		return []herdr.Pane{{PaneID: "wA:p1", WorkspaceID: "wA", Agent: &agent, AgentStatus: status}}
	}
	// The dialog closes (the agent moved on), and then a new one opens — well inside
	// the suppression window.
	q.Reconcile(pane(herdr.StatusWorking), "local", nil, now.Add(2*time.Second))
	next := q.Reconcile(pane(herdr.StatusBlocked), "local", nil, now.Add(3*time.Second))

	if len(next.Created) != 1 {
		t.Fatalf("a new request after the dialog closed was hidden: %+v", next)
	}
	if next.Created[0].ID == created.ID {
		t.Error("the new request reused the resolved decision's id")
	}
}

// TestAnApprovalThatDidNotTakeEffectComesBack is the third case, and the reason the
// window still exists at all: if the key did not land, the dialog is still there and
// the user must be able to try again rather than stare at a permanently suppressed
// request.
func TestAnApprovalThatDidNotTakeEffectComesBack(t *testing.T) {
	q := New(nil, Options{SuppressAfterHandled: 10 * time.Second})
	now := time.Now()
	created := q.Observe(obs("wA:p1", herdr.StatusBlocked, "live_strong_blocker", now)).Created
	q.MarkHandledLocally(created.ID, now)

	agent := "codex"
	panes := []herdr.Pane{{
		PaneID: "wA:p1", WorkspaceID: "wA", Agent: &agent, AgentStatus: herdr.StatusBlocked,
	}}
	if early := q.Reconcile(panes, "local", nil, now.Add(5*time.Second)); len(early.Created) != 0 {
		t.Error("the card came back while the agent may still have been reacting")
	}
	if late := q.Reconcile(panes, "local", nil, now.Add(30*time.Second)); len(late.Created) != 1 {
		t.Errorf("the request never came back, so a user whose key did not land is stuck: %+v", late)
	}
}

// TestSuppressionExpiresSoALaterBlockIsNormal.
func TestSuppressionExpiresSoALaterBlockIsNormal(t *testing.T) {
	q := New(nil, Options{SuppressAfterHandled: 10 * time.Second})
	now := time.Now()
	created := q.Observe(obs("wA:p1", herdr.StatusBlocked, "live_strong_blocker", now)).Created
	q.MarkHandledLocally(created.ID, now)

	later := q.Observe(obsFor("claude", "wA:p1", herdr.StatusBlocked, "bash_permission_prompt", now.Add(30*time.Second)))
	if later.Created == nil {
		t.Fatal("a block after the suppression window is a genuine new request")
	}
}

// TestValueStatsUseTheStatedBaseline covers FR-013's requirement that the
// number is attributable rather than a black box.
func TestValueStatsUseTheStatedBaseline(t *testing.T) {
	q := New(nil, Options{})
	now := time.Now()
	q.RecordValue(ValueRecord{DecisionID: "a", DecidedAt: now, Kind: actions.KindApprove, OnDevice: true})
	q.RecordValue(ValueRecord{DecisionID: "b", DecidedAt: now.Add(-8 * 24 * time.Hour), Kind: actions.KindApprove, OnDevice: true})

	stats := q.ValueStatsSince(now.Add(-7 * 24 * time.Hour))
	if stats.Decisions != 1 || stats.OnDevice != 1 {
		t.Fatalf("weekly stats = %+v", stats)
	}
	if stats.SecondsSaved != BaselineSeconds {
		t.Errorf("seconds saved = %d, want the stated baseline %d", stats.SecondsSaved, BaselineSeconds)
	}
	if stats.BaselineSeconds != BaselineSeconds {
		t.Errorf("the baseline must be reported to the user: %+v", stats)
	}
}
