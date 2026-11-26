package core

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// startFakeNotifier writes an executable script at a path of the test's choosing and
// runs it, so /proc reports a command line that looks like the notifier a user would
// have — the script's own path, because that is what a shebang produces.
//
// It used to run `/bin/cat` with a hand-written argv, and that fake was quietly dead:
// `cat` treats the argument that made it *look* like a notifier as a file to read, could
// not open it, and exited — leaving nothing in /proc. The test passed anyway, and only
// because this machine really did have `herdr-notify` running (the test's own comment
// admitted as much), so the assertion was about the user's process rather than about the
// fixture. Removing herdr-notify from the development machine turned that into a red
// test on 2026-10-13, which is how it was found. Nothing about the fixture is now
// assumed: it exists, it is visible, and it is ours.
func startFakeNotifier(t *testing.T, path string) {
	t.Helper()
	full := filepath.Join(t.TempDir(), path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Not `exec sleep`: that would replace the process image and take the path we are
	// asking /proc about with it, which is how the first version of this fix was wrong.
	if err := os.WriteFile(full, []byte("#!/bin/sh\nsleep 300\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	cmd := exec.Command(full)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	// /proc needs a moment to reflect a new process.
	time.Sleep(150 * time.Millisecond)
}

func TestTheOldNotifierIsNoticed(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("this check reads /proc and only exists on Linux")
	}
	startFakeNotifier(t, "bin/herdr-notify.py")

	found, ok := FindCompetingNotifier()
	if !ok {
		t.Fatal("a running herdr-notify should be found; the whole point of this check is to see it")
	}
	if found.PID <= 0 {
		t.Errorf("pid = %d", found.PID)
	}
	// Not asserting *which* notifier: this test may well run on a machine that
	// already has a real one going — as the development machine does — and the
	// contract is "a herdr notifier is noticed", not "this particular one is".
	if !strings.HasSuffix(found.Script, "herdr-notify.py") {
		t.Errorf("script = %q, which is not a herdr notifier", found.Script)
	}
	// The fix line has to be a command the user can run, not a description of one.
	if unit := found.UnitHint(); unit != "herdr-notify.service" {
		t.Errorf("unit hint = %q, want herdr-notify.service", unit)
	}
}

func TestSomethingMerelyCalledNotifyIsNotFlagged(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux only")
	}
	// Narrowness matters: a check that flags anything with "notify" in the name
	// would fire on unrelated background processes, and the user would learn to
	// ignore this line — which is exactly how a warning becomes noise.
	startFakeNotifier(t, "share/some-notify-broker/index.js")

	if found, ok := FindCompetingNotifier(); ok && strings.Contains(found.Script, "some-notify-broker") {
		t.Errorf("an unrelated process was flagged: %+v", found)
	}
}
