package state

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestTheLogFileIsPrivateAndAppended.
//
// The log holds pane ids, agent names and whatever herdr reported — not secrets, but
// not public either, and on a shared machine the difference matters.
func TestTheLogFileIsPrivateAndAppended(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes do not describe Windows ACLs")
	}
	dir := t.TempDir()

	first, err := OpenLog(dir, 1<<20)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := first.WriteString("a line\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = first.Close()

	// A restart appends: the reason the last run ended is usually in the lines just
	// before it, and truncating them away is how a log stops being useful.
	second, err := OpenLog(dir, 1<<20)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := second.WriteString("another line\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = second.Close()

	body, err := os.ReadFile(filepath.Join(dir, FileLog))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "a line") || !strings.Contains(string(body), "another line") {
		t.Errorf("the log did not survive a restart: %q", body)
	}
	if mode := modeOf(t, filepath.Join(dir, FileLog)); mode != 0o600 {
		t.Errorf("the log is %o, want 0600", mode)
	}
}

// TestTheLogIsRotatedRatherThanGrowingForever.
//
// Nobody configures logrotate for something they installed with one command, and a
// daemon that runs for months must not be able to fill a disk.
func TestTheLogIsRotatedRatherThanGrowingForever(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileLog)

	if err := os.WriteFile(path, []byte(strings.Repeat("x", 4096)), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	file, err := OpenLog(dir, 1024)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := file.WriteString("after rotation\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = file.Close()

	// The previous generation is kept: a truncate would destroy exactly the lines
	// someone is looking for after a crash.
	previous, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("no previous generation: %v", err)
	}
	if len(previous) != 4096 {
		t.Errorf("the previous log is %d bytes, want the 4096 it had", len(previous))
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(current), "xxxx") {
		t.Errorf("the current log still holds the rotated content: %q", current)
	}
}

// TestTheBridgeDoesNotDoubleLogWhenItsOutputIsAlreadyCaptured.
//
// The plugin's detached start and launchd both send stdout/stderr into the log file.
// A child that also opened the file would write every line twice, which reads as a
// bug in whatever the log is describing.
func TestTheBridgeDoesNotDoubleLogWhenItsOutputIsAlreadyCaptured(t *testing.T) {
	t.Setenv(EnvLogRedirected, "")
	if !LogIfNotRedirected() {
		t.Error("with nothing capturing our output, the bridge must keep its own log")
	}

	t.Setenv(EnvLogRedirected, "1")
	if LogIfNotRedirected() {
		t.Error("with the output already captured, the bridge must not open the file again")
	}
}
