package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOnlyOneBridgeCanHoldTheStateDirectory.
//
// Two bridges on one state directory is not a small mistake: the phone would get
// every decision twice, both copies would fight over the queue file, and herdr would
// see two subscribers for every pane. The lock is what makes "install once, start
// once" true even when the plugin's startup hook and the systemd unit fire at the same
// moment — and until now nothing tested it.
func TestOnlyOneBridgeCanHoldTheStateDirectory(t *testing.T) {
	t.Setenv(EnvStateDir, filepath.Join(t.TempDir(), "state"))

	first, err := Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	held, err := first.Acquire()
	if err != nil {
		t.Fatalf("the first bridge could not take the lock: %v", err)
	}

	// A second process — simulated by a second Store over the same directory, which
	// is what flock sees — must be told, not allowed in.
	second, err := Open()
	if err != nil {
		t.Fatalf("open second: %v", err)
	}
	if _, err := second.Acquire(); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("a second bridge got in with %v, want ErrAlreadyRunning", err)
	}

	// The holder's pid is written for diagnostics: "already running (pid N)" is only
	// useful if N is the process that is actually running.
	pid, ok := first.RunningPID()
	if !ok {
		t.Fatal("the holder's pid was not written; 'already running (pid N)' needs it")
	}
	if pid != os.Getpid() {
		t.Errorf("pid file says %d, want %d", pid, os.Getpid())
	}

	// After a clean stop the directory is usable again — the lock is advisory and
	// released, not left behind as a corpse.
	held.Release()
	again, err := second.Acquire()
	if err != nil {
		t.Fatalf("the directory stayed locked after release: %v", err)
	}
	again.Release()
}

// TestAWipedStateDirectoryIsRebuiltRatherThanFatal.
//
// The state directory is disposable by design (constitution I: the computer is the
// source of truth), so deleting it — which the uninstall does, and which a user may
// do — must leave a bridge that starts, pairs and serves. The only visible effect is
// that the phone has to pair again, which is the honest consequence of throwing away
// the token.
func TestAWipedStateDirectoryIsRebuiltRatherThanFatal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	t.Setenv(EnvStateDir, dir)

	store, err := Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	token, err := store.EnsureToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("wipe: %v", err)
	}

	reopened, err := Open()
	if err != nil {
		t.Fatalf("reopen after a wipe: %v", err)
	}
	fresh, err := reopened.EnsureToken()
	if err != nil {
		t.Fatalf("token after a wipe: %v", err)
	}
	if fresh == token {
		t.Error("the token survived the wipe, which would mean it was not really in the state directory")
	}
	if len(fresh) < 20 {
		t.Errorf("the replacement token looks too short to be one: %q", fresh)
	}
	if !strings.HasPrefix(reopened.Path(FilePairing), dir) {
		t.Errorf("the state moved somewhere unexpected: %s", reopened.Path(FilePairing))
	}
}
