package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/th2zz/perch/bridge/internal/discover"
	"github.com/th2zz/perch/bridge/internal/herdr"
	"github.com/th2zz/perch/bridge/internal/queue"
)


const (
	// remotePollInterval is how often other machines are re-read. It is slow on
	// purpose: every poll spawns a process and an SSH connection on a machine that
	// is also running the user's agents.
	remotePollInterval = 20 * time.Second
	// remoteCallTimeout bounds one host read. Longer than the local paths because
	// it includes an SSH connection; short enough that a dead host cannot stall
	// the poller for long.
	remoteCallTimeout = 15 * time.Second
	// remoteMaxBackoff caps the retry delay after repeated failures.
	remoteMaxBackoff = 5 * time.Minute
)

// remoteHost is one other computer, as far as we last managed to read it.
type remoteHost struct {
	machine herdr.Machine
	snap    *herdr.Snapshot
	at      time.Time

	// reason and fix describe the last failure in the user's terms (FR-015).
	reason   string
	fix      string
	failures int
	nextTry  time.Time
}

// machineList returns the user's saved machines, asking herdr at most once per
// TTL.
//
// A failed lookup keeps the previous answer rather than returning nothing: an
// intermittent CLI failure should not make the user's other computers disappear
// from a screen that is only informing them of what exists.
func (c *Core) machineList(ctx context.Context) []herdr.Machine {
	c.machinesMu.Lock()
	defer c.machinesMu.Unlock()
	if !c.machinesAt.IsZero() && time.Since(c.machinesAt) < c.machinesTTL {
		return c.machines
	}
	machines, err := c.cli.Machines(ctx)
	c.machinesAt = time.Now()
	if err != nil {
		c.log.Debug("could not list saved herdr machines; keeping the last answer", "err", err)
		return c.machines
	}
	c.machines = machines
	return c.machines
}

// registeredHosts returns the machines the bridge will read, in the order herdr
// lists them.
//
// Disabled machines are skipped: herdr's own enable/disable switch is the user's
// statement about whether to use that computer, and honouring it costs nothing.
func (c *Core) registeredHosts(ctx context.Context) []herdr.Machine {
	var out []herdr.Machine
	for _, m := range c.machineList(ctx) {
		if m.Label == "" {
			continue
		}
		out = append(out, m)
	}
	return out
}

// placed returns the local machine plus every remote host that has been read,
// ready for discover.Build.
//
// Remote hosts that have never been read still appear — as a row saying "not read
// yet" — because the user's saved machines are part of the truth about their
// setup, and hiding them would make the app disagree with herdr's own sidebar.
func (c *Core) placed(ctx context.Context, local *herdr.Snapshot, localAt time.Time) []discover.Placed {
	localHost := discover.Host{
		HostID:  discover.LocalHostID,
		Label:   c.opts.HostLabel,
		Kind:    "local",
		Status:  discover.StatusReachable,
		Enabled: true,
	}
	out := []discover.Placed{{Host: localHost, Snap: local, TakenAt: localAt}}

	c.remoteMu.Lock()
	defer c.remoteMu.Unlock()
	for _, machine := range c.registeredHosts(ctx) {
		host := discover.Host{
			HostID:  machine.Label,
			Label:   machine.Label,
			Kind:    "ssh",
			Enabled: machine.Enabled,
		}
		rh := c.remote[machine.Label]
		if rh == nil || rh.snap == nil {
			host.Status = "unknown"
		} else {
			host.Status = discover.StatusReachable
		}
		if rh != nil && rh.reason != "" {
			host.Status = discover.StatusUnreachable
			host.Reason = rh.reason
			host.Fix = rh.fix
		}
		if !machine.Enabled {
			host.Status = "disabled"
		}
		var snap *herdr.Snapshot
		var at time.Time
		if rh != nil {
			snap, at = rh.snap, rh.at
		}
		out = append(out, discover.Placed{Host: host, Snap: snap, TakenAt: at})
	}
	return out
}

// hostMachine resolves the host an action names.
//
// An unknown host is refused rather than silently treated as local: acting on the
// wrong computer is the one mistake this feature must not make.
func (c *Core) hostMachine(ctx context.Context, hostID string) (string, error) {
	id := strings.TrimSpace(hostID)
	if id == "" || id == discover.LocalHostID {
		return "", nil
	}
	for _, m := range c.machineList(ctx) {
		if m.Label != id {
			continue
		}
		if !m.Enabled {
			return "", fmt.Errorf("%w: %s is turned off in herdr", ErrHostUnavailable, id)
		}
		return m.Label, nil
	}
	return "", fmt.Errorf("%w: no saved machine is called %s", ErrHostUnavailable, id)
}

// ErrHostUnavailable means the host an action names cannot be used: it is not a
// saved machine, or the user turned it off in herdr.
//
// It is a sentinel so the HTTP layer can answer 400 with a next step rather than
// 500 "something went wrong" — the phone naming a machine that this computer does
// not have is a mismatch between two views of the setup, not a bridge failure.
var ErrHostUnavailable = errors.New("this computer is not available")

// RefreshRemoteHosts reads every due host once. A manual call ignores the retry
// backoff: a person asking "is it reachable now?" deserves a fresh attempt.
func (c *Core) RefreshRemoteHosts(ctx context.Context) {
	c.pollRemoteHosts(ctx, true)
}

// runRemotePoller polls other machines until ctx is done.
func (c *Core) runRemotePoller(ctx context.Context) {
	ticker := time.NewTicker(remotePollInterval)
	defer ticker.Stop()
	c.pollRemoteHosts(ctx, true)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pollRemoteHosts(ctx, false)
		}
	}
}

// pollRemoteHosts reads each registered host that is due.
//
// Sequential on purpose: N machines means N SSH connections and N remote API
// bridges, and firing them at once would put a burst of load on the user's
// machines to save a few seconds nobody is waiting for.
func (c *Core) pollRemoteHosts(ctx context.Context, force bool) {
	for _, machine := range c.registeredHosts(ctx) {
		if !machine.Enabled {
			continue
		}
		if !force && !c.dueForPoll(machine.Label) {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		c.readRemoteHost(ctx, machine)
	}
}

func (c *Core) dueForPoll(label string) bool {
	c.remoteMu.Lock()
	defer c.remoteMu.Unlock()
	rh := c.remote[label]
	if rh == nil || rh.nextTry.IsZero() {
		return true
	}
	return !time.Now().Before(rh.nextTry)
}

// readRemoteHost reads one machine and folds the result into the queue, the
// mirror and the host's own row.
func (c *Core) readRemoteHost(ctx context.Context, machine herdr.Machine) {
	callCtx, cancel := context.WithTimeout(ctx, remoteCallTimeout)
	defer cancel()

	snap, err := c.cli.RemoteSnapshot(callCtx, machine.Label)

	c.remoteMu.Lock()
	if c.remote == nil {
		c.remote = map[string]*remoteHost{}
	}
	rh := c.remote[machine.Label]
	if rh == nil {
		rh = &remoteHost{machine: machine}
		c.remote[machine.Label] = rh
	}
	rh.machine = machine
	now := time.Now()
	if err != nil {
		rh.failures++
		rh.reason, rh.fix = remoteFailure(machine.Label, err)
		rh.nextTry = now.Add(backoffFor(rh.failures))
		c.log.Warn("could not read another computer", "machine", machine.Label, "err", err)
		c.remoteMu.Unlock()
		return
	}
	firstRead := rh.snap == nil
	rh.snap = snap
	rh.at = now
	rh.reason, rh.fix = "", ""
	rh.failures = 0
	rh.nextTry = now.Add(remotePollInterval)
	c.remoteMu.Unlock()

	// The queue is fed from the host's own inventory, through the same code path
	// the local snapshot uses, so a blocked agent on another machine becomes a
	// card with the same lifecycle as a local one.
	labels := make(map[string]string, len(snap.Workspaces))
	for _, w := range snap.Workspaces {
		labels[w.WorkspaceID] = w.Label
	}
	facts := c.remoteFacts(ctx, machine.Label, snap.Panes)
	result := c.q.ReconcileInventory(queue.Inventory{
		Panes:  snap.Panes,
		HostID: machine.Label,
		Labels: labels,
		At:     now,
		Facts:  facts,
	})
	for _, d := range result.Created {
		created := d
		c.log.Info("decision request", "host", machine.Label, "pane", created.PaneID,
			"agent", created.AgentKind)
		c.publish(Event{Type: EventDecision, Decision: &created})
	}
	for _, d := range result.Resolved {
		resolved := d
		c.publish(Event{Type: EventResolved, Decision: &resolved})
	}
	if firstRead || len(result.Created) > 0 || len(result.Resolved) > 0 {
		c.publish(Event{Type: EventSnapshot})
	}
}

// remoteFacts asks a machine what its blocked dialogs are asking, before the cards
// that describe them are published.
//
// It runs only for panes the queue does not already know about: a request the user
// has already seen does not need re-describing, so a steady-state poll costs one
// call rather than three per blocked pane.
//
// The cost is real and deliberate: two extra remote calls (~7s) delay the *first*
// notification for that machine. The alternative is worse — a notification whose
// buttons say "we cannot tell what this dialog wants", followed by a second
// notification correcting it, is exactly the kind of small lie that costs trust.
func (c *Core) remoteFacts(
	ctx context.Context,
	machine string,
	panes []herdr.Pane,
) func(herdr.Pane) (string, string) {
	known := map[string]bool{}
	for _, d := range c.q.List(false) {
		if d.HostID == machine {
			known[d.PaneID] = true
		}
	}
	type fact struct{ rule, excerpt string }
	facts := map[string]fact{}
	for _, p := range panes {
		if !p.AgentStatus.NeedsDecision() || known[p.PaneID] {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, remoteCallTimeout)
		var f fact
		if det, err := c.cli.ExplainAt(callCtx, machine, p.PaneID); err == nil {
			f.rule = det.Rule
		}
		if text, err := c.cli.ReadPaneTextAt(callCtx, machine, p.PaneID, c.opts.SummaryLines); err == nil {
			f.excerpt = firstLines(text, c.opts.SummaryLines)
		}
		cancel()
		facts[p.PaneID] = f
	}
	return func(p herdr.Pane) (string, string) {
		f, ok := facts[p.PaneID]
		if !ok {
			return "", ""
		}
		return f.rule, f.excerpt
	}
}

// remoteFailure turns a failed remote read into the reason and the next step the
// user sees on that computer's row (spec FR-015).
//
// The underlying error is logged, not shown: "ssh exited 255" is not something a
// person can act on, and the actionable statement is the same in every case.
func remoteFailure(label string, err error) (string, string) {
	reason := fmt.Sprintf("Perch could not reach %s.", label)
	if strings.Contains(err.Error(), "unknown machine") || strings.Contains(err.Error(), "no saved") {
		reason = fmt.Sprintf("%s is no longer saved in herdr.", label)
	}
	return reason, "Check that the computer is on and reachable; Perch will try again on its own."
}

func backoffFor(failures int) time.Duration {
	d := remotePollInterval * time.Duration(1<<min(failures, 6))
	if d > remoteMaxBackoff {
		return remoteMaxBackoff
	}
	return d
}
