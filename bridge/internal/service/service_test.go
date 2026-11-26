package service

import (
	"strings"
	"testing"
)

// TestBothStartupFilesAreRenderedFromTheCodeThatInstallsThem.
//
// The macOS plist cannot be exercised on Linux, so the honest option is to render it
// with the same function the macOS build calls and read it here. Anything weaker —
// a copy of the template in the test — proves only that the copy is valid.
func TestBothStartupFilesAreRenderedFromTheCodeThatInstallsThem(t *testing.T) {
	linux, err := RenderFor("linux", "/usr/local/bin/perch-bridge")
	if err != nil {
		t.Fatalf("linux: %v", err)
	}
	// The environment the service needs is part of the unit, not of the shell that installed it:
	// measured on 2026-10-14, a bridge under systemd could not find `herdr` at all, which silently
	// broke the terminal and every call to another computer.
	for _, want := range []string{
		"[Unit]",
		"ExecStart=/usr/local/bin/perch-bridge run",
		"WantedBy=default.target",
		"Environment=PATH=",
	} {
		if !strings.Contains(linux, want) {
			t.Errorf("the systemd unit is missing %q:\n%s", want, linux)
		}
	}

	darwin, err := RenderFor("darwin", "/opt/homebrew/bin/perch-bridge")
	if err != nil {
		t.Fatalf("darwin: %v", err)
	}
	for _, want := range []string{
		"<string>dev.perch.bridge</string>",
		"<string>/opt/homebrew/bin/perch-bridge</string>",
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
	} {
		if !strings.Contains(darwin, want) {
			t.Errorf("the launchd plist is missing %q:\n%s", want, darwin)
		}
	}
	// A plist that is not valid XML is not a plist, and launchd would reject it at
	// the moment the user is least able to debug it.
	if !strings.HasPrefix(darwin, "<?xml") || !strings.Contains(darwin, "</plist>") {
		t.Errorf("the plist is not a complete document:\n%s", darwin)
	}
}

// TestTheUnitGivesUpInsteadOfRetryingForEver.
//
// `Restart=on-failure` with a five-second delay is a loop, not a recovery: measured on
// systemd 252, an always-failing service was restarted eleven times in a minute and sat
// in "activating (auto-restart)" indefinitely, because the default rate limit never
// trips at that interval. The unit's own comment promises a visible failed state; the
// two directives below are what keep that promise, and this test is what keeps them.
func TestTheUnitGivesUpInsteadOfRetryingForEver(t *testing.T) {
	unit, err := RenderFor("linux", "/usr/local/bin/perch-bridge")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{"StartLimitIntervalSec=300", "StartLimitBurst=10"} {
		if !strings.Contains(unit, want) {
			t.Errorf("the unit has no %s, so a permanent failure retries for ever:\n%s", want, unit)
		}
	}
}

func TestAnUnsupportedPlatformIsRefusedRatherThanGuessed(t *testing.T) {
	// Rendering a Linux unit for a Windows machine would install a file nothing
	// reads, which looks like success and behaves like nothing.
	if _, err := RenderFor("windows", "perch-bridge"); err == nil {
		t.Error("windows was accepted")
	}
}

func TestTheUnitNamesOneExecutableAndOneWayToRun(t *testing.T) {
	// The unit's ExecStart and this assertion agree on purpose: a unit that starts
	// the binary with the wrong subcommand is a service that fails at every boot,
	// and nothing in a build would notice.
	unit, err := RenderFor("linux", "/home/someone/.local/bin/perch-bridge")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(unit, "ExecStart=/home/someone/.local/bin/perch-bridge run") {
		t.Errorf("ExecStart does not run the bridge:\n%s", unit)
	}
}
