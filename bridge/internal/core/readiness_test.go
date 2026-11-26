package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

func probeEnv(t *testing.T) []string {
	t.Helper()
	return append(os.Environ(), "PATH="+os.Getenv("PATH"))
}

func readyDshHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	readyDshHomeAt(t, filepath.Join(home, "profiles", "dsh-tui"))
}

func fakeProbeHarness(t *testing.T, script string) herdr.Harness {
	t.Helper()
	path := filepath.Join(t.TempDir(), "harness")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write harness: %v", err)
	}
	return herdr.Harness{Kind: "probe", Executable: "harness", Path: path}
}

func envWithout(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return out
}

func TestHarnessReadinessDshNeedsItsProfile(t *testing.T) {
	t.Setenv("DSH_HOME", t.TempDir())
	harness := fakeProbeHarness(t, "#!/bin/sh\nexit 0\n")
	harness.Kind = "dsh-tui"
	readiness := harnessReadiness(context.Background(), harness, probeEnv(t))
	if readiness.Ready || !readiness.NeedsSetup {
		t.Fatalf("readiness = %+v, want setup required", readiness)
	}
	if !strings.Contains(readiness.Fix, "dsh-tui") {
		t.Errorf("fix = %q, want a dsh-tui instruction", readiness.Fix)
	}
}

func TestHarnessReadinessDshNeedsAGreenDoctor(t *testing.T) {
	readyDshHome(t)
	harness := fakeProbeHarness(t, "#!/bin/sh\necho '✗ DEEPSEEK_API_KEY: missing'\nexit 1\n")
	harness.Kind = "dsh-tui"
	readiness := harnessReadiness(context.Background(), harness, probeEnv(t))
	if readiness.Ready || !readiness.NeedsSetup {
		t.Fatalf("readiness = %+v, want setup required", readiness)
	}
	if !strings.Contains(readiness.Reason, "DEEPSEEK_API_KEY") {
		t.Errorf("reason = %q, want the failing check", readiness.Reason)
	}
	if !strings.Contains(readiness.Fix, "doctor") {
		t.Errorf("fix = %q, want the doctor command", readiness.Fix)
	}
}

func TestHarnessReadinessDshIsReadyWhenDoctorIsGreen(t *testing.T) {
	readyDshHome(t)
	harness := fakeProbeHarness(t, "#!/bin/sh\necho '✓ profile: 0.16.1'\necho '✓ DEEPSEEK_API_KEY: set'\nexit 0\n")
	harness.Kind = "dsh-tui"
	readiness := harnessReadiness(context.Background(), harness, probeEnv(t))
	if !readiness.Ready || readiness.NeedsSetup {
		t.Fatalf("readiness = %+v, want ready", readiness)
	}
}

func TestHarnessReadinessClaudeReadsAuthStatus(t *testing.T) {
	harness := fakeProbeHarness(t, "#!/bin/sh\necho '{\"loggedIn\":false}'\n")
	harness.Kind = "claude"
	readiness := harnessReadiness(context.Background(), harness, probeEnv(t))
	if readiness.Ready || !readiness.NeedsSetup {
		t.Fatalf("readiness = %+v, want setup required", readiness)
	}

	harness = fakeProbeHarness(t, "#!/bin/sh\necho '{\"loggedIn\":true}'\n")
	harness.Kind = "claude"
	readiness = harnessReadiness(context.Background(), harness, probeEnv(t))
	if !readiness.Ready {
		t.Fatalf("readiness = %+v, want ready", readiness)
	}
}

func TestHarnessReadinessClaudeUnknownOutputDoesNotBlock(t *testing.T) {
	harness := fakeProbeHarness(t, "#!/bin/sh\nexit 1\n")
	harness.Kind = "claude"
	readiness := harnessReadiness(context.Background(), harness, probeEnv(t))
	if !readiness.Ready {
		t.Fatalf("readiness = %+v, an older CLI must not be blocked", readiness)
	}
}

func TestHarnessReadinessClaudeReadsJSONEvenWhenTheCommandFails(t *testing.T) {
	harness := fakeProbeHarness(t, "#!/bin/sh\necho '{\"loggedIn\":false}'\nexit 1\n")
	harness.Kind = "claude"
	readiness := harnessReadiness(context.Background(), harness, probeEnv(t))
	if readiness.Ready || !readiness.NeedsSetup {
		t.Fatalf("readiness = %+v, want setup required", readiness)
	}
}

func TestHarnessReadinessCodexFallsBackToItsConfiguredProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(
		"model_provider = \"deepseek\"\n\n[model_providers.deepseek]\nenv_key = \"DEEPSEEK_API_KEY\"\n",
	), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	harness := fakeProbeHarness(t, "#!/bin/sh\necho 'Not logged in'\nexit 1\n")
	harness.Kind = "codex"
	env := envWithout(probeEnv(t), "DEEPSEEK_API_KEY")
	env = setEnv(env, "DEEPSEEK_API_KEY", "test-key")
	readiness := harnessReadiness(context.Background(), harness, env)
	if !readiness.Ready {
		t.Fatalf("readiness = %+v, a configured provider is usable", readiness)
	}
}

func TestHarnessReadinessCodexDoesNotTrustAProviderWithoutItsKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(
		"model_provider = \"deepseek\"\n\n[model_providers.deepseek]\nenv_key = \"DEEPSEEK_API_KEY\"\n",
	), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	harness := fakeProbeHarness(t, "#!/bin/sh\necho 'Not logged in'\nexit 1\n")
	harness.Kind = "codex"
	env := envWithout(probeEnv(t), "DEEPSEEK_API_KEY")
	readiness := harnessReadiness(context.Background(), harness, env)
	if readiness.Ready || !readiness.NeedsSetup {
		t.Fatalf("readiness = %+v, a provider without its key is not ready", readiness)
	}
}

func TestHarnessReadinessCodexWithoutLoginOrProviderNeedsSetup(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	harness := fakeProbeHarness(t, "#!/bin/sh\necho 'Not logged in'\nexit 1\n")
	harness.Kind = "codex"
	readiness := harnessReadiness(context.Background(), harness, probeEnv(t))
	if readiness.Ready || !readiness.NeedsSetup {
		t.Fatalf("readiness = %+v, want setup required", readiness)
	}
	if !strings.Contains(readiness.Fix, "codex login") {
		t.Errorf("fix = %q, want the login command", readiness.Fix)
	}
}

func TestHarnessReadinessCodexUnknownProbeDoesNotBlock(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	harness := fakeProbeHarness(t, "#!/bin/sh\necho 'unknown command: login'\nexit 2\n")
	harness.Kind = "codex"
	readiness := harnessReadiness(context.Background(), harness, probeEnv(t))
	if !readiness.Ready {
		t.Fatalf("readiness = %+v, an inconclusive probe must not block", readiness)
	}
}

func TestHarnessReadinessCodexUnderstandsNotSignedIn(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	harness := fakeProbeHarness(t, "#!/bin/sh\necho 'Not signed in'\nexit 1\n")
	harness.Kind = "codex"
	readiness := harnessReadiness(context.Background(), harness, probeEnv(t))
	if readiness.Ready || !readiness.NeedsSetup {
		t.Fatalf("readiness = %+v, want setup required", readiness)
	}
}

func TestHarnessReadinessCodexAcceptsAnOpenAIKey(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	harness := fakeProbeHarness(t, "#!/bin/sh\necho 'Not logged in'\nexit 1\n")
	harness.Kind = "codex"
	env := setEnv(probeEnv(t), "OPENAI_API_KEY", "test-key")
	readiness := harnessReadiness(context.Background(), harness, env)
	if !readiness.Ready {
		t.Fatalf("readiness = %+v, an API key is usable", readiness)
	}
}

func TestHarnessReadinessCodexAcceptsACommentedProviderSection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(
		"model_provider = \"deep.seek#custom\"\n\n[model_providers.\"deep.seek#custom\"] # custom\nenv_key = \"DEEPSEEK_API_KEY\"\n",
	), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	harness := fakeProbeHarness(t, "#!/bin/sh\necho 'Not logged in'\nexit 1\n")
	harness.Kind = "codex"
	env := setEnv(envWithout(probeEnv(t), "DEEPSEEK_API_KEY"), "DEEPSEEK_API_KEY", "test-key")
	readiness := harnessReadiness(context.Background(), harness, env)
	if !readiness.Ready {
		t.Fatalf("readiness = %+v, want the configured provider", readiness)
	}
}

func TestHarnessReadinessOpencodeReportsNoCredentials(t *testing.T) {
	harness := fakeProbeHarness(t, "#!/bin/sh\necho 'No credentials found'\nexit 1\n")
	harness.Kind = "opencode"
	readiness := harnessReadiness(context.Background(), harness, probeEnv(t))
	if readiness.Ready || !readiness.NeedsSetup {
		t.Fatalf("readiness = %+v, want setup required", readiness)
	}

	harness = fakeProbeHarness(t, "#!/bin/sh\nexit 0\n")
	harness.Kind = "opencode"
	readiness = harnessReadiness(context.Background(), harness, probeEnv(t))
	if readiness.Ready || !readiness.NeedsSetup {
		t.Fatalf("readiness = %+v, a successful empty list means no credentials", readiness)
	}

	harness = fakeProbeHarness(t, "#!/bin/sh\necho 'Error: config is invalid'\nexit 1\n")
	harness.Kind = "opencode"
	readiness = harnessReadiness(context.Background(), harness, probeEnv(t))
	if readiness.Ready || !readiness.NeedsSetup {
		t.Fatalf("readiness = %+v, a failed credential check is not ready", readiness)
	}

	harness = fakeProbeHarness(t, "#!/bin/sh\necho 'unknown command: auth'\nexit 2\n")
	harness.Kind = "opencode"
	readiness = harnessReadiness(context.Background(), harness, probeEnv(t))
	if !readiness.Ready {
		t.Fatalf("readiness = %+v, an unsupported command is inconclusive", readiness)
	}
}

func TestHarnessReadinessForUnknownHarnessDefaultsReady(t *testing.T) {
	for _, kind := range []string{"agy", "pi", "qwen"} {
		harness := fakeProbeHarness(t, "#!/bin/sh\nexit 1\n")
		harness.Kind = kind
		readiness := harnessReadiness(context.Background(), harness, probeEnv(t))
		if !readiness.Ready || readiness.NeedsSetup {
			t.Errorf("%s readiness = %+v, want ready by default", kind, readiness)
		}
	}
}

func TestSetupRequiredOnScreenRecognisesEveryHarnessWizard(t *testing.T) {
	cases := []struct {
		kind string
		text string
	}{
		{"dsh-tui", "Welcome to dsh-TUI · Step 1 of 4"},
		{"claude", "Select login method:\n1. Claude account"},
		{"codex", "Sign in with ChatGPT\nPress Enter to sign in"},
		{"opencode", "Add provider\nSelect provider"},
	}
	for _, tc := range cases {
		reason, fix, required := setupRequiredOnScreen(tc.kind, tc.text)
		if !required {
			t.Errorf("%s: wizard was not detected", tc.kind)
			continue
		}
		if reason == "" || fix == "" {
			t.Errorf("%s: setup notice is incomplete: %q / %q", tc.kind, reason, fix)
		}
	}
}

func TestSetupRequiredOnScreenIgnoresNormalOutputAndOldScrollback(t *testing.T) {
	if _, _, required := setupRequiredOnScreen("dsh-tui", "Session ba447a16\n❯ "); required {
		t.Error("a normal launchpad was treated as setup")
	}
	if _, _, required := setupRequiredOnScreen("pi", "Welcome to dsh-TUI"); required {
		t.Error("an unsupported harness was guessed at")
	}
	old := "Welcome to dsh-TUI · Step 1 of 4\n" + strings.Repeat("normal output\n", 60)
	if _, _, required := setupRequiredOnScreen("dsh-tui", old); required {
		t.Error("wizard text from old scrollback was treated as the current screen")
	}
	recent := "Welcome to dsh-TUI · Step 1 of 4\n" + strings.Repeat("normal output\n", 25)
	if _, _, required := setupRequiredOnScreen("dsh-tui", recent); required {
		t.Error("wizard text 25 lines back was treated as the current screen")
	}
}

func TestSetupRequiredOnScreenFindsAWizardWithoutAKnownAgentKind(t *testing.T) {
	for _, text := range []string{
		"Welcome to dsh-TUI · Step 1 of 4",
		"[dsh-tui] initializing dsh-tui profile...",
	} {
		if _, _, required := setupRequiredOnScreen("", text); !required {
			t.Errorf("wizard without an agent kind was not detected: %q", text)
		}
	}
}

func TestSetupRequiredOnScreenDoesNotMistakeACommandForAWizard(t *testing.T) {
	for _, text := range []string{"opencode auth login", "step 1 of 4"} {
		if _, _, required := setupRequiredOnScreen("", text); required {
			t.Errorf("ordinary shell text was treated as setup: %q", text)
		}
	}
	if _, _, required := setupRequiredOnScreen(
		"dsh-tui", "Here is the plan.\nStep 1 of 4: inspect the parser",
	); required {
		t.Error("a normal answer mentioning Step 1 of 4 was treated as setup")
	}
	if _, _, required := setupRequiredOnScreen(
		"codex", "You can sign in with ChatGPT from the settings page.",
	); required {
		t.Error("a normal answer mentioning ChatGPT sign-in was treated as setup")
	}
}

func TestDshSetupProblemReadsTheProbeEnvironment(t *testing.T) {
	home := t.TempDir()
	env := setEnv(probeEnv(t), "DSH_HOME", home)
	if _, _, missing := dshSetupProblem(env); !missing {
		t.Fatal("an empty DSH_HOME was treated as ready")
	}
	readyDshHomeAt(t, filepath.Join(home, "profiles", "dsh-tui"))
	if _, _, missing := dshSetupProblem(env); missing {
		t.Fatal("a ready DSH_HOME from the probe environment was not seen")
	}
}

func readyDshHomeAt(t *testing.T, profile string) {
	t.Helper()
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatalf("make profile: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(profile, "cordis.patch.yml"), []byte("[]\n"), 0o644,
	); err != nil {
		t.Fatalf("write profile: %v", err)
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
}

func TestHarnessProbeDoesNotWaitForAChildHoldingThePipe(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	harness := fakeProbeHarness(t, "#!/bin/sh\nsleep 10 &\necho 'Not logged in'\nexit 1\n")
	harness.Kind = "codex"
	start := time.Now()
	readiness := harnessReadiness(
		context.Background(), harness, envWithout(probeEnv(t), "DEEPSEEK_API_KEY"),
	)
	if readiness.Ready {
		t.Fatal("a codex without login or provider was reported ready")
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("probe waited %s for a background child", elapsed)
	}
}
