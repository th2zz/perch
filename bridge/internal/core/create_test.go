package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNameWithSuffixKeepsTheSuffixOnAFullLengthName(t *testing.T) {
	base := strings.Repeat("a", 32)
	got := nameWithSuffix(base, 2)
	if got == base {
		t.Fatalf("collision name is unchanged: %q", got)
	}
	if len(got) > 32 {
		t.Fatalf("collision name is too long: %q", got)
	}
	if !strings.HasSuffix(got, "-2") {
		t.Fatalf("collision name lost its suffix: %q", got)
	}
}

func TestMergePathEnvKeepsTheFocusedPaneFirstAndDropsDuplicates(t *testing.T) {
	got := mergePathEnv(
		strings.Join([]string{"/one", "/two"}, string(os.PathListSeparator)),
		strings.Join([]string{"/three", "/one"}, string(os.PathListSeparator)),
	)
	want := strings.Join([]string{"/one", "/two", "/three"}, string(os.PathListSeparator))
	if got != want {
		t.Errorf("merged PATH = %q, want %q", got, want)
	}
}

func TestCreationEnvOnlyOverridesPathWhenTheBridgeCannotFindTheHarness(t *testing.T) {
	t.Setenv("PATH", "/nowhere")
	env := creationEnv("/pane/bin", "codex")
	if env["PATH"] != "/pane/bin" {
		t.Errorf("env = %v, want the merged pane PATH", env)
	}
}

func TestDshProfileProblemNamesTheMissingProfile(t *testing.T) {
	t.Setenv("DSH_HOME", t.TempDir())
	if err := dshProfileProblem(); err == nil || !strings.Contains(err.Error(), "dsh-tui") {
		t.Errorf("profile error = %v, want a dsh-tui instruction", err)
	}
}

func TestDshProfileProblemNamesTheMissingCliPlugin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	profile := filepath.Join(home, "profiles", "dsh-tui")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatalf("make profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profile, "cordis.patch.yml"), []byte("[]\n"), 0o644); err != nil {
		t.Fatalf("write patch: %v", err)
	}
	if err := dshProfileProblem(); err == nil || !strings.Contains(err.Error(), "dsh plugin") {
		t.Errorf("plugin error = %v, want the install command", err)
	}

	plugin := filepath.Join(
		profile, "node_modules", "@deepseek-harness-tui", "dsh-tui", "package.json",
	)
	if err := os.MkdirAll(filepath.Dir(plugin), 0o755); err != nil {
		t.Fatalf("make plugin: %v", err)
	}
	if err := os.WriteFile(plugin, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write plugin: %v", err)
	}
	if err := dshProfileProblem(); err != nil {
		t.Errorf("a ready profile was refused: %v", err)
	}
}

func TestLastLinesKeepsTheTailBounded(t *testing.T) {
	got := lastLines("one\ntwo\nthree\nfour", 2)
	if got != "three\nfour" {
		t.Errorf("tail = %q, want the last two lines", got)
	}
}

func TestEnsureDshOnboardingSkipsTheFirstRunWizard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".dsh-tui")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make dsh-tui dir: %v", err)
	}
	path := filepath.Join(dir, "onboarding.json")
	if err := os.WriteFile(path, []byte(`{"completed":false,"version":0}`), 0o600); err != nil {
		t.Fatalf("write onboarding: %v", err)
	}
	if err := ensureDshOnboarding(); err != nil {
		t.Fatalf("ensure onboarding: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read onboarding: %v", err)
	}
	var state struct {
		Completed bool `json:"completed"`
		Version   int  `json:"version"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode onboarding: %v", err)
	}
	if !state.Completed || state.Version < 1 {
		t.Errorf("onboarding = %+v, want completed at version 1", state)
	}
}
