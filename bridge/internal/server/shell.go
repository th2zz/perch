package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/th2zz/perch/bridge/internal/shell"
)

func (s *Server) handleShell(w http.ResponseWriter, r *http.Request) {
	// A shell is a shell *of a machine*. Naming another computer here would silently run the commands on
	// this one, which is the kind of surprise that costs somebody an afternoon — so it is refused, in the
	// user's terms, with the fix in the message.
	if host := strings.TrimSpace(r.URL.Query().Get("host")); host != "" && host != "local" {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error": "a terminal on another computer is not supported yet",
			"fix":   "open the terminal on the computer running Perch, or use its own session screen",
		})
		return
	}

	ws, err := upgrade(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	defer func() { _ = ws.Close() }()

	fresh := r.URL.Query().Get("new") == "1"
	session, err := s.shells.Attach(fresh)
	if err != nil {
		s.opts.Logger.Warn("could not start a shell", "err", err)
		_ = ws.WriteClose(1011, "could not start a shell")
		return
	}

	attachment, err := session.Attach()
	if err != nil {
		_ = ws.WriteClose(1011, "the shell is not running")
		return
	}
	defer attachment.Detach()

	cols, rows := session.Size()
	// "shell", not "pane": this stream has nothing to do with a pane, and a reader of the log should not
	// have to work that out from a dash.
	s.opts.Logger.Info("terminal attached", "shell", true, "cols", cols, "rows", rows, "fresh", fresh)

	// The replay, before anything live: this is what makes a reconnect show the terminal instead of an
	// empty screen. Sent as a binary frame like every other terminal byte, so the client has one path.
	if replay := session.Snapshot(); len(replay) > 0 {
		if err := ws.WriteBinary(replay); err != nil {
			return
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for chunk := range attachment.Output() {
			if err := ws.WriteBinary(chunk); err != nil {
				return
			}
		}
		// The stream ended: either the shell exited, or this client fell too far behind to keep up. Both
		// are the client's cue to come back — the app reconnects, and the replay puts it right — but only
		// one of them deserves the word "ended", and [shell.Session.Exited] is the flag that is already
		// true when this goroutine wakes up. (Asking `Done()` here instead was a race: it closes *after*
		// the streams do, so a live check saw the socket shut with no explanation. Measured on 2026-10-15
		// against the running bridge — the checks in `scripts/verify-shell.sh` passed and this one flaked,
		// which is exactly why a verification is worth more than a green unit test.)
		if session.Exited() {
			if payload, err := json.Marshal(map[string]any{
				"type":   "ended",
				"reason": exitReason(session),
			}); err == nil {
				_ = ws.WriteText(payload)
			}
		}
		_ = ws.WriteClose(1000, "closed")
	}()

	for {
		opcode, payload, err := ws.ReadMessage()
		if err != nil {
			break
		}
		switch opcode {
		case opBinary:
			if _, err := session.Write(payload); err != nil {
				// The shell is gone; there is nothing left to type into.
				<-done
				return
			}
		case opText:
			restart, err := s.handleShellControl(session, payload)
			if err != nil {
				if werr := ws.WriteText([]byte(`{"error":"unreadable control message"}`)); werr != nil {
					<-done
					return
				}
				continue
			}
			if restart {
				// A new shell is a new stream: this socket has nothing left to carry, so it ends and the
				// app opens another (which attaches to the shell that now exists).
				_ = ws.WriteClose(1000, "restarted")
				<-done
				return
			}
		}
	}
	<-done
}

// handleShellControl is the text channel: the things about a terminal that are not its bytes.
//
// It returns true when the client asked for a fresh shell, which ends this socket.
func (s *Server) handleShellControl(session *shell.Session, payload []byte) (bool, error) {
	var command struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if err := json.Unmarshal(payload, &command); err != nil {
		return false, err
	}
	switch command.Type {
	case "resize":
		// Bounds only, no opinion: a phone that reports a shape no terminal could be is a client bug, and
		// the pane stream's lesson (a viewport reported before layout squashed somebody's codex to two
		// rows) is that a size is never taken on faith.
		if command.Cols < 20 || command.Rows < 5 || command.Cols > 1000 || command.Rows > 500 {
			return false, fmt.Errorf("refusing to resize a terminal to %dx%d", command.Cols, command.Rows)
		}
		return false, session.Resize(command.Cols, command.Rows)
	case "close":
		return false, session.Close()
	case "restart":
		if _, err := s.shells.Attach(true); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, fmt.Errorf("unknown control message %q", command.Type)
	}
}

// exitReason is one line about why a shell stopped, for the phone to show.
func exitReason(session *shell.Session) string {
	// A shell somebody ended is not a failure, and saying "signal: killed" about our own decision would
	// read like one: the phone shows this sentence to the person who tapped the button.
	if session.ClosedOnPurpose() {
		return "closed on request"
	}
	if err := session.ExitErr(); err != nil {
		return err.Error()
	}
	return "the shell exited"
}
