package core

import (
	"context"
	"errors"
	"time"

	"github.com/th2zz/perch/bridge/internal/discover"
	"github.com/th2zz/perch/bridge/internal/herdr"
	"github.com/th2zz/perch/bridge/internal/records"
)

type ConversationView struct {
	// Readable says Perch has this session's conversation to show, which is the one question the
	// app has to answer before it draws anything: yes, draw the conversation; no, draw the screen
	// instead (that is what the reading area did before this existed, and it is a better answer
	// than an empty one).
	//
	// It is false for two different situations that the user needs the same thing from: an agent
	// nobody has written an adapter for, and an agent whose record could not be found — a session
	// that has only just started, or a pane running something else since the bridge last looked.
	// Empty is then a *third* thing, and a different one: readable, with nothing in it, is a
	// session where nobody has typed yet, and the app says so in its own words.
	Readable bool `json:"readable"`
	// Messages is the conversation, oldest first. Never null, so the app's decoder has one shape
	// to read rather than two.
	Messages []records.Message `json:"messages"`
	// Working is true while a turn is in flight, and WorkingSecs is how long it has been going.
	//
	// Seconds rather than a timestamp because the clock on the host and the clock on the phone
	// are two different clocks: the phone shows a duration, and a duration computed from two
	// machines' idea of "now" is a number that can come out negative.
	Working     bool `json:"working"`
	WorkingSecs int  `json:"working_secs"`
	// Truncated says the conversation starts part-way through: the record is longer than the app is
	// sent, and the beginning of it is on the user's computer. Without it a reader who scrolls to the
	// top has no way to tell "this is where the conversation begins" from "this is where Perch
	// stopped looking".
	Truncated bool `json:"truncated"`
}

// Conversation reads the conversation of one session from its harness's records.
//
// The same shape as [Core.Usage], and for the same reason: both answers come from files on the
// host, and both are per-request rather than per-poll-on-everything. Unlike usage, this one *is*
// polled — the reading face follows an answer as it is written — which is why the codex adapter
// tails the record incrementally instead of parsing it again (see records.codexConversation).
func (c *Core) Conversation(ctx context.Context, hostID, paneID string) (ConversationView, error) {
	if paneID == "" {
		return ConversationView{}, errors.New("which session? a pane id is required")
	}
	if hostID != "" && hostID != "local" {
		// Another machine's records are its own, and the chain that finds them lives there.
		// Reporting "not readable" is the honest answer, and it sends the app to its screen-based
		// fallback rather than to an empty conversation.
		return ConversationView{Messages: []records.Message{}, Readable: false}, nil
	}
	kind, where, err := c.sessionWhere(ctx, paneID)
	if err != nil {
		return ConversationView{}, err
	}
	transcript, readable := records.Conversation(kind, homeDir(), where)
	if !readable && kind == "codex" {
		if sessionID, ok := c.sessions.lookup(paneID, where.PID, where.Title); ok {
			where.PreferredSessionID = sessionID
			transcript, readable = records.Conversation(kind, homeDir(), where)
		}
	}
	if readable && kind == "codex" && transcript.SessionID != "" {
		c.sessions.save(paneID, transcript.SessionID, where.PID, where.Title, where.CWD)
	}
	view := ConversationView{
		Readable:  readable,
		Messages:  transcript.Messages,
		Truncated: transcript.Truncated,
	}
	if view.Messages == nil {
		view.Messages = []records.Message{}
	}
	if !transcript.WorkingSince.IsZero() {
		view.Working = true
		if elapsed := int(time.Since(transcript.WorkingSince).Seconds()); elapsed > 0 {
			view.WorkingSecs = elapsed
		}
	}
	return view, nil
}

// sessionWhere finds which session a pane is running: the harness that owns it, and the facts about
// its process that let an adapter find the harness's own records.
//
// Shared by the two record readers, [Core.Usage] and [Core.Conversation], so that "which process is
// this session" is answered in exactly one place. Those two panels describe the same session at the
// same moment, and the day they disagree about which one it is, the user is looking at a model name
// from one session next to an answer from another.
//
// The identity is asked for in the order it is most exact: herdr's own registration of the session,
// which the harness states about itself (`agent_session`, kind "id", present from the moment the
// agent starts); then the process, whose command line names the session for harnesses that do
// (`codex resume <session-id>`) and whose pid finds the record file for the rest. A pane's own cwd
// is the fallback rather than the first choice, because a machine here has eight codex sessions open
// at once, several of them in the same directory.
func (c *Core) sessionWhere(ctx context.Context, paneID string) (string, records.Where, error) {
	base := records.Where{PaneID: paneID}

	// Which agent is in this pane comes from the inventory the bridge already keeps, because a call
	// to herdr costs about 101 ms on a 100 ms tick (measured: see the herdr package's live probe) and
	// this runs on every poll of an open screen. The inventory is kept by the event subscription,
	// with a five-minute backstop and a refresh on every snapshot the app asks for.
	//
	// A pane the inventory does not have — created since the last reconcile, or a bridge that has
	// only just started — is asked about directly, which is the case this can afford to be slow.
	var kind string
	known := false
	if m := c.Mirror(); m != nil {
		for _, placed := range m.Panes {
			if placed.PaneID == paneID && placed.HostID == discover.LocalHostID {
				kind, known = herdr.Str(placed.Agent), true
				base.SessionID = placed.AgentSession.ID()
				base.Title = firstNonEmpty(
					herdr.Str(placed.TerminalTitleStripped), herdr.Str(placed.TerminalTitle),
				)
				break
			}
		}
	}
	if !known {
		pane, err := c.api.GetPane(ctx, paneID)
		if err != nil {
			return "", records.Where{}, err
		}
		kind = herdr.Str(pane.Agent)
		base.SessionID = pane.AgentSession.ID()
		base.Title = firstNonEmpty(
			herdr.Str(pane.TerminalTitleStripped), herdr.Str(pane.TerminalTitle),
		)
		base.CWD = firstNonEmpty(herdr.Str(pane.ForegroundCwd), herdr.Str(pane.Cwd))
	}

	// The first foreground process, and deliberately without asking the record readers whether they
	// can read it.
	//
	// They used to be asked, per process, per poll — and that was measured here at about 110 ms on
	// every conversation refresh, because `records.Read` for codex answers by parsing 512 KB of a
	// record's head and the end of a 56 MB file. What the probe actually decided was "is this
	// harness one with an adapter?", which is a property of the harness's *name*, not of the
	// process; and for both harnesses that have an adapter the answer is yes on the first process,
	// so the first process is what was being used anyway.
	//
	// `foreground_processes` is the pane's foreground chain and the agent is its head. That is
	// checked rather than assumed: it is the same choice [Core.Usage] has always made, and its
	// numbers on a live pane are right.
	if procs, err := c.api.PaneProcesses(ctx, paneID); err == nil && len(procs) > 0 {
		base.Argv = procs[0].Argv
		base.PID = procs[0].PID
		if procs[0].Cwd != "" {
			base.CWD = procs[0].Cwd
		}
	}
	return kind, base, nil
}
