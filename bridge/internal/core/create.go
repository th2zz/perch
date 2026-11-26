package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

type HarnessView struct {
	Kind       string `json:"kind"`
	Executable string `json:"executable"`
	Path       string `json:"path"`
	Ready      bool   `json:"ready"`
	NeedsSetup bool   `json:"needs_setup"`
	Reason     string `json:"reason"`
	Fix        string `json:"fix"`
}

type HarnessesView struct {
	Harnesses  []HarnessView `json:"harnesses"`
	DefaultCwd string        `json:"default_cwd"`
}

type Created struct {
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	PaneID      string `json:"pane_id"`
	Equivalent  string `json:"equivalent"`
}

func (c *Core) Harnesses(ctx context.Context, hostID string) (HarnessesView, error) {
	machine, err := c.hostMachine(ctx, hostID)
	if err != nil {
		return HarnessesView{}, err
	}
	if machine != "" {
		return HarnessesView{}, fmt.Errorf("starting sessions on another computer is not supported yet")
	}
	runtime := c.harnessRuntime(ctx)
	available := herdr.AvailableHarnessesIn(runtime.Path)
	out := make([]HarnessView, len(available))
	var wg sync.WaitGroup
	for i := range available {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			harness := available[i]
			readiness := harnessReadiness(ctx, harness, runtime.Env)
			out[i] = HarnessView{
				Kind:       harness.Kind,
				Executable: harness.Executable,
				Path:       harness.Path,
				Ready:      readiness.Ready,
				NeedsSetup: readiness.NeedsSetup,
				Reason:     readiness.Reason,
				Fix:        readiness.Fix,
			}
		}(i)
	}
	wg.Wait()
	return HarnessesView{Harnesses: out, DefaultCwd: homeDir()}, nil
}

func (c *Core) CreateProject(
	ctx context.Context,
	hostID, label, cwd, kind, name string,
	args []string,
) (*Created, error) {
	machine, err := c.hostMachine(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if machine != "" {
		return nil, fmt.Errorf("starting sessions on another computer is not supported yet")
	}
	cleanLabel, err := herdr.CleanLabel(label)
	if err != nil {
		return nil, err
	}
	cleanCwd, err := projectCwd(cwd)
	if err != nil {
		return nil, err
	}
	runtime := c.harnessRuntime(ctx)
	pathEnv := runtime.Path
	harness, err := herdr.HarnessForIn(pathEnv, kind)
	if err != nil {
		return nil, err
	}
	cleanArgs, err := herdr.CleanAgentArgs(args)
	if err != nil {
		return nil, err
	}
	readiness := harnessReadiness(ctx, harness, runtime.Env)
	if !readiness.Ready {
		return nil, &HarnessSetupError{
			Kind: harness.Kind, Reason: readiness.Reason, Fix: readiness.Fix,
		}
	}
	if herdr.StartsInPane(kind) {
		if err := ensureDshOnboardingIn(homeDirFromEnv(runtime.Env)); err != nil {
			return nil, err
		}
	}
	env := creationEnv(pathEnv, kind)
	snapshot, err := c.api.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not read your computer: %w", err)
	}
	agentName, err := uniqueAgentNameFromSnapshot(snapshot, kind, name)
	if err != nil {
		return nil, err
	}

	workspace, err := c.api.CreateWorkspace(ctx, cleanLabel, cleanCwd, env, false)
	if err != nil {
		return nil, fmt.Errorf("herdr could not create the project: %w", err)
	}
	if err := c.launch(
		ctx, agentName, kind, workspace.Pane.PaneID, harness, cleanArgs,
	); err != nil {
		c.closeWorkspaceAfterFailure(workspace.Workspace.WorkspaceID)
		return nil, fmt.Errorf("herdr could not start %s: %w", kind, err)
	}
	if err := c.confirmStarted(ctx, workspace.Pane.PaneID, kind); err != nil {
		c.closeWorkspaceAfterFailure(workspace.Workspace.WorkspaceID)
		return nil, err
	}
	return &Created{
		WorkspaceID: workspace.Workspace.WorkspaceID,
		TabID:       workspace.Tab.TabID,
		PaneID:      workspace.Pane.PaneID,
		Equivalent: herdr.FormatShell(
			herdr.CreateWorkspaceCommand(cleanCwd, cleanLabel, env, false),
		) + " " +
			herdr.Separator + " " +
			herdr.FormatShell(
				launchCommand(agentName, kind, workspace.Pane.PaneID, harness, cleanArgs),
			),
	}, nil
}

func (c *Core) CreateSession(
	ctx context.Context,
	hostID, workspaceID, kind, name string,
	args []string,
) (*Created, error) {
	machine, err := c.hostMachine(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if machine != "" {
		return nil, fmt.Errorf("starting sessions on another computer is not supported yet")
	}
	if strings.TrimSpace(workspaceID) == "" {
		return nil, fmt.Errorf("which project? a workspace id is required")
	}
	runtime := c.harnessRuntime(ctx)
	pathEnv := runtime.Path
	harness, err := herdr.HarnessForIn(pathEnv, kind)
	if err != nil {
		return nil, err
	}
	cleanArgs, err := herdr.CleanAgentArgs(args)
	if err != nil {
		return nil, err
	}
	readiness := harnessReadiness(ctx, harness, runtime.Env)
	if !readiness.Ready {
		return nil, &HarnessSetupError{
			Kind: harness.Kind, Reason: readiness.Reason, Fix: readiness.Fix,
		}
	}
	if herdr.StartsInPane(kind) {
		if err := ensureDshOnboardingIn(homeDirFromEnv(runtime.Env)); err != nil {
			return nil, err
		}
	}
	env := creationEnv(pathEnv, kind)
	snapshot, err := c.api.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not read your computer: %w", err)
	}
	cwd := ""
	for _, pane := range snapshot.Panes {
		if pane.WorkspaceID != workspaceID {
			continue
		}
		candidate := herdr.Str(pane.Cwd)
		if candidate == "" {
			candidate = herdr.Str(pane.ForegroundCwd)
		}
		// Keep looking when this pane cannot name its folder: a project can have several panes,
		// and the first one in the snapshot is not necessarily the one that knows where the
		// project lives. The old loop stopped at the first match and turned that into a refusal.
		if candidate != "" {
			cwd = candidate
			break
		}
	}
	if cwd == "" {
		return nil, fmt.Errorf(
			"could not find this project's working folder; open the project on your computer and try again",
		)
	}
	resolved, err := resolveFolder(cwd)
	if err != nil {
		return nil, fmt.Errorf("this project's working folder is not usable: %w", err)
	}
	cwd = resolved
	agentName, err := uniqueAgentNameFromSnapshot(snapshot, kind, name)
	if err != nil {
		return nil, err
	}
	tab, err := c.api.CreateTab(ctx, workspaceID, agentName, cwd, env, false)
	if err != nil {
		return nil, fmt.Errorf("herdr could not create the session: %w", err)
	}
	if err := c.launch(
		ctx, agentName, kind, tab.Pane.PaneID, harness, cleanArgs,
	); err != nil {
		c.closePaneAfterFailure(tab.Pane.PaneID)
		return nil, fmt.Errorf("herdr could not start %s: %w", kind, err)
	}
	if err := c.confirmStarted(ctx, tab.Pane.PaneID, kind); err != nil {
		c.closePaneAfterFailure(tab.Pane.PaneID)
		return nil, err
	}
	return &Created{
		WorkspaceID: workspaceID,
		TabID:       tab.Tab.TabID,
		PaneID:      tab.Pane.PaneID,
		Equivalent: herdr.FormatShell(
			herdr.CreateTabCommand(workspaceID, cwd, agentName, env, false),
		) + " " +
			herdr.Separator + " " +
			herdr.FormatShell(
				launchCommand(agentName, kind, tab.Pane.PaneID, harness, cleanArgs),
			),
	}, nil
}

func (c *Core) launch(
	ctx context.Context,
	agentName, kind, paneID string,
	harness herdr.Harness,
	args []string,
) error {
	if !herdr.StartsInPane(kind) {
		_, err := c.api.StartAgent(ctx, agentName, kind, paneID, args, 60*time.Second)
		return err
	}
	command := herdr.QuoteArgs(append([]string{harness.Path}, args...))
	return c.api.SendInput(ctx, paneID, command, []string{"enter"})
}

func launchCommand(
	agentName, kind, paneID string,
	harness herdr.Harness,
	args []string,
) []string {
	if !herdr.StartsInPane(kind) {
		return herdr.StartAgentCommand(agentName, kind, paneID, args)
	}
	command := herdr.QuoteArgs(append([]string{harness.Path}, args...))
	return herdr.RunCommand(paneID, command)
}

func projectCwd(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", fmt.Errorf("a working folder is required; choose one before creating the project")
	}
	return resolveFolder(cwd)
}

const maxPanePaths = 8

type harnessRuntime struct {
	Path string
	Env  []string
}

func (c *Core) harnessRuntime(ctx context.Context) harnessRuntime {
	paths := []string{os.Getenv("PATH")}
	env := os.Environ()
	if os.Getenv(herdr.EnvBinPath) != "" && os.Getenv("PERCH_MANAGED_BY") != "" {
		herdrEnv := herdrProcessEnv()
		env = mergeEnv(env, herdrEnv)
		if path := envValue(herdrEnv, "PATH"); path != "" {
			paths = append(paths, path)
		}
	}
	snapshot, err := c.api.Snapshot(ctx)
	if err == nil {
		focused := focusedPaneID(snapshot)
		seen := map[string]bool{}
		add := func(paneID string) {
			if paneID == "" || seen[paneID] {
				return
			}
			seen[paneID] = true
			processes, err := c.api.PaneProcesses(ctx, paneID)
			if err != nil {
				return
			}
			for _, process := range processes {
				if path := processPath(process.PID); path != "" {
					if paneID == focused {
						paths = append([]string{path}, paths...)
					} else {
						paths = append(paths, path)
					}
					return
				}
			}
		}
		add(focused)
		other := 0
		for _, pane := range snapshot.Panes {
			if other >= maxPanePaths {
				break
			}
			if !seen[pane.PaneID] {
				other++
			}
			add(pane.PaneID)
		}
	}
	pathEnv := mergePathEnv(paths...)
	env = setEnv(env, "PATH", pathEnv)
	return harnessRuntime{Path: pathEnv, Env: env}
}

func mergePathEnv(paths ...string) string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, list := range paths {
		for _, dir := range filepath.SplitList(list) {
			if dir == "" {
				continue
			}
			dir = filepath.Clean(dir)
			if seen[dir] {
				continue
			}
			seen[dir] = true
			out = append(out, dir)
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}

func mergeEnv(base, override []string) []string {
	out := append([]string(nil), base...)
	for _, entry := range override {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		out = setEnv(out, key, strings.TrimPrefix(entry, key+"="))
	}
	return out
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}

func creationEnv(pathEnv, kind string) map[string]string {
	if _, err := herdr.HarnessForIn(os.Getenv("PATH"), kind); err == nil {
		return nil
	}
	return map[string]string{"PATH": pathEnv}
}

func processPath(pid int) string {
	data, err := os.ReadFile(filepath.Join("/proc", fmt.Sprint(pid), "environ"))
	if err != nil {
		return ""
	}
	for _, entry := range strings.Split(string(data), "\x00") {
		if strings.HasPrefix(entry, "PATH=") {
			return strings.TrimPrefix(entry, "PATH=")
		}
	}
	return ""
}

func herdrProcessEnv() []string {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err != nil || strings.TrimSpace(string(comm)) != herdr.Program {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", fmt.Sprint(pid), "environ"))
		if err != nil {
			continue
		}
		entries := strings.Split(string(data), "\x00")
		out := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry != "" {
				out = append(out, entry)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func (c *Core) confirmStarted(ctx context.Context, paneID, kind string) error {
	if herdr.StartsInPane(kind) {
		return c.confirmPaneAgent(ctx, paneID, herdr.DetectedAgent(kind))
	}
	return c.confirmAgent(ctx, paneID, kind)
}

func (c *Core) confirmPaneAgent(ctx context.Context, paneID, agent string) error {
	const hold = 750 * time.Millisecond
	deadline := time.Now().Add(15 * time.Second)
	var presentSince time.Time
	for {
		pane, err := c.api.GetPane(ctx, paneID)
		present := err == nil && pane.Agent != nil && herdr.Str(pane.Agent) == agent
		if present {
			if presentSince.IsZero() {
				presentSince = time.Now()
			} else if time.Since(presentSince) >= hold {
				return nil
			}
		} else if !presentSince.IsZero() {
			return c.paneStartError(
				ctx, paneID, fmt.Errorf("%s stopped before it was ready", agent),
			)
		}
		if time.Now().After(deadline) {
			return c.paneStartError(
				ctx, paneID,
				fmt.Errorf("%s did not start; the CLI profile or plugin may not be ready", agent),
			)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func (c *Core) paneStartError(ctx context.Context, paneID string, cause error) error {
	read, err := c.api.ReadPane(ctx, paneID, "recent", 40)
	if err != nil {
		return cause
	}
	if tail := lastLines(read.Text, 12); tail != "" {
		return fmt.Errorf("%w. The last output was:\n%s", cause, tail)
	}
	return cause
}

func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := strings.TrimSpace(strings.Join(lines, "\n"))
	const max = 2000
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

func dshSetupProblem(env []string) (string, string, bool) {
	base := envValue(env, "DSH_HOME")
	if base == "" {
		base = filepath.Join(homeDirFromEnv(env), ".dsh")
	}
	profile := filepath.Join(base, "profiles", "dsh-tui")
	if _, err := os.Stat(filepath.Join(profile, "cordis.patch.yml")); err != nil {
		return "dsh's CLI profile is not ready.",
			"Run dsh-tui once on your computer to create its profile, then try again.",
			true
	}
	plugin := filepath.Join(
		profile, "node_modules", "@deepseek-harness-tui", "dsh-tui", "package.json",
	)
	if _, err := os.Stat(plugin); err != nil {
		return "dsh's CLI plugin is not installed.",
			"Run: dsh plugin --profile dsh-tui add @deepseek-harness-tui/dsh-tui",
			true
	}
	return "", "", false
}

func dshProfileProblem() error {
	reason, fix, missing := dshSetupProblem(os.Environ())
	if !missing {
		return nil
	}
	return fmt.Errorf("%s %s", reason, fix)
}

// ensureDshOnboarding makes a Perch-launched dsh-tui land in its normal chat
// surface. dsh-tui's four-step first-run wizard is persisted at
// ~/.dsh-tui/onboarding.json; leaving it up makes typed messages look ignored.
// `/setup` reopens the wizard when the person wants it.
func ensureDshOnboarding() error {
	return ensureDshOnboardingIn(homeDir())
}

func ensureDshOnboardingIn(home string) error {
	dir := filepath.Join(home, ".dsh-tui")
	path := filepath.Join(dir, "onboarding.json")
	var current struct {
		Completed bool `json:"completed"`
		Version   int  `json:"version"`
	}
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &current) == nil && current.Completed && current.Version >= 1 {
			return nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("could not prepare dsh's first-run state: %w", err)
	}
	data := []byte("{\n  \"completed\": true,\n  \"version\": 1\n}\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("could not prepare dsh's first-run state: %w", err)
	}
	return nil
}

func (c *Core) confirmAgent(ctx context.Context, paneID, kind string) error {
	const hold = 750 * time.Millisecond
	deadline := time.Now().Add(3 * time.Second)
	var presentSince time.Time
	for {
		pane, err := c.api.GetPane(ctx, paneID)
		present := err == nil && pane.Agent != nil && herdr.Str(pane.Agent) == kind
		if present {
			if presentSince.IsZero() {
				presentSince = time.Now()
			} else if time.Since(presentSince) >= hold {
				return nil
			}
		} else if !presentSince.IsZero() {
			return fmt.Errorf("the harness did not stay running; check the arguments")
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("could not confirm the session started: %w", err)
			}
			return fmt.Errorf("the harness did not stay running; check the arguments")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func (c *Core) closeWorkspaceAfterFailure(workspaceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = c.api.CloseWorkspace(ctx, workspaceID)
}

func (c *Core) closePaneAfterFailure(paneID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = c.api.ClosePane(ctx, paneID)
}

func uniqueAgentNameFromSnapshot(snapshot *herdr.Snapshot, kind, requested string) (string, error) {
	base := agentName(requested)
	if base == "" {
		base = agentName(kind)
	}
	if snapshot == nil {
		return base, nil
	}
	used := map[string]bool{}
	for _, agent := range snapshot.Agents {
		if agent.Name != nil {
			used[*agent.Name] = true
		}
	}
	if !used[base] {
		return base, nil
	}
	for i := 2; i < 1000; i++ {
		candidate := nameWithSuffix(base, i)
		if !used[candidate] {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find an unused session name")
}

func focusedPaneID(snapshot *herdr.Snapshot) string {
	if snapshot == nil {
		return ""
	}
	for _, pane := range snapshot.Panes {
		if pane.Focused {
			return pane.PaneID
		}
	}
	return ""
}

func nameWithSuffix(base string, n int) string {
	suffix := fmt.Sprintf("-%d", n)
	limit := 32 - len(suffix)
	if limit < 1 {
		return fmt.Sprintf("agent-%d", n)
	}
	prefix := base
	if len(prefix) > limit {
		prefix = strings.TrimRight(prefix[:limit], "-_")
	}
	if prefix == "" {
		prefix = "agent"
	}
	return prefix + suffix
}

func agentName(requested string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(requested)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return ""
	}
	if out[0] < 'a' || out[0] > 'z' {
		out = "agent-" + out
	}
	if len(out) > 32 {
		out = strings.TrimRight(out[:32], "-_")
	}
	return out
}
