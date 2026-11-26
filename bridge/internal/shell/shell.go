package shell

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

const (
	// historyBytes is how much of the session's own output is kept for a client that comes back.
	//
	// A megabyte is about twenty thousand lines of ordinary output — far more than a phone will scroll
	// through, and small enough to send in one burst over a tailnet. It is kept in memory, per session,
	// and only the newest of it survives.
	historyBytes = 1 << 20

	// subscriberQueue is how far behind a client may fall before it is dropped and asked to reconnect.
	subscriberQueue = 512

	defaultCols = 80
	defaultRows = 24
)

// Options is the shell to run and where.
type Options struct {
	Command string
	Args    []string
	Dir     string
	Env     []string
	Cols    int
	Rows    int
}

// DefaultOptions is a login shell for the user this process runs as.
//
// A *login* shell because that is what a person means by "a terminal on my computer": their prompt,
// their PATH, their aliases. `TERM` is set to what the phone's emulator actually is.
func DefaultOptions() Options {
	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		shellPath = "/bin/bash"
	}
	dir := os.Getenv("HOME")
	return Options{
		Command: shellPath,
		Args:    []string{"-l"},
		Dir:     dir,
		Env:     append(WithoutHerdr(os.Environ()), "TERM=xterm-256color"),
		Cols:    defaultCols,
		Rows:    defaultRows,
	}
}

// WithoutHerdr removes herdr's own environment from a shell we are about to start.
//
// The bridge runs as a herdr plug-in in some setups, and those variables describe *which pane this
// process is inside*. An inherited `HERDR_ENV=1` makes herdr refuse to run at all ("already inside a
// herdr session"), which would turn "let me run anything" into "let me run everything except herdr" —
// and the reason a person opens a terminal is usually to poke at the thing that is not working. The
// lab's throwaway-session helper learned this the hard way (`scripts/lib/scratch-session.sh`); this is the
// same lesson one layer down.
func WithoutHerdr(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		// Which names those are is herdr's to spell, and it spells them in one place. Asking that package
		// keeps a rename upstream a one-file change here too, and `herdr.IsSessionEnv` keeps the
		// exception for HERDR_BIN_PATH (where the binary is, not where we are) in the same place.
		if ok && herdr.IsSessionEnv(name) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// Session is one shell, running until it exits or somebody kills it.
type Session struct {
	opts   Options
	cmd    *exec.Cmd
	master *os.File

	mu      sync.Mutex
	history []byte
	cols    int
	rows    int
	subs    map[int]*subscriber
	nextID  int
	exited  bool
	// closedOnPurpose is set when *we* ended the shell because a person asked, so the reason it gives
	// afterwards is "closed on request" rather than the kernel's "signal: killed" — jargon about our own
	// decision, which the phone would otherwise show as if something had gone wrong.
	closedOnPurpose bool
	exitErr         error
	done            chan struct{}
}

// subscriber is one attached client's view of the session.
type subscriber struct {
	ch chan []byte
}

// Start runs the shell on a pseudo-terminal of its own.
func Start(opts Options) (*Session, error) {
	if opts.Command == "" {
		return nil, errors.New("no shell to run")
	}
	if opts.Cols <= 0 {
		opts.Cols = defaultCols
	}
	if opts.Rows <= 0 {
		opts.Rows = defaultRows
	}
	cmd := exec.Command(opts.Command, opts.Args...)
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env
	master, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(opts.Cols),
		Rows: uint16(opts.Rows),
	})
	if err != nil {
		return nil, fmt.Errorf("could not start %s: %w", opts.Command, err)
	}
	s := &Session{
		opts:   opts,
		cmd:    cmd,
		master: master,
		cols:   opts.Cols,
		rows:   opts.Rows,
		subs:   map[int]*subscriber{},
		done:   make(chan struct{}),
	}
	go s.readLoop()
	go s.waitLoop()
	return s, nil
}

// readLoop moves the shell's output into the history and out to whoever is attached.
func (s *Session) readLoop() {
	buf := make([]byte, 32<<10)
	for {
		n, err := s.master.Read(buf)
		if n > 0 {
			s.publish(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// waitLoop notices the shell exiting, so an attached client can be told rather than left staring at a
// screen that will never change again.
func (s *Session) waitLoop() {
	err := s.cmd.Wait()

	s.mu.Lock()
	s.exited = true
	s.exitErr = err
	for id, sub := range s.subs {
		close(sub.ch)
		delete(s.subs, id)
	}
	s.mu.Unlock()

	_ = s.master.Close()
	close(s.done)
}

// publish appends to the history and hands the chunk to every attached client.
func (s *Session) publish(chunk []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.history = append(s.history, chunk...)
	if len(s.history) > historyBytes {
		s.history = append([]byte(nil), s.history[len(s.history)-historyBytes:]...)
	}
	for id, sub := range s.subs {
		select {
		case sub.ch <- chunk:
		default:
			// Dropped rather than shortened: a terminal with bytes missing from the middle of an escape
			// sequence draws something that never happened. Closing this client ends its stream, and the
			// app reconnects and is handed the replay below.
			close(sub.ch)
			delete(s.subs, id)
		}
	}
}

// Snapshot is the recent output, for a client that has just attached.
//
// Trimmed at the *front*, to the first line boundary: the buffer is a ring, so the oldest byte in it was
// dropped mid-stream and the replay may begin in the middle of an escape sequence — which draws one
// garbled line at the top. Everything after that first newline is intact, and the newest output is what
// a client coming back needs.
//
// **The first version of this trimmed at the wrong end** — it kept only the text after the *last*
// newline, which is one line of prompt — and the test caught it by asking for what happened while
// nobody was watching and being shown a prompt instead (`TestTheShellOutlivesItsClientAndReplaysHistory`).
// Worth keeping in mind: "trim to a line boundary" is ambiguous, and the direction is the whole point.
func (s *Session) Snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.history
	if index := firstNewline(history); index >= 0 {
		history = history[index+1:]
	}
	return append([]byte(nil), history...)
}

func firstNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}

// Attachment is one client's hold on a session.
type Attachment struct {
	session *Session
	id      int
	ch      chan []byte
	once    sync.Once
}

// Attach adds a client. The caller must Detach when it goes away.
func (s *Session) Attach() (*Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited {
		return nil, errors.New("the shell has exited")
	}
	sub := &subscriber{ch: make(chan []byte, subscriberQueue)}
	id := s.nextID
	s.nextID++
	s.subs[id] = sub
	return &Attachment{session: s, id: id, ch: sub.ch}, nil
}

// Output is the live stream. It is closed when the shell exits or when this client fell too far behind.
func (a *Attachment) Output() <-chan []byte { return a.ch }

// Detach lets go. The shell keeps running: that is the whole point of it living on the host.
func (a *Attachment) Detach() {
	a.once.Do(func() {
		a.session.mu.Lock()
		if sub, ok := a.session.subs[a.id]; ok {
			close(sub.ch)
			delete(a.session.subs, a.id)
		}
		a.session.mu.Unlock()
	})
}

// Write sends what the user typed to the shell.
func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	exited := s.exited
	s.mu.Unlock()
	if exited {
		return 0, errors.New("the shell has exited")
	}
	return s.master.Write(p)
}

// Resize tells the shell how big the screen is.
//
// This is the piece that keeps a full-screen program honest across a rotation or a zoom: the kernel
// sends SIGWINCH, and a TUI redraws itself for the new shape instead of drawing for the old one.
func (s *Session) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("a terminal cannot be %dx%d", cols, rows)
	}
	if err := pty.Setsize(s.master, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		return err
	}
	s.mu.Lock()
	s.cols, s.rows = cols, rows
	s.mu.Unlock()
	return nil
}

// Size is the shape the shell currently believes it has.
func (s *Session) Size() (cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

// Done is closed when the shell exits.
func (s *Session) Done() <-chan struct{} { return s.done }

// Exited says whether the shell is gone.
//
// Separate from [Done] on purpose, and the difference is a race I lost once on a live bridge: a reader
// woken by its stream being closed could not tell "the shell exited" (say so) from "you fell behind"
// (say nothing, just reconnect) by looking at a channel that closes *after* the streams do. The flag is
// set before anything is closed, so it is the one answer that is already true when a reader asks.
func (s *Session) Exited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

// ExitErr is what the shell exited with, once it has.
func (s *Session) ExitErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitErr
}

// Close ends the shell.
//
// Only ever because a person asked: closing the last client must *not* do this, or a phone that loses
// signal would take the running build with it.
func (s *Session) Close() error {
	s.mu.Lock()
	exited := s.exited
	s.closedOnPurpose = true
	s.mu.Unlock()
	if exited {
		return nil
	}
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return nil
}

// ClosedOnPurpose says whether this shell was ended by a person asking, rather than exiting on its own.
func (s *Session) ClosedOnPurpose() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closedOnPurpose
}

// Manager keeps the one shell this host offers, and restarts it when somebody asks for a fresh one.
type Manager struct {
	mu      sync.Mutex
	opts    Options
	current *Session
}

func NewManager(opts Options) *Manager { return &Manager{opts: opts} }

// Attach returns the running shell, starting one if there is none — or replacing it when `fresh` is set,
// which is what "new terminal" means to a person.
func (m *Manager) Attach(fresh bool) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil {
		select {
		case <-m.current.Done():
			m.current = nil
		default:
		}
	}
	if m.current != nil && fresh {
		_ = m.current.Close()
		<-m.current.Done()
		m.current = nil
	}
	if m.current != nil {
		return m.current, nil
	}
	session, err := Start(m.opts)
	if err != nil {
		return nil, err
	}
	m.current = session
	return session, nil
}

// Current is the running shell, if there is one.
func (m *Manager) Current() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

// Options is what this manager starts shells with.
func (m *Manager) Options() Options {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.opts
}
