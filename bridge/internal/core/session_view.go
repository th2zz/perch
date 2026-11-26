package core

import (
	"context"
	"fmt"
	"time"

	"github.com/th2zz/perch/bridge/internal/actions"
	"github.com/th2zz/perch/bridge/internal/discover"
	"github.com/th2zz/perch/bridge/internal/herdr"
)

// The two sentences the escape hatch shows where a key row would be. Named rather
// than inlined because they are displayed to the user: they are copy, and copy is
// testable in a way that a string buried inside a function is not.
const (
	// SessionNoAgent is for a session with no agent in it.
	SessionNoAgent = "Perch does not see an agent in this session. You can read it and send what you type."
	// SessionNotWaiting is for a session that is not blocked on a person.
	SessionNotWaiting = "This session is not waiting on you."
)

// SessionView is one session as the phone needs to see it to help.
//
// This is the escape hatch's read half. It exists because the product's honesty
// rule has a cost: when a dialog cannot be driven deterministically, the bridge
// refuses — and a refusal with nothing behind it strands the user away from their
// desk, which is the one situation the product exists for. The escape hatch is
// the contract's designated answer to that (contracts/herdr-usage.md §2), and it
// starts with letting the user *see* what is on screen.
//
// Everything here is read-only except the three capability flags, which say what
// the user may then do — and those come from the same key table the decision
// cards use, so the escape hatch cannot offer something a card would refuse.
type SessionView struct {
	// HostID says which computer this session is on ("local" for this one), and
	// HostLabel is what to call it on screen. Both travel with the view because a
	// pane id alone does not identify a session once there is more than one
	// machine (spec FR-016).
	HostID      string            `json:"host_id"`
	HostLabel   string            `json:"host_label"`
	PaneID      string            `json:"pane_id"`
	WorkspaceID string            `json:"workspace_id"`
	TabID       string            `json:"tab_id"`
	Workspace   string            `json:"workspace_label"`
	AgentKind   string            `json:"agent"`
	AgentName   string            `json:"display_agent"`
	AgentStatus herdr.AgentStatus `json:"agent_status"`
	Title       string            `json:"title"`

	// Text is the recent output, with ANSI stripped: the escape hatch renders
	// what the terminal showed, not a re-interpretation of it.
	Text      string    `json:"text"`
	Source    string    `json:"source"`
	Lines     int       `json:"lines"`
	Revision  uint64    `json:"revision"`
	Truncated bool      `json:"truncated"`
	ReadAt    time.Time `json:"read_at"`

	// Rule is the detection rule that matched, when there is one.
	Rule string `json:"rule,omitempty"`
	// DecisionID points at the queue entry this session is waiting on, if any.
	DecisionID string `json:"decision_id,omitempty"`

	Unsent string `json:"unsent,omitempty"`

	// UnsentCommand is true when the text waiting in the input line is a slash command, so the app can say
	// what sending would really do (see `composer.Composer.Command`).
	UnsentCommand bool `json:"unsent_command,omitempty"`

	// Menu is true when the bottom of this session's screen is a menu rather than an
	// input line — codex's model picker, a slash-command list. Typing into one drives it
	// (measured), and herdr reports such panes as `idle`, so this is the only thing in the
	// system that knows. The app refuses to send while it is true and says why; the keys
	// are still there for the person to press.
	Menu bool `json:"menu,omitempty"`

	Status string `json:"status,omitempty"`

	// ChromeLines is how many trailing lines of Text are the harness's own furniture —
	// its input box, the rules around it, the footer. The conversation view drops them;
	// the terminal view keeps everything, because that view's promise is that it is the
	// screen.
	ChromeLines int `json:"chrome_lines,omitempty"`

	// Capabilities, from the same table the cards use.
	Approvable bool `json:"approvable"`
	Rejectable bool `json:"rejectable"`
	Answerable bool `json:"answerable"`
	// CanDeliverText says whether the session is ready for a sentence. It is false
	// while a harness is still in its own first-run setup.
	CanDeliverText bool   `json:"can_deliver_text"`
	SetupRequired  bool   `json:"setup_required"`
	SetupReason    string `json:"setup_reason"`
	SetupFix       string `json:"setup_fix"`
	// Unavailable explains, in the user's terms, why nothing can be pressed.
	Unavailable string `json:"unavailable,omitempty"`
}

// Session reads one session and works out what may be done with it.
//
// The host is part of the address (spec FR-016): pane ids are unique within one
// machine and nowhere else, so "session w1:p1" without a machine would be
// ambiguous the moment a user has two computers.
func (c *Core) Session(ctx context.Context, hostID, paneID string, lines int) (*SessionView, error) {
	if paneID == "" {
		return nil, fmt.Errorf("which session? a pane id is required")
	}
	lines = readDepth(lines)
	machine, err := c.hostMachine(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if machine != "" {
		return c.sessionAt(ctx, machine, paneID, lines)
	}

	pane, err := c.api.GetPane(ctx, paneID)
	if err != nil {
		return nil, fmt.Errorf("cannot read session %s: %w", paneID, err)
	}

	view := &SessionView{
		HostID:      discover.LocalHostID,
		HostLabel:   c.opts.HostLabel,
		PaneID:      pane.PaneID,
		WorkspaceID: pane.WorkspaceID,
		TabID:       pane.TabID,
		AgentKind:   herdr.Str(pane.Agent),
		AgentName:   herdr.Str(pane.DisplayAgent),
		AgentStatus: pane.AgentStatus,
		Title:       firstNonEmpty(herdr.Str(pane.Title), herdr.Str(pane.TerminalTitleStripped)),
		Revision:    pane.Revision,
		Source:      "recent",
		Lines:       lines,
		ReadAt:      time.Now(),
		// Writing what the user typed is always available; it is the fourth write
		// class and does not depend on what is on screen.
		CanDeliverText: true,
	}

	c.mu.RLock()
	view.Workspace = c.labels[pane.WorkspaceID]
	c.mu.RUnlock()

	read, err := c.api.ReadPane(ctx, paneID, "recent", lines)
	if err != nil {
		// A pane that is blocked or busy may refuse a long read; the visible
		// screen is still worth having, so try the cheaper source before giving up.
		if fallback, fallbackErr := c.api.ReadPane(ctx, paneID, "visible", 0); fallbackErr == nil {
			read = fallback
		} else {
			return nil, fmt.Errorf("cannot read session %s: %w", paneID, err)
		}
	}
	view.Text = read.Text
	view.Source = read.Source
	view.Truncated = read.Truncated
	if read.Revision != 0 {
		view.Revision = read.Revision
	}
	if reason, fix, required := setupRequiredOnScreen(view.AgentKind, view.Text); required {
		view.SetupRequired = true
		view.SetupReason = reason
		view.SetupFix = fix
		view.CanDeliverText = false
		view.Unavailable = reason
	}

	view.Rule = c.ruleFor(ctx, paneID, view.AgentKind)
	c.applyCapabilities(view)
	if !view.SetupRequired {
		composer := c.readComposer(ctx, paneID, view.AgentKind, view.AgentStatus)
		view.Unsent = composer.Unsent
		view.UnsentCommand = composer.Command
		view.Menu = composer.Menu
		view.Status = composer.Status
		view.ChromeLines = composer.ChromeLines
	} else {
		view.Approvable = false
		view.Rejectable = false
		view.Answerable = false
	}

	// Tie the view to a queue entry when there is one, so the app can act on the
	// request rather than re-deriving it.
	for _, decision := range c.q.List(false) {
		if decision.PaneID == paneID {
			view.DecisionID = decision.ID
			break
		}
	}
	return view, nil
}

// readDepth decides how much of a pane the caller gets: what it asked for, bounded.
//
// It used to read `if lines <= 0 || lines > 400 { lines = 160 }`, and that one line was a bug with a
// shell of a sane-looking clamp: **asking for more returned less**. The app asked for 400 when it
// opened a session and 160 on every poll, so the clamp never fired for it — but anything that asked
// for 500 (a person debugging with curl, a future screen wanting the whole scrollback) got 160, the
// smallest answer in the building, with no error and no way to tell.
//
// So: ask for nothing and you get a default; ask for a sane number and you get it; ask for something
// absurd and you get the ceiling, not the floor. The ceiling is a budget for this process rather than
// a statement about the pane — herdr answers with its own scrollback and nothing beyond it (measured
// 2026-10-15: 411, 688 and 670 lines on three live panes, the same numbers whether the request was
// 1000 or 5000).
func readDepth(requested int) int {
	const (
		defaultLines = 160
		maxLines     = 2000
	)
	if requested <= 0 {
		return defaultLines
	}
	if requested > maxLines {
		return maxLines
	}
	return requested
}

func (c *Core) readComposer(ctx context.Context, paneID, kind string, status herdr.AgentStatus) Composer {
	if _, known := composers[kind]; !known {
		return Composer{}
	}
	read, err := c.api.ReadPaneStyled(ctx, paneID, "visible", composerRows)
	if err != nil {
		c.log.Debug("could not read the input line", "pane", paneID, "err", err)
		return Composer{}
	}
	return ReadComposer(kind, status, read.Text)
}

// sessionAt reads a session on another machine.
//
// It is deliberately the same shape as the local read, built from the same
// records, so the escape hatch and the decision path cannot tell the difference —
// only the transport differs (`herdr --machine …`, see herdr/remote.go).
//
// One honest difference: the remote read returns text, not the metadata the local
// socket read returns, so `truncated` is always false there. Saying "we did not
// fetch older output" when we do not know would be a worse lie than saying nothing.
func (c *Core) sessionAt(ctx context.Context, machine, paneID string, lines int) (*SessionView, error) {
	pane, ok := c.remotePane(machine, paneID)
	if !ok {
		// Not in the last inventory. It may be brand new or the poll may be behind,
		// so this is stated as what it is rather than as "no such session".
		return nil, fmt.Errorf("%s has not reported a session called %s; refresh and try again", machine, paneID)
	}

	view := &SessionView{
		HostID:         machine,
		PaneID:         pane.PaneID,
		WorkspaceID:    pane.WorkspaceID,
		TabID:          pane.TabID,
		AgentKind:      herdr.Str(pane.Agent),
		AgentName:      herdr.Str(pane.DisplayAgent),
		AgentStatus:    pane.AgentStatus,
		Title:          firstNonEmpty(herdr.Str(pane.Title), herdr.Str(pane.TerminalTitleStripped)),
		Revision:       pane.Revision,
		Source:         "recent",
		Lines:          lines,
		ReadAt:         time.Now(),
		CanDeliverText: true,
	}
	view.HostLabel = machine

	text, err := c.cli.ReadPaneTextAt(ctx, machine, paneID, lines)
	if err != nil {
		// A pane that is mid-redraw can refuse a read; an empty view with a reason
		// is still better than no screen at all.
		view.Text = ""
	} else {
		view.Text = text
	}
	if reason, fix, required := setupRequiredOnScreen(view.AgentKind, view.Text); required {
		view.SetupRequired = true
		view.SetupReason = reason
		view.SetupFix = fix
		view.CanDeliverText = false
		view.Unavailable = reason
	}

	rule := ""
	if det, err := c.cli.ExplainAt(ctx, machine, paneID); err == nil {
		rule = det.Rule
	}
	view.Rule = rule
	if view.SetupRequired {
		view.Approvable = false
		view.Rejectable = false
		view.Answerable = false
	} else {
		c.applyCapabilities(view)
	}

	for _, decision := range c.q.List(false) {
		if decision.PaneID == paneID && decision.HostID == machine {
			view.DecisionID = decision.ID
			break
		}
	}
	return view, nil
}

// ruleFor asks herdr which detection rule matched. A failure is not fatal: the
// session is still readable, there are simply no capabilities.
func (c *Core) ruleFor(ctx context.Context, paneID, agentKind string) string {
	if agentKind == "" {
		return ""
	}
	explainCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	detection, err := c.cli.Explain(explainCtx, paneID)
	if err != nil {
		return ""
	}
	return detection.Rule
}

func (c *Core) applyCapabilities(view *SessionView) {
	if view.SetupRequired {
		return
	}
	if view.AgentKind == "" {
		view.Unavailable = SessionNoAgent
		return
	}
	if view.AgentStatus != herdr.StatusBlocked {
		view.Unavailable = SessionNotWaiting
		return
	}

	keys, reason, _ := actions.LookupKeysForPrompt(view.AgentKind, view.Rule, view.Text)
	view.Approvable = keys.CanPress() && len(keys.Approve) > 0
	view.Rejectable = keys.CanPress() && len(keys.Reject) > 0
	view.Answerable = keys.AcceptsTypedAnswer()

	if !view.Approvable && !view.Rejectable && !view.Answerable {
		if reason == "" {
			reason = "nothing about this dialog says which keys it accepts"
		}
		view.Unavailable = reason
	} else if !view.Approvable && reason != "" {
		view.Unavailable = reason
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
