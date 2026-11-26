package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What a cloud machine needs from the installer.
//
// The failing case is not exotic: `ssh vm 'perch-bridge install-service'` is how
// the product is meant to be set up on a server, and it has no user session, so
// systemctl cannot find the user manager and every --user command fails — while
// the manager is running fine. The fix is one environment variable, and these
// tests pin the two ways it can be wrong: inventing a directory that is not there,
// and overriding a value the platform already gave us.
func TestUserSessionEnvFillsInWhatAnSshSessionLeavesOut(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "1000")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	env := userSessionEnvFor([]string{"PATH=/usr/bin"}, 1000, base)
	if got := lookupEnv(env, "XDG_RUNTIME_DIR"); got != dir {
		t.Errorf("XDG_RUNTIME_DIR = %q, want %q", got, dir)
	}

	// With a bus socket present the address comes too, so systemctl does not have to
	// guess how to reach it.
	bus := filepath.Join(dir, "bus")
	if err := os.WriteFile(bus, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	env = userSessionEnvFor([]string{"PATH=/usr/bin"}, 1000, base)
	if got := lookupEnv(env, "DBUS_SESSION_BUS_ADDRESS"); got != "unix:path="+bus {
		t.Errorf("DBUS_SESSION_BUS_ADDRESS = %q, want the bus in the runtime directory", got)
	}
	// And the environment we were handed is still in there: this adds, it does not
	// replace, because the child process needs PATH and HOME as much as we do.
	if lookupEnv(env, "PATH") != "/usr/bin" {
		t.Errorf("the caller's own environment must survive: %v", env)
	}
}

func TestUserSessionEnvNeverInventsADirectory(t *testing.T) {
	// A machine with no user manager: /run/user/<uid> does not exist. Adding a path
	// to nothing would turn "there is no user session here" into a stranger error
	// from systemctl, and the honest sentence for the user is the one the installer
	// already prints.
	base := t.TempDir()
	env := userSessionEnvFor([]string{"PATH=/usr/bin"}, 1000, base)
	for _, entry := range env {
		if strings.HasPrefix(entry, "XDG_RUNTIME_DIR=") ||
			strings.HasPrefix(entry, "DBUS_SESSION_BUS_ADDRESS=") {
			t.Errorf("nothing should be added when the runtime directory is absent, got %q", entry)
		}
	}
}

func TestUserSessionEnvKeepsWhatThePlatformAlreadySaid(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "1000")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	// The service's own systemd unit has these set. Overwriting them would point a
	// correctly-configured process at a different session, which is a worse bug
	// than the one being fixed.
	env := userSessionEnvFor(
		[]string{"XDG_RUNTIME_DIR=/somewhere/else", "DBUS_SESSION_BUS_ADDRESS=unix:path=/x"},
		1000, base,
	)
	if got := lookupEnv(env, "XDG_RUNTIME_DIR"); got != "/somewhere/else" {
		t.Errorf("XDG_RUNTIME_DIR = %q, want the caller's value", got)
	}
	if got := lookupEnv(env, "DBUS_SESSION_BUS_ADDRESS"); got != "unix:path=/x" {
		t.Errorf("DBUS_SESSION_BUS_ADDRESS = %q, want the caller's value", got)
	}
	if len(env) != 2 {
		t.Errorf("nothing should have been appended: %v", env)
	}
}
