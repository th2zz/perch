package queue

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
	"github.com/th2zz/perch/bridge/internal/state"
)

func blockedPane(paneID string) herdr.Pane {
	return herdr.Pane{PaneID: paneID, WorkspaceID: "w1", TabID: "w1:t1", AgentStatus: herdr.StatusBlocked}
}

// newTestQueue is a queue with no persistence: these tests are about identity,
// not about surviving a restart.
func newTestQueue(t *testing.T) *Queue {
	t.Helper()
	return New(nil, Options{})
}

func TestTwoComputersCanHaveTheSamePaneID(t *testing.T) {
	q := newTestQueue(t)

	local := q.Reconcile([]herdr.Pane{blockedPane("w1:p1")}, "local", nil, time.Now())
	if len(local.Created) != 1 {
		t.Fatalf("local created %d decisions, want 1", len(local.Created))
	}
	remote := q.Reconcile([]herdr.Pane{blockedPane("w1:p1")}, "workbox", nil, time.Now())
	if len(remote.Created) != 1 {
		t.Fatalf("the other computer's identical pane id was treated as the same request")
	}

	pending := q.List(false)
	if len(pending) != 2 {
		t.Fatalf("pending = %d, want 2 (one per computer): %+v", len(pending), pending)
	}
	hosts := map[string]string{}
	for _, d := range pending {
		hosts[d.HostID] = d.PaneID
	}
	if hosts["local"] != "w1:p1" || hosts["workbox"] != "w1:p1" {
		t.Errorf("hosts = %v", hosts)
	}
	if pending[0].ID == pending[1].ID {
		t.Errorf("two machines produced the same decision id: %s", pending[0].ID)
	}

	// And acting on one must not touch the other.
	if _, ok := q.MarkHandledLocally(remote.Created[0].ID, time.Now()); !ok {
		t.Fatal("could not mark the remote decision handled")
	}
	still := q.List(false)
	if len(still) != 1 || still[0].HostID != "local" {
		t.Errorf("resolving one computer's request changed the other's: %+v", still)
	}
}

// TestOneComputersInventoryCannotCloseAnothersCard: a reconcile pass is a
// statement about *one* machine. The local inventory knows nothing about a pane on
// another computer, so treating a missing pane as "that session ended" would close
// the user's remote cards every time the local session changed — which is every
// few seconds.
func TestOneComputersInventoryCannotCloseAnothersCard(t *testing.T) {
	q := newTestQueue(t)

	remote := q.Reconcile([]herdr.Pane{blockedPane("w1:p1")}, "workbox", nil, time.Now())
	if len(remote.Created) != 1 {
		t.Fatalf("remote created %d decisions, want 1", len(remote.Created))
	}

	// A local pass that sees nothing at all: no panes, so from the local machine's
	// point of view that session is gone.
	local := q.Reconcile(nil, "local", nil, time.Now())
	if len(local.Resolved) != 0 {
		t.Fatalf("a local reconcile closed %d remote decisions", len(local.Resolved))
	}
	if len(q.List(false)) != 1 {
		t.Fatalf("the other computer's card disappeared: %+v", q.List(false))
	}

	// The same pass *on the right host* does close it, which is the half that must
	// keep working: a request answered on the computer has to leave the phone.
	remoteLater := q.Reconcile([]herdr.Pane{{
		PaneID: "w1:p1", WorkspaceID: "w1", AgentStatus: herdr.StatusIdle,
	}}, "workbox", nil, time.Now())
	if len(remoteLater.Resolved) != 1 {
		t.Fatalf("the other computer's own pass resolved %d decisions, want 1", len(remoteLater.Resolved))
	}
	if len(q.List(false)) != 0 {
		t.Errorf("the request is still on the phone: %+v", q.List(false))
	}
}

// TestTwoTapsRunTheCommandOnce is the safety property behind every button.
//
// The notification action and the card in the app are two entry points for the same
// request, and a phone can deliver one tap to both — or the user can simply tap
// twice. Without a claim, both callers see `pending`, both run the herdr command,
// and an agent's dialog receives Enter twice: the second one confirms whatever is
// highlighted after the first, which is not what anybody asked for.
func TestTwoTapsRunTheCommandOnce(t *testing.T) {
	q := newTestQueue(t)
	created := q.Reconcile([]herdr.Pane{blockedPane("w1:p1")}, "local", nil, time.Now())
	if len(created.Created) != 1 {
		t.Fatalf("no decision to act on: %+v", created.Created)
	}
	id := created.Created[0].ID

	first, err := q.Claim(id, time.Now())
	if err != nil {
		t.Fatalf("the first tap must win the claim: %v", err)
	}
	if first.ID != id {
		t.Errorf("claimed the wrong decision: %s", first.ID)
	}

	if _, err := q.Claim(id, time.Now()); err == nil {
		t.Fatal("a second tap claimed the same request, so its command would run twice")
	} else if !errors.Is(err, ErrInFlight) {
		t.Errorf("the second tap was refused for the wrong reason: %v", err)
	}

	// A failed attempt gives the claim back: the user's tap must not be spent by a
	// laptop that was asleep.
	q.Release(id)
	if _, err := q.Claim(id, time.Now()); err != nil {
		t.Fatalf("a released claim must be claimable again: %v", err)
	}

	// Once the request is settled, no claim can resurrect it.
	q.MarkHandledLocally(id, time.Now())
	if _, err := q.Claim(id, time.Now()); err == nil {
		t.Fatal("a settled request was claimed again")
	} else if !errors.Is(err, ErrNotPending) {
		t.Errorf("wrong refusal for a settled request: %v", err)
	}
}

func TestAClaimFromADeadAttemptEventuallyExpires(t *testing.T) {
	// A bridge killed mid-command must not leave a card nobody can ever act on.
	q := newTestQueue(t)
	created := q.Reconcile([]herdr.Pane{blockedPane("w1:p1")}, "local", nil, time.Now())
	id := created.Created[0].ID

	start := time.Now()
	if _, err := q.Claim(id, start); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if _, err := q.Claim(id, start.Add(time.Second)); err == nil {
		t.Fatal("a fresh claim was treated as stale")
	}
	if _, err := q.Claim(id, start.Add(claimTTL+time.Second)); err != nil {
		t.Errorf("a claim older than the TTL must not block the user: %v", err)
	}
}

func TestClaimingSomethingUnknownIsRefused(t *testing.T) {
	q := newTestQueue(t)
	if _, err := q.Claim("never-existed", time.Now()); !errors.Is(err, ErrUnknownDecision) {
		t.Errorf("claiming an unknown request gave %v", err)
	}
}

// TestACorruptQueueCacheSelfHealsFromTheNextSnapshot.
//
// The queue's file is a cache, not a source of truth (constitution I), and the way it
// is treated has to match that claim: a truncated or hand-edited file must not stop the
// bridge, and the requests it held must come back from the computer rather than being
// lost. "Come back" is the part worth testing — it is the difference between a cache
// that failed and a queue that dropped somebody's pending approvals.
func TestACorruptQueueCacheSelfHealsFromTheNextSnapshot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(state.EnvStateDir, dir)
	st, err := state.Open()
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	if err := os.WriteFile(st.Path(state.FileDecisions), []byte(`{"not":"a list"`), 0o600); err != nil {
		t.Fatalf("write corrupt cache: %v", err)
	}

	// Loading a corrupt cache is not an error: the queue starts empty.
	q := New(st, Options{})
	if got := q.List(false); len(got) != 0 {
		t.Fatalf("a corrupt cache produced %d decisions: %+v", len(got), got)
	}

	// And the next authoritative read brings the waiting agent back, with a fresh id.
	reconciled := q.Reconcile([]herdr.Pane{blockedPane("w1:p1")}, "local", nil, time.Now())
	if len(reconciled.Created) != 1 {
		t.Fatalf("the request did not come back: %+v", reconciled)
	}
	if pending := q.List(false); len(pending) != 1 || pending[0].PaneID != "w1:p1" {
		t.Errorf("pending = %+v, want the one waiting agent", pending)
	}
}
