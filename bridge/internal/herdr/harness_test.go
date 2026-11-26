package herdr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHarnessLookupUsesThePathItIsGiven(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write harness: %v", err)
	}

	found, err := HarnessForIn(dir, "codex")
	if err != nil {
		t.Fatalf("HarnessForIn: %v", err)
	}
	if found.Path != path {
		t.Errorf("path = %q, want %q", found.Path, path)
	}

	available := AvailableHarnessesIn(dir)
	if len(available) != 1 || available[0].Kind != "codex" {
		t.Errorf("available = %+v, want only codex", available)
	}

	if _, err := HarnessForIn(t.TempDir(), "codex"); err == nil {
		t.Error("an empty PATH found codex")
	}
}

func TestDshIsFoundThroughItsCliLauncher(t *testing.T) {
	dir := t.TempDir()
	launcher := filepath.Join(dir, "dsh-tui")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write dsh-tui: %v", err)
	}

	found, err := HarnessForIn(dir, "dsh-tui")
	if err != nil {
		t.Fatalf("HarnessForIn: %v", err)
	}
	if found.Path != launcher {
		t.Errorf("path = %q, want %q", found.Path, launcher)
	}
	if !StartsInPane("dsh-tui") {
		t.Error("dsh-tui should start in a pane, not through agent.start")
	}
	if got := DetectedAgent("dsh-tui"); got != "dsh-tui" {
		t.Errorf("detected agent = %q, want dsh-tui", got)
	}
}

func TestGeminiIsNotASupportedHarness(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "gemini"), []byte("#!/bin/sh\n"), 0o755,
	); err != nil {
		t.Fatalf("write gemini: %v", err)
	}
	if _, err := HarnessForIn(dir, "gemini"); err == nil {
		t.Fatal("an installed gemini was treated as a supported harness")
	}
	if available := AvailableHarnessesIn(dir); len(available) != 0 {
		t.Errorf("available = %+v, want no supported harnesses", available)
	}
}
