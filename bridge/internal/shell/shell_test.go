package shell

import (
	"os"
	"strings"
	"testing"
	"time"
)

// These are real shells on real pseudo-terminals, because that is the only way to know that a terminal
// works: no herdr, no network, no phone — just a process, a pty, and the bytes that come back.

func testOptions(t *testing.T) Options {
	t.Helper()
	return Options{
		Command: "/bin/sh",
		Args:    []string{"-i"},
		Dir:     t.TempDir(),
		Env:     []string{"PATH=/usr/bin:/bin", "TERM=xterm", "HOME=" + t.TempDir()},
		Cols:    80,
		Rows:    24,
	}
}

// waitForOutput reads until the wanted text arrives, or the test fails with what did arrive.
func waitForOutput(t *testing.T, ch <-chan []byte, wanted string, within time.Duration) string {
	t.Helper()
	deadline := time.After(within)
	var seen strings.Builder
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				t.Fatalf("the stream ended while waiting for %q; saw %q", wanted, seen.String())
			}
			seen.Write(chunk)
			if strings.Contains(seen.String(), wanted) {
				return seen.String()
			}
		case <-deadline:
			t.Fatalf("waited %s for %q; saw %q", within, wanted, seen.String())
		}
	}
}

// runCommand types a command and waits for its output, typing it again if nothing comes back.
//
// The retry is not papering over a bug: an interactive shell that has *just* started is not always reading
// yet, and a line typed into that window is echoed and then ignored — measured here (2 of 5 runs, before
// this loop existed). A person does the same thing, which is the reason a retry is the honest shape and
// the reason the product's own verification (`scripts/verify-shell.sh`, through the bridge, with the shell
// already attached) does not have this problem.
func runCommand(t *testing.T, session *Session, attachment *Attachment, command, wanted string) string {
	t.Helper()
	var seen strings.Builder
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := session.Write([]byte(command + "\n")); err != nil {
			t.Fatalf("write %q: %v", command, err)
		}
		got, ok := readWithin(attachment.Output(), wanted, 2*time.Second)
		seen.WriteString(got)
		if ok || strings.Contains(seen.String(), wanted) {
			return seen.String()
		}
	}
	t.Fatalf("typed %q repeatedly and never saw %q; saw %q", command, wanted, seen.String())
	return ""
}

// readWithin is waitForOutput without the failure: it answers whether the text arrived in time.
func readWithin(ch <-chan []byte, wanted string, within time.Duration) (string, bool) {
	deadline := time.After(within)
	var seen strings.Builder
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return seen.String(), false
			}
			seen.Write(chunk)
			if strings.Contains(seen.String(), wanted) {
				return seen.String(), true
			}
		case <-deadline:
			return seen.String(), false
		}
	}
}

// A command runs, and its *output* comes back — not just the echo of what was typed.
//
// The command is chosen so that its output cannot appear in the input: `$((6*7))` is arithmetic the
// shell has to do, so seeing `perch-42` is evidence of execution rather than of a terminal echoing.
func TestAShellRunsCommandsAndAnswers(t *testing.T) {
	session, err := Start(testOptions(t))
	if err != nil {
		t.Fatalf("could not start a shell: %v", err)
	}
	defer session.Close()

	attachment, err := session.Attach()
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer attachment.Detach()

	// Wait for the prompt first, the way a person does, and then type: this test lost a race once (the line
	// was echoed and never ran), so `runCommand` types again if a freshly started shell was not reading.
	waitForOutput(t, attachment.Output(), "$", 5*time.Second)
	runCommand(t, session, attachment, "echo perch-$((6*7))", "perch-42")
}

func TestTheShellOutlivesItsClientAndReplaysHistory(t *testing.T) {
	session, err := Start(testOptions(t))
	if err != nil {
		t.Fatalf("could not start a shell: %v", err)
	}
	defer session.Close()

	first, err := session.Attach()
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, err := session.Write([]byte("echo perch-before-leaving\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitForOutput(t, first.Output(), "perch-before-leaving", 5*time.Second)
	first.Detach()

	// While nobody is attached the shell keeps working — the part of mosh that matters most here.
	if _, err := session.Write([]byte("printf 'perch-missed-%s\\n' 7\n")); err != nil {
		t.Fatalf("write while detached: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	select {
	case <-session.Done():
		t.Fatal("the shell exited when its only client detached; a lost phone must not kill the session")
	default:
	}

	second, err := session.Attach()
	if err != nil {
		t.Fatalf("re-attach: %v", err)
	}
	defer second.Detach()

	// The replay is what the client sees first, so the screen it draws is the terminal that has been
	// running rather than an empty one.
	replay := string(session.Snapshot())
	if !strings.Contains(replay, "perch-before-leaving") {
		t.Errorf("the replay is missing earlier output: %q", replay)
	}
	if !strings.Contains(replay, "perch-missed-7") {
		t.Errorf("the replay is missing what happened while nobody was watching: %q", replay)
	}
}

// A client that cannot keep up is dropped rather than handed a terminal with holes in it.
//
// Dropping bytes would corrupt every escape sequence spanning the gap, so the stream is closed and the
// app reconnects into a replay. Half a screen is worse than a screen that comes back.
func TestASlowClientIsDroppedNotShortened(t *testing.T) {
	session, err := Start(testOptions(t))
	if err != nil {
		t.Fatalf("could not start a shell: %v", err)
	}
	defer session.Close()

	attachment, err := session.Attach()
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	// Nobody reads this channel, and nobody ever will.
	for i := 0; i < subscriberQueue+10; i++ {
		session.publish([]byte("chunk\n"))
	}

	select {
	case _, ok := <-attachment.Output():
		if ok {
			// Drain whatever was buffered before the drop; the close is what matters.
			for range attachment.Output() {
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an overwhelmed client was neither served nor dropped")
	}

	session.mu.Lock()
	_, stillAttached := session.subs[attachment.id]
	session.mu.Unlock()
	if stillAttached {
		t.Error("the dropped client is still attached; it would receive a stream with a hole in it")
	}
}

// The replay starts at a line boundary, because a client that begins mid-escape-sequence draws a
// garbled line — the one artifact of "send the buffer" that is worth preventing.
func TestTheReplayStartsOnALineBoundary(t *testing.T) {
	session := &Session{history: []byte("first line\nsecond line\nthird")}

	got := string(session.Snapshot())
	if got != "second line\nthird" {
		t.Errorf("replay = %q, want it to start after the last complete line", got)
	}

	session.history = []byte("no newline at all")
	if got := string(session.Snapshot()); got != "no newline at all" {
		t.Errorf("a history with no newline should be replayed whole, got %q", got)
	}
}

// The shell hears about a resize, which is what makes a full-screen program redraw for the phone.
//
// The proof is the shell's own answer: `stty size` reports what the kernel has, so this measures the
// whole path (setsize → SIGWINCH → the program's view of its world) rather than our own bookkeeping.
func TestResizeReachesTheShell(t *testing.T) {
	session, err := Start(testOptions(t))
	if err != nil {
		t.Fatalf("could not start a shell: %v", err)
	}
	defer session.Close()

	attachment, err := session.Attach()
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer attachment.Detach()

	if err := session.Resize(100, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if cols, rows := session.Size(); cols != 100 || rows != 40 {
		t.Errorf("size = %dx%d, want 100x40", cols, rows)
	}

	runCommand(t, session, attachment, "stty size", "40 100")
}

// Closing is something a person asks for; it is never what a lost connection does.
func TestCloseEndsTheShell(t *testing.T) {
	session, err := Start(testOptions(t))
	if err != nil {
		t.Fatalf("could not start a shell: %v", err)
	}
	attachment, err := session.Attach()
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer attachment.Detach()

	if err := session.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the shell did not exit when it was closed")
	}
	if _, err := session.Write([]byte("echo nobody-is-listening\n")); err == nil {
		t.Error("writing to a closed shell should fail rather than be dropped silently")
	}
}

// "New terminal" means a new shell, and the old one stops: a person asking for a fresh terminal is
// usually asking *because* the old one is stuck.
func TestAskingForAFreshTerminalReplacesTheShell(t *testing.T) {
	manager := NewManager(testOptions(t))
	defer func() {
		if current := manager.Current(); current != nil {
			_ = current.Close()
		}
	}()

	first, err := manager.Attach(false)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	again, err := manager.Attach(false)
	if err != nil {
		t.Fatalf("attach again: %v", err)
	}
	if first != again {
		t.Error("attaching without asking for a fresh terminal started a second shell")
	}

	fresh, err := manager.Attach(true)
	if err != nil {
		t.Fatalf("attach fresh: %v", err)
	}
	if fresh == first {
		t.Fatal("asking for a fresh terminal returned the old shell")
	}
	select {
	case <-first.Done():
	case <-time.After(5 * time.Second):
		t.Error("the replaced shell is still running")
	}
}

// The shell we start is a normal shell on the user's machine, not a child of a herdr pane.
//
// `HERDR_ENV=1` would make herdr refuse to run inside it ("already inside a herdr session"), and poking
// at the tools that are not working is the main reason to want a terminal at all.
func TestTheShellDoesNotInheritHerdrInsideAHerdr(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HERDR_ENV=1",
		"HERDR_PANE_ID=wA:p1",
		"HERDR_TAB_ID=t1",
		"HERDR_WORKSPACE_ID=wA",
		"HERDR_SOCKET_PATH=/tmp/herdr.sock",
		"HERDR_BIN_PATH=/usr/local/bin/herdr",
	}

	got := WithoutHerdr(env)
	joined := strings.Join(got, " ")
	for _, unwanted := range []string{"HERDR_ENV", "HERDR_PANE_ID", "HERDR_TAB_ID", "HERDR_WORKSPACE_ID", "HERDR_SOCKET_PATH"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("%s survived: %s", unwanted, joined)
		}
	}
	// The path to the binary is not "which pane am I in": it stays, so `herdr` still works inside.
	if !strings.Contains(joined, "HERDR_BIN_PATH") {
		t.Errorf("HERDR_BIN_PATH was removed, but it says where the binary is rather than where we are: %s", joined)
	}
	if !strings.Contains(joined, "PATH=/usr/bin") {
		t.Errorf("the rest of the environment was not left alone: %s", joined)
	}
	if os.Getenv("SHELL") == "" && DefaultOptions().Command == "" {
		t.Error("a machine without SHELL still needs a shell to run")
	}
}
