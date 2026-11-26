// Package testherdr is a fake `herdr` executable for tests.
//
// The bridge reaches other machines by running the user's own herdr binary
// (`herdr --machine <label> …`, see internal/herdr/remote.go). That is deliberate
// — reusing herdr's SSH plumbing is the whole reason a second computer works at
// all — but it means a test cannot reach the `--machine` path without either a
// second computer or a stand-in.
//
// So the stand-in is the test binary itself: call MaybeActAsHerdr from TestMain
// and, when the bridge spawns it as `herdr`, it answers from a directory of
// fixtures and records every invocation. What that buys is exactly what the real
// thing costs: the argv the bridge builds, the JSON it parses, and the promise
// that a refused action writes nothing.
//
// The shapes it answers with are not invented: they were captured from herdr
// 0.9.1 on 2026-10-13 (`herdr machine list --json`, `herdr --machine X api
// snapshot`, `pane read --format text`, `agent explain`).
package testherdr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// EnvShim marks a process that should behave as herdr rather than as the test.
	EnvShim = "PERCH_TEST_HERDR_SHIM"
	// EnvDir points at the fixture directory the shim reads and writes.
	EnvDir = "PERCH_TEST_HERDR_DIR"
)

// MaybeActAsHerdr takes over the process when it was started as the fake binary.
// It never returns in that case.
func MaybeActAsHerdr() {
	if os.Getenv(EnvShim) != "1" {
		return
	}
	dir := os.Getenv(EnvDir)
	args := os.Args[1:]
	record(dir, args)

	// A machine can be marked as broken, which is how the "other computer is
	// asleep" path is exercised without unplugging anything.
	if label, ok := machineOf(args); ok {
		if _, err := os.Stat(filepath.Join(dir, "fail-"+label)); err == nil {
			fmt.Fprintf(os.Stderr, "ssh: connect to host %s port 22: No route to host\n", label)
			os.Exit(255)
		}
	}

	label, remote := machineOf(args)
	rest := args
	if remote {
		rest = args[2:]
	}

	switch {
	case len(rest) >= 2 && rest[0] == "machine" && rest[1] == "list":
		emitFile(dir, "machines.json", "[]\n")
	case remote && len(rest) >= 2 && rest[0] == "api" && rest[1] == "snapshot":
		emitFile(dir, "snapshot-"+label+".json", emptySnapshot)
	case remote && len(rest) >= 2 && rest[0] == "pane" && rest[1] == "read":
		emitFile(dir, "panetext-"+label+".txt", "")
	case remote && len(rest) >= 2 && rest[0] == "agent" && rest[1] == "explain":
		emitFile(dir, "explain-"+label+".txt", "")
	// The local machine's detection rule, for tests that exercise the socket path but
	// still need `agent explain` to answer like the real CLI.
	case !remote && len(rest) >= 2 && rest[0] == "agent" && rest[1] == "explain":
		emitFile(dir, "explain-local.txt", "")
	case !remote && len(rest) >= 1 && rest[0] == "status":
		emitFile(dir, "status.txt", "client:\n  version: 0.9.1\n  protocol: 22\n")
	default:
		// Every other invocation is a write or a query the test does not care
		// about: succeed silently, because the point is the recorded argv.
	}
	os.Exit(0)
}

// emptySnapshot is the shape herdr answers with, with nothing in it. A test that
// wants panes writes its own snapshot file into the fixture directory.
const emptySnapshot = `{"id":"cli:api:snapshot","result":{"type":"session_snapshot",` +
	`"snapshot":{"version":"0.9.1","protocol":22,"workspaces":[],"tabs":[],"panes":[],"agents":[]}}}` + "\n"

// machineOf reports the machine label an invocation addresses, if any.
func machineOf(args []string) (string, bool) {
	if len(args) >= 2 && args[0] == "--machine" {
		return args[1], true
	}
	return "", false
}

func emitFile(dir, name, fallback string) {
	path := filepath.Join(dir, name)
	body, err := os.ReadFile(path)
	if err != nil {
		body = []byte(fallback)
	}
	_, _ = os.Stdout.Write(body)
}

// record appends the invocation so a test can assert what was run. One line per
// call, arguments joined: enough to answer "did anything get written" and "was it
// addressed to the right machine".
func record(dir string, args []string) {
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(filepath.Join(dir, "calls.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	_, _ = file.WriteString(strings.Join(args, " ") + "\n")
}

// Calls reads the recorded invocations. Tests use it to assert that a refused
// action ran nothing, and that an accepted one addressed the right machine.
func Calls(dir string) []string {
	body, err := os.ReadFile(filepath.Join(dir, "calls.log"))
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// Install points HERDR_BIN_PATH at the test binary and returns the fixture
// directory the shim will use.
func Install(t interface{ Fatalf(string, ...any) }, dir string) {
	set := func(key, value string) {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
	set(EnvShim, "1")
	set(EnvDir, dir)
	set("HERDR_BIN_PATH", os.Args[0])
}
