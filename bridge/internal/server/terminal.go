package server

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	paneID := strings.TrimSpace(r.URL.Query().Get("pane_id"))
	if paneID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "pane_id is required",
			"fix":   "open the session first, then open its terminal",
		})
		return
	}
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))

	ws, err := upgrade(w, r)
	if err != nil {
		return
	}
	defer func() { _ = ws.Close() }()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	stream, err := s.core.Terminal(ctx, r.URL.Query().Get("host"), paneID, cols, rows)
	if err != nil {
		s.opts.Logger.Info("terminal attach failed", "pane", paneID, "err", err)
		_ = ws.WriteClose(1011, "could not attach")
		return
	}
	defer stream.Close()

	// The terminal's bytes, out.
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := pumpTerminalOut(ws, stream); err != nil {
			// A terminal that ends early is worth a line: herdr's CLI explains a refusal on stderr,
			// and "the terminal went blank" with nothing in the log is the kind of report that costs
			// an afternoon.
			s.opts.Logger.Info("terminal stream ended", "pane", paneID, "err", err, "engine", stream.Diagnostics())
		}
		// The reader is finished, either because the terminal closed or because the phone went away.
		// Closing here is what unblocks the loop below.
		cancel()
		_ = ws.WriteClose(1000, "closed")
		_ = ws.Close()
	}()

	// The phone's bytes and control messages, in.
	//
	// Labelled because the two failures below have to leave the *loop*, not the switch — a bare
	// `break` there breaks the switch and kept reading keystrokes into a terminal that had already
	// stopped accepting them. staticcheck's SA4011 said so (2026-10-15). The outcome was usually the
	// same, because the reader goroutine cancels this context when the stream ends, but "usually the
	// same because of a race" is not a loop exit anybody can read.
readLoop:
	for {
		opcode, payload, err := ws.ReadMessage()
		if err != nil {
			break
		}
		switch opcode {
		case opBinary:
			if err := stream.WriteInput(payload); err != nil {
				s.opts.Logger.Info("terminal input failed", "pane", paneID, "err", err)
				_ = ws.WriteClose(1011, "terminal input failed")
				break readLoop
			}
		case opText:
			if err := handleTerminalControl(stream, payload); err != nil {
				// A control message we cannot read is the phone's bug, not a reason to drop a
				// terminal: say so and keep the session.
				if werr := ws.WriteText([]byte(`{"error":"unreadable control message"}`)); werr != nil {
					break readLoop
				}
			}
		}
	}
	cancel()
	stream.Close()
	<-done
}

// pumpTerminalOut moves the terminal's own bytes onto the wire.
func pumpTerminalOut(ws *wsConn, stream *herdr.TerminalStream) error {
	scanner := bufio.NewScanner(stream.Out)
	// A screen repaint can be larger than a scanner's default line budget; measured panes here
	// repaint tens of kilobytes at once.
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	cols, rows := 0, 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var chunk struct {
			Bytes  string `json:"bytes"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		}
		if json.Unmarshal([]byte(line), &chunk) != nil || chunk.Bytes == "" {
			// Not a byte chunk: herdr's CLI prints its own notices on this stream too, and they are
			// worth passing on rather than muting, so they go out as text.
			if err := ws.WriteText([]byte(line)); err != nil {
				return err
			}
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(chunk.Bytes)
		if err != nil {
			continue
		}
		if chunk.Width > 0 && chunk.Height > 0 && (chunk.Width != cols || chunk.Height != rows) {
			notice, err := herdr.SizeNotice(chunk.Width, chunk.Height)
			if err != nil {
				return err
			}
			if err := ws.WriteText(notice); err != nil {
				return err
			}
			cols, rows = chunk.Width, chunk.Height
		}
		if err := ws.WriteBinary(decoded); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// handleTerminalControl is the text channel: the things about a terminal that are not its bytes.
func handleTerminalControl(stream *herdr.TerminalStream, payload []byte) error {
	var command struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if err := json.Unmarshal(payload, &command); err != nil {
		return err
	}
	switch command.Type {
	case "resize":
		if command.Cols < 20 || command.Rows < 5 || command.Cols > 1000 || command.Rows > 500 {
			return fmt.Errorf("refusing to resize a terminal to %dx%d", command.Cols, command.Rows)
		}
		return stream.Resize(command.Cols, command.Rows)
	case "release":
		stream.Close()
		return nil
	default:
		return nil
	}
}
