package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
	"github.com/th2zz/perch/bridge/internal/state"
)

// cmdEnsureRunning is what herdr's [[startup]] hook calls.
//
// A herdr startup hook is a one-shot initialization command, not a supervised
// daemon (lab/bridge-probe; docs: "one-shot initialization commands rather than
// supervised daemons"). So this command does the only thing a one-shot can do
// correctly: make sure the supervised process exists. Where a service manager
// is available it delegates to it; otherwise it detaches a copy of itself.
//
// It always exits 0 when the daemon ends up running, so a herdr restart loop
// cannot turn into a bridge restart storm.
func cmdEnsureRunning(ctx context.Context, args []string) error {
	c := mustCore(true)

	if pid, running := c.Store().RunningPID(); running {
		fmt.Printf("perch bridge already running (pid %d)\n", pid)
		return nil
	}

	if unitInstalled() {
		if err := runCommand("systemctl", "--user", "start", serviceName); err == nil {
			fmt.Println("perch bridge started via systemd")
			return nil
		}
	}

	if err := startDetached(c); err != nil {
		return err
	}
	fmt.Println("perch bridge started")
	return nil
}

// startDetached launches `perch-bridge run` in its own session so it outlives
// the hook process. Output goes to a log file inside the state directory, which
// disappears when the plugin is unlinked (constitution III: zero residue).
func startDetached(c interface{ Store() *state.Store }) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := c.Store().Path("bridge.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()

	cmd := exec.Command(exe, "run")
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// Our output already lands in the log file, so the child must not open it too:
	// every line would appear twice.
	cmd.Env = append(os.Environ(), state.EnvLogRedirected+"=1")
	if err := cmd.Start(); err != nil {
		return err
	}
	// Detach: we deliberately do not Wait, the child is a daemon now.
	_ = cmd.Process.Release()
	return nil
}

// cmdOnEvent is the plugin's [[events]] hook. It must stay cheap: herdr spawns
// one process per event, so this writes a line and returns.
//
// The daemon holds its own socket subscription, so this path is a redundancy
// rather than the main channel. It records the payload so a bridge that was
// restarting does not lose the events that arrived while it was down.
func cmdOnEvent(ctx context.Context, args []string) error {
	c := mustCore(true)
	payload := os.Getenv(herdr.EnvPluginEventJSON)
	kind := os.Getenv(herdr.EnvPluginEvent)
	if payload == "" && kind == "" {
		return nil
	}
	record := struct {
		At      time.Time       `json:"at"`
		Kind    string          `json:"kind"`
		Payload json.RawMessage `json:"payload,omitempty"`
	}{
		At:   time.Now(),
		Kind: kind,
	}
	if payload != "" && json.Valid([]byte(payload)) {
		record.Payload = json.RawMessage(payload)
	}
	line, err := json.Marshal(record)
	if err != nil {
		// A hook process must never fail loudly: herdr spawns it per event, and
		// an error here would surface in the user's herdr log rather than
		// anywhere useful. The daemon's own socket subscription is the primary
		// path, so dropping one redundant record loses nothing.
		//nolint:nilerr // deliberate: see above
		return nil
	}
	return c.Store().AppendLine(state.FileEventLog, string(line))
}

// cmdLogs shows recent bridge activity without requiring journalctl access.
func cmdLogs(ctx context.Context, args []string) error {
	c := mustCore(true)
	fmt.Printf("state dir: %s\n", c.Store().Dir())
	fmt.Printf("log file : %s\n\n", c.Store().Path("bridge.log"))

	if unitInstalled() {
		out, err := exec.Command("systemctl", "--user", "status", serviceName, "--lines=15", "--no-pager").CombinedOutput()
		if err == nil || len(out) > 0 {
			fmt.Println(strings.TrimRight(string(out), "\n"))
			fmt.Println()
		}
	}

	// The bridge's own log, which is the only place a failure leaves a trace on
	// platforms where the journal is not running (a WSL distribution without
	// systemd-journald is the common one). Printing the path and not the content was
	// the old behaviour: the command was named "logs" and showed everything except
	// the log.
	if file, err := os.Open(c.Store().Path(state.FileLog)); err == nil {
		fmt.Println("bridge log (most recent lines):")
		lines, _ := tailLines(file, 20)
		_ = file.Close()
		if len(lines) == 0 {
			fmt.Println("  (empty)")
		}
		for _, line := range lines {
			fmt.Println("  " + line)
		}
		fmt.Println()
	}

	f, err := os.Open(c.Store().Path(state.FileEventLog))
	if err != nil {
		fmt.Println("no herdr events recorded yet")
		return nil
	}
	defer func() { _ = f.Close() }()

	// Keep the last 20 lines: the log is a ring of whatever herdr sent us.
	const keep = 20
	ring := make([]string, 0, keep)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		ring = append(ring, scanner.Text())
		if len(ring) > keep {
			ring = ring[1:]
		}
	}
	fmt.Printf("last %d herdr events:\n", len(ring))
	for _, line := range ring {
		fmt.Println("  " + line)
	}
	return nil
}

// tailLines returns the last n lines of a log, reading the whole file because these
// logs are bounded by the rotation in internal/state/logging.go.
func tailLines(r io.Reader, n int) ([]string, error) {
	ring := make([]string, 0, n)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		ring = append(ring, scanner.Text())
		if len(ring) > n {
			ring = ring[1:]
		}
	}
	return ring, scanner.Err()
}

func runCommand(name string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args...)
	// Not plain os.Environ(): a user-session tool run over ssh needs
	// XDG_RUNTIME_DIR filled in, or it cannot find the user manager at all. See
	// session_env.go.
	cmd.Env = userSessionEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(name + ": " + strings.TrimSpace(string(out)))
	}
	return nil
}

// runCommandQuiet is runCommand for a probe rather than an action.
//
// The difference matters when the tool is missing: "systemctl is not installed" is
// an answer to "is it running?" (no), and it should not print a shell error on the
// way to saying so.
func runCommandQuiet(name string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args...)
	cmd.Env = userSessionEnv()
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}
