package herdr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type TerminalStream struct {
	// Out carries the terminal's own bytes: raw, exactly as the harness wrote them.
	Out io.ReadCloser
	// In takes JSON commands, one per line.
	In io.WriteCloser

	cmd *exec.Cmd
	// diagnostics holds whatever herdr said on stderr, for the one case where it matters.
	diagnostics *terminalDiagnostics
	viewer      *terminalViewer
	closeOnce   sync.Once
}

type terminalDiagnostics struct {
	mu   sync.Mutex
	text strings.Builder
}

func (d *terminalDiagnostics) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.text.Write(p)
}

func (d *terminalDiagnostics) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.text.String()
}

type terminalViewer struct {
	mu         sync.Mutex
	ctx        context.Context
	api        *Client
	cli        *CLI
	target     string
	pending    []byte
	controller *TerminalStream
	closed     bool
	cancel     context.CancelFunc
	done       chan struct{}
	cols       int
	rows       int
}

// Diagnostics is what the stream said on stderr, for a log line when it dies early.
func (t *TerminalStream) Diagnostics() string {
	if t == nil || t.diagnostics == nil {
		return ""
	}
	return strings.TrimSpace(t.diagnostics.String())
}

func (c *CLI) TerminalStream(ctx context.Context, target, machine string, cols, rows int) (*TerminalStream, error) {
	return c.startTerminalStream(ctx, target, machine, VerbControl, cols, rows)
}

func (c *CLI) TerminalView(ctx context.Context, target string, api *Client) (*TerminalStream, error) {
	cols, rows, err := api.PaneGeometry(ctx, target)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	observer, err := c.startTerminalStream(ctx, target, "", VerbObserve, cols, rows)
	if err != nil {
		cancel()
		return nil, err
	}
	reader, writer := io.Pipe()
	v := &terminalViewer{ctx: ctx, cancel: cancel, done: make(chan struct{}), api: api, cli: c, target: target}
	go v.observe(writer, observer, cols, rows)
	return &TerminalStream{Out: reader, viewer: v}, nil
}

func (v *terminalViewer) observe(out *io.PipeWriter, observer *TerminalStream, cols, rows int) {
	defer close(v.done)
	defer func() { observer.Close() }()
	defer func() { _ = out.Close() }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	copyOutput := func() <-chan error {
		done := make(chan error, 1)
		current := observer
		go func() {
			_, err := io.Copy(out, current.Out)
			done <- err
		}()
		return done
	}
	pump := copyOutput()
	for {
		select {
		case <-v.ctx.Done():
			observer.Close()
			<-pump
			return
		case err := <-pump:
			if err == nil {
				err = fmt.Errorf("terminal observer ended: %s", observer.Diagnostics())
			}
			_ = out.CloseWithError(err)
			return
		case <-ticker.C:
			v.mu.Lock()
			nextCols, nextRows := v.cols, v.rows
			v.mu.Unlock()
			if nextCols == 0 || nextRows == 0 {
				ctx, cancel := context.WithTimeout(v.ctx, 3*time.Second)
				var err error
				nextCols, nextRows, err = v.api.PaneGeometry(ctx, v.target)
				cancel()
				if err != nil {
					continue
				}
			}
			if nextCols == cols && nextRows == rows {
				continue
			}
			observer.Close()
			<-pump
			next, err := v.cli.startTerminalStream(v.ctx, v.target, "", VerbObserve, nextCols, nextRows)
			if err != nil {
				_ = out.CloseWithError(err)
				return
			}
			observer = next
			cols, rows = nextCols, nextRows
			pump = copyOutput()
		}
	}
}

func (c *CLI) startTerminalStream(ctx context.Context, target, machine, mode string, cols, rows int) (*TerminalStream, error) {
	if c.Bin == "" {
		return nil, ErrCLIMissing
	}
	if target == "" {
		return nil, fmt.Errorf("which terminal? a pane id or agent name is required")
	}
	args := []string{}
	if machine != "" {
		args = append(args, FlagMachine, machine)
	}
	args = append(args, GroupTerminal, GroupSession, mode, target)
	if cols > 0 && rows > 0 {
		args = append(args, FlagCols, fmt.Sprint(cols), FlagRows, fmt.Sprint(rows))
	}

	cmd := exec.CommandContext(ctx, c.Bin, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// stderr is where herdr explains a refused command ("invalid json command: …"), and swallowing
	// it once already cost an afternoon of guessing at a silent stream. It is captured rather than
	// shown, because a terminal's stderr is noise 99% of the time and evidence the other 1%.
	var diagnostics terminalDiagnostics
	cmd.Stderr = &diagnostics
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &TerminalStream{Out: stdout, In: stdin, cmd: cmd, diagnostics: &diagnostics}, nil
}

// Close ends the stream. The pane is untouched: it belongs to herdr and to the person sitting at
// the computer, and a client that walks away must not take their terminal with it.
func (t *TerminalStream) Close() {
	if t == nil {
		return
	}
	t.closeOnce.Do(func() {
		if t.viewer != nil {
			t.viewer.cancel()
			_ = t.Out.Close()
			t.viewer.mu.Lock()
			t.viewer.closed = true
			if t.viewer.controller != nil {
				t.viewer.controller.Close()
			}
			t.viewer.mu.Unlock()
			<-t.viewer.done
			return
		}
		_ = t.In.Close()
		if t.cmd != nil && t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
			_ = t.cmd.Wait()
		}
	})
}

func (t *TerminalStream) WriteInput(payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	if t.viewer == nil {
		line, err := InputCommand(payload)
		if err != nil {
			return err
		}
		_, err = t.In.Write(append(line, '\n'))
		return err
	}
	v := t.viewer
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return io.ErrClosedPipe
	}
	if v.controller != nil {
		return v.controller.WriteInput(payload)
	}
	data := append(v.pending, payload...)
	end := 0
	for end < len(data) {
		r, size := utf8.DecodeRune(data[end:])
		if r == utf8.RuneError && size == 1 {
			if !utf8.FullRune(data[end:]) {
				break
			}
			return fmt.Errorf("terminal input is not UTF-8")
		}
		end += size
	}
	if end > 0 {
		ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
		defer cancel()
		if err := v.api.SendText(ctx, v.target, string(data[:end])); err != nil {
			return err
		}
	}
	v.pending = append([]byte(nil), data[end:]...)
	return nil
}

func (t *TerminalStream) Resize(cols, rows int) error {
	if t.viewer == nil {
		line, err := ResizeCommand(cols, rows)
		if err != nil {
			return err
		}
		_, err = t.In.Write(append(line, '\n'))
		return err
	}
	v := t.viewer
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return io.ErrClosedPipe
	}
	if len(v.pending) != 0 {
		return fmt.Errorf("terminal input has an incomplete UTF-8 character")
	}
	if v.controller != nil {
		if err := v.controller.Resize(cols, rows); err != nil {
			return err
		}
		v.cols, v.rows = cols, rows
		return nil
	}
	controller, err := v.cli.TerminalStream(v.ctx, v.target, "", cols, rows)
	if err != nil {
		return err
	}
	v.controller = controller
	v.cols, v.rows = cols, rows
	go func() {
		_, _ = io.Copy(io.Discard, controller.Out)
		v.cancel()
	}()
	return nil
}

// The command vocabulary of herdr's terminal stream.
//
// It lives here, in one place, for the same reason our own stream events live in `core/events.go`:
// a vocabulary written as string literals scattered through the code is a vocabulary nobody can
// enumerate, and a rename on one side becomes a silent no-op. This one is herdr's protocol rather
// than ours, which is why it is defined next to the code that speaks it rather than with our events.
//
// Measured against herdr 0.9.1 (2026-10-14): the four names below are the whole of it, and
// `terminal.input` is the one that makes a terminal a terminal.
const (
	// CommandInput writes bytes into the terminal, as if they had been typed.
	CommandInput = "terminal.input"
	// CommandResize tells the terminal how big the phone's screen is.
	CommandResize = "terminal.resize"
	// CommandScroll moves the terminal's own scrollback.
	CommandScroll = "terminal.scroll"
	// CommandRelease lets go of the screen without touching the process in the pane.
	CommandRelease = "terminal.release"
)

// InputCommand encodes bytes to type into the terminal.
func InputCommand(payload []byte) ([]byte, error) {
	return json.Marshal(map[string]string{
		"type":  CommandInput,
		"bytes": base64.StdEncoding.EncodeToString(payload),
	})
}

// ResizeCommand encodes a new terminal size.
func ResizeCommand(cols, rows int) ([]byte, error) {
	return json.Marshal(map[string]any{
		"type": CommandResize,
		"cols": cols,
		"rows": rows,
	})
}

// ReleaseCommand encodes "I am done with this screen".
func ReleaseCommand() ([]byte, error) {
	return json.Marshal(map[string]string{"type": CommandRelease})
}

func SizeNotice(cols, rows int) ([]byte, error) {
	return json.Marshal(map[string]any{"type": NoticeSize, "cols": cols, "rows": rows})
}

// NoticeSize is the type name of the bridge's geometry message on the terminal's text channel.
const NoticeSize = "size"
