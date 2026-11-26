package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/th2zz/perch/bridge/internal/herdr"
)

type HarnessReadiness struct {
	Ready      bool   `json:"ready"`
	NeedsSetup bool   `json:"needs_setup"`
	Reason     string `json:"reason,omitempty"`
	Fix        string `json:"fix,omitempty"`
}

type HarnessSetupError struct {
	Kind   string
	Reason string
	Fix    string
}

func (e *HarnessSetupError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("%s needs setup before Perch can start it", e.Kind)
	}
	return e.Reason
}

func (e *HarnessSetupError) FixText() string {
	return e.Fix
}

func readyHarness() HarnessReadiness {
	return HarnessReadiness{Ready: true}
}

func setupNeeded(reason, fix string) HarnessReadiness {
	return HarnessReadiness{NeedsSetup: true, Reason: reason, Fix: fix}
}

const harnessProbeTimeout = 5 * time.Second

func harnessReadiness(
	ctx context.Context,
	harness herdr.Harness,
	env []string,
) HarnessReadiness {
	switch harness.Kind {
	case "dsh-tui":
		return dshReadiness(ctx, harness, env)
	case "claude":
		return claudeReadiness(ctx, harness, env)
	case "codex":
		return codexReadiness(ctx, harness, env)
	case "opencode":
		return opencodeReadiness(ctx, harness, env)
	default:
		return readyHarness()
	}
}

func runHarnessProbe(
	ctx context.Context,
	harness herdr.Harness,
	env []string,
	args ...string,
) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, harnessProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, harness.Path, args...)
	cmd.Env = env
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func probeTail(out string, err error) string {
	if out != "" {
		return lastLines(out, 6)
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func dshReadiness(
	ctx context.Context,
	harness herdr.Harness,
	env []string,
) HarnessReadiness {
	if reason, fix, missing := dshSetupProblem(env); missing {
		return setupNeeded(reason, fix)
	}
	out, err := runHarnessProbe(ctx, harness, env, "doctor")
	if err != nil {
		detail := probeTail(out, err)
		reason := "dsh-tui is not ready."
		if detail != "" {
			reason += " " + detail
		}
		return setupNeeded(
			reason,
			"Run `dsh-tui doctor` on your computer and fix the lines marked ✗, then try again.",
		)
	}
	if strings.Contains(out, "✗") || strings.Contains(out, "✘") {
		return setupNeeded(
			"dsh-tui's startup checks are not all green.",
			"Run `dsh-tui doctor` on your computer and fix the lines marked ✗, then try again.",
		)
	}
	return readyHarness()
}

func claudeReadiness(
	ctx context.Context,
	harness herdr.Harness,
	env []string,
) HarnessReadiness {
	out, _ := runHarnessProbe(ctx, harness, env, "auth", "status")
	var status struct {
		LoggedIn     *bool  `json:"loggedIn"`
		AuthMethod   string `json:"authMethod"`
		APIKeySource string `json:"apiKeySource"`
	}
	if json.Unmarshal([]byte(out), &status) != nil {
		return readyHarness()
	}
	if status.LoggedIn != nil && !*status.LoggedIn &&
		status.AuthMethod == "" && status.APIKeySource == "" {
		return setupNeeded(
			"Claude Code is not signed in.",
			"Open Claude Code on your computer and sign in, or set ANTHROPIC_API_KEY, then try again.",
		)
	}
	return readyHarness()
}

func codexReadiness(
	ctx context.Context,
	harness herdr.Harness,
	env []string,
) HarnessReadiness {
	out, _ := runHarnessProbe(ctx, harness, env, "login", "status")
	lower := strings.ToLower(out)
	negative := strings.Contains(lower, "not logged in") ||
		strings.Contains(lower, "not signed in") ||
		strings.Contains(lower, "signed out") ||
		strings.Contains(lower, "logged out") ||
		strings.Contains(lower, "not authenticated") ||
		strings.Contains(lower, "sign in required") ||
		strings.Contains(lower, "login required") ||
		strings.Contains(lower, "authentication required")
	if !negative {
		return readyHarness()
	}
	if codexProviderReady(env) {
		return readyHarness()
	}
	return setupNeeded(
		"Codex is not signed in.",
		"Run `codex login` on your computer, or configure a model provider with an API key in ~/.codex/config.toml, then try again.",
	)
}

func opencodeReadiness(
	ctx context.Context,
	harness herdr.Harness,
	env []string,
) HarnessReadiness {
	out, err := runHarnessProbe(ctx, harness, env, "auth", "list")
	lower := strings.ToLower(out)
	if strings.TrimSpace(out) == "" {
		if err == nil {
			return setupNeeded(
				"OpenCode has no sign-in configured.",
				"Run `opencode auth login` on your computer, then try again.",
			)
		}
		return readyHarness()
	}
	if err != nil {
		if strings.Contains(lower, "unknown command") ||
			strings.Contains(lower, "unrecognized") {
			return readyHarness()
		}
		return setupNeeded(
			"OpenCode's credential check failed.",
			"Run `opencode auth login` on your computer, then try again.",
		)
	}
	if strings.Contains(lower, "no credentials") ||
		strings.Contains(lower, "no providers") ||
		strings.Contains(lower, "not authenticated") ||
		strings.Contains(lower, "not logged in") {
		return setupNeeded(
			"OpenCode has no sign-in configured.",
			"Run `opencode auth login` on your computer, then try again.",
		)
	}
	return readyHarness()
}

func envHas(env []string, key string) bool {
	return envValue(env, key) != ""
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) && len(entry) > len(prefix) {
			return entry[len(prefix):]
		}
	}
	return ""
}

func homeDirFromEnv(env []string) string {
	if home := envValue(env, "HOME"); home != "" {
		return home
	}
	return homeDir()
}

func codexProviderReady(env []string) bool {
	if envHas(env, "OPENAI_API_KEY") {
		return true
	}
	base := envValue(env, "CODEX_HOME")
	if base == "" {
		base = filepath.Join(homeDirFromEnv(env), ".codex")
	}
	var config struct {
		ModelProvider  string `toml:"model_provider"`
		ModelProviders map[string]struct {
			EnvKey  string `toml:"env_key"`
			BaseURL string `toml:"base_url"`
		} `toml:"model_providers"`
	}
	if _, err := toml.DecodeFile(filepath.Join(base, "config.toml"), &config); err != nil {
		return false
	}
	if config.ModelProvider == "" || config.ModelProvider == "openai" {
		return false
	}
	provider, ok := config.ModelProviders[config.ModelProvider]
	if !ok {
		return false
	}
	if provider.EnvKey != "" {
		return envHas(env, provider.EnvKey)
	}
	return provider.BaseURL != ""
}

type setupMarker struct {
	markers []string
	all     []string
	reason  string
	fix     string
}

var harnessSetupMarkers = map[string]setupMarker{
	"dsh-tui": {
		markers: []string{
			"welcome to dsh-tui", "initializing dsh-tui profile",
			"esc skip the guide",
		},
		reason: "dsh-tui is waiting in its first-run setup wizard.",
		fix:    "Finish the setup steps in the terminal, then try sending again.",
	},
	"claude": {
		markers: []string{"select login method"},
		reason:  "Claude Code is waiting for you to sign in.",
		fix:     "Finish signing in in the terminal, then try sending again.",
	},
	"codex": {
		all:    []string{"sign in with chatgpt", "press enter to sign in"},
		reason: "Codex is waiting for you to sign in.",
		fix:    "Finish signing in in the terminal, then try sending again.",
	},
	"opencode": {
		markers: []string{"sign in to opencode"},
		all:     []string{"add provider", "select provider"},
		reason:  "OpenCode is waiting for you to choose a provider and sign in.",
		fix:     "Finish signing in in the terminal, then try sending again.",
	},
}

var genericSetupMarkers = []struct {
	kind    string
	markers []string
	all     []string
}{
	{"dsh-tui", []string{
		"welcome to dsh-tui", "initializing dsh-tui profile",
		"esc skip the guide",
	}, nil},
	{"claude", []string{"select login method"}, nil},
	{"codex", nil, []string{"sign in with chatgpt", "press enter to sign in"}},
	{"opencode", []string{"sign in to opencode"}, nil},
}

func setupRequiredOnScreen(kind, text string) (string, string, bool) {
	if kind == "" {
		for _, generic := range genericSetupMarkers {
			marker := harnessSetupMarkers[generic.kind]
			marker.markers = generic.markers
			marker.all = generic.all
			if reason, fix, ok := matchesSetupMarker(marker, text); ok {
				return reason, fix, true
			}
		}
		return "", "", false
	}
	marker, ok := harnessSetupMarkers[kind]
	if !ok {
		return "", "", false
	}
	return matchesSetupMarker(marker, text)
}

func matchesSetupMarker(marker setupMarker, text string) (string, string, bool) {
	tail := strings.ToLower(lastScreenLines(text, 20))
	for _, candidate := range marker.markers {
		if strings.Contains(tail, candidate) {
			return marker.reason, marker.fix, true
		}
	}
	if len(marker.all) > 0 {
		for _, candidate := range marker.all {
			if !strings.Contains(tail, candidate) {
				return "", "", false
			}
		}
		return marker.reason, marker.fix, true
	}
	return "", "", false
}

func (c *Core) setupRequiredAt(
	ctx context.Context,
	machine, paneID, kind string,
) (string, string, bool, error) {
	var text string
	if machine == "" {
		read, err := c.api.ReadPane(ctx, paneID, "recent", 40)
		if err != nil {
			read, err = c.api.ReadPane(ctx, paneID, "visible", 0)
			if err != nil {
				return "", "", false, err
			}
		}
		text = read.Text
	} else {
		read, err := c.cli.ReadPaneTextAt(ctx, machine, paneID, 40)
		if err != nil {
			return "", "", false, err
		}
		text = read
	}
	if strings.TrimSpace(text) == "" {
		return "", "", false, fmt.Errorf("the session screen is empty")
	}
	reason, fix, required := setupRequiredOnScreen(kind, text)
	return reason, fix, required, nil
}

func lastScreenLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
