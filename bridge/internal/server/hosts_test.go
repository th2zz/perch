package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/th2zz/perch/bridge/internal/testherdr"
)

// The bridge's HTTP surface for a second computer (spec FR-016).
//
// These run against the shim in internal/testherdr, which is the test binary
// pretending to be `herdr`: the bridge reaches other machines by running that
// binary, so this is the only way to exercise the real code path — the argv it
// builds, the JSON it parses, and what it does or does not run — without a second
// computer on the network.
func remoteFixture(t *testing.T, fail string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("machines.json", `[{"id":"aaa","label":"workbox","target":"workbox","session":"workbox","enabled":true}]`)
	write("snapshot-workbox.json", `{"id":"cli:api:snapshot","result":{"type":"session_snapshot","snapshot":{
  "version":"0.9.1","protocol":22,
  "workspaces":[{"workspace_id":"w1","number":1,"label":"perch","focused":true,"pane_count":1,"tab_count":1,"active_tab_id":"w1:t1","agent_status":"blocked"}],
  "tabs":[],
  "panes":[{"pane_id":"w1:p1","terminal_id":"term_x","workspace_id":"w1","tab_id":"w1:t1","focused":true,"agent_status":"blocked","revision":3}],
  "agents":[]}}}`)
	write("panetext-workbox.txt", "what now?\n")
	write("explain-workbox.txt", "agent: codex\nstate: blocked\nrule: live_strong_blocker\n")
	if fail != "" {
		write("fail-"+fail, "")
	}
	testherdr.Install(t, dir)
	return dir
}

func ranCalls(dir, needle string) []string {
	var out []string
	for _, call := range testherdr.Calls(dir) {
		if strings.Contains(call, needle) {
			out = append(out, call)
		}
	}
	return out
}

// TestTextDeliveryCanBeAddressedToAnotherComputer: the escape hatch's write goes
// to the machine the session is on, as the same command line the app shows.
func TestTextDeliveryCanBeAddressedToAnotherComputer(t *testing.T) {
	dir := remoteFixture(t, "")
	ts, _ := newTestServer(t)

	status, equivalent, body := postCommand(t, ts, "/v1/decisions",
		`{"kind":"prompt","target":"w1:p1","text":"npm test","host":"workbox"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%v), want 200", status, body)
	}
	// Read it the way the app does: the equivalent command is a list of
	// arguments, not a sentence.
	want := "herdr pane send-text w1:p1 npm test"
	if got := strings.Join(equivalent, " "); got != want {
		t.Errorf("equivalent_command = %q, want %q", got, want)
	}
	calls := ranCalls(dir, "pane send-text")
	if len(calls) != 1 {
		t.Fatalf("remote writes = %v, want exactly one", calls)
	}
	if !strings.HasPrefix(calls[0], "--machine workbox pane send-text w1:p1 npm test") {
		t.Errorf("the write was not addressed to the machine: %q", calls[0])
	}
}

// TestAWriteToAMachineNobodySavedIsRefusedAndRunsNothing: naming a machine that is
// not saved must not fall back to the local one. Sending a keystroke to the wrong
// computer's pane is the failure this whole feature has to avoid.
func TestAWriteToAMachineNobodySavedIsRefusedAndRunsNothing(t *testing.T) {
	dir := remoteFixture(t, "")
	ts, _ := newTestServer(t)

	status, body := call(t, ts, http.MethodPost, "/v1/decisions", testToken,
		`{"kind":"prompt","target":"w1:p1","text":"npm test","host":"typo-machine"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d (%v), want 400", status, body)
	}
	if !strings.Contains(body["error"], "typo-machine") {
		t.Errorf("the refusal does not name the machine: %v", body)
	}
	if body["fix"] == "" {
		t.Error("a refusal with no next step (FR-015)")
	}
	for _, call := range testherdr.Calls(dir) {
		if strings.Contains(call, "send-text") || strings.Contains(call, "send-keys") {
			t.Errorf("a refused write still ran something: %q", call)
		}
	}
}

// TestTheEscapeHatchCanReadAndPressOnAnotherComputer: reading a session and
// pressing a key both have to work on a remote machine, or the fallback this
// product leans on only exists on one computer.
func TestTheEscapeHatchCanReadAndPressOnAnotherComputer(t *testing.T) {
	dir := remoteFixture(t, "")
	ts, core := newTestServer(t)

	// The poller is what fills the host's inventory; in the daemon it runs on its
	// own, here it is driven directly.
	core.RefreshRemoteHosts(t.Context())

	sessionStatus, sessionBody := call(t, ts, http.MethodGet,
		"/v1/session?host=workbox&pane_id=w1:p1&lines=5", testToken, "")
	if sessionStatus != http.StatusOK {
		t.Fatalf("session status = %d (%v), want 200", sessionStatus, sessionBody)
	}

	status, equivalent, body := postCommand(t, ts, "/v1/session/keys",
		`{"host":"workbox","pane_id":"w1:p1","keys":["esc"]}`)
	if status != http.StatusOK {
		t.Fatalf("keys status = %d (%v), want 200", status, body)
	}
	if got := strings.Join(equivalent, " "); got != "herdr pane send-keys w1:p1 esc" {
		t.Errorf("equivalent_command = %q", got)
	}
	calls := ranCalls(dir, "--machine workbox pane send-keys")
	if len(calls) != 1 {
		t.Fatalf("remote key presses = %v, want exactly one", calls)
	}
}

// TestAnotherComputerThatIsAsleepIsReportedNotHidden: the app has to be able to
// say why a machine is missing from the list, and the local machine has to keep
// working while it is missing.
func TestAnotherComputerThatIsAsleepIsReportedNotHidden(t *testing.T) {
	remoteFixture(t, "workbox")
	// A working local machine, so the only thing under test is what an unreachable
	// *other* machine does to the view.
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent_status": "idle", "revision": 1,
	}
	_, socketPath := startFakeHerdr(t, pane)
	ts, core := newTestServerWithSocket(t, socketPath)
	core.RefreshRemoteHosts(t.Context())

	// RefreshOnce reads the local session and rebuilds the mirror from every host.
	if _, err := core.RefreshOnce(t.Context()); err != nil {
		t.Fatalf("local read: %v", err)
	}
	raw := getJSON(t, ts, "/v1/snapshot", testToken)
	if !strings.Contains(string(raw), `"host_id":"workbox"`) {
		t.Fatalf("the unreachable machine is missing from the mirror: %s", truncate(string(raw), 400))
	}
	if !strings.Contains(string(raw), `"status":"unreachable"`) {
		t.Errorf("the mirror does not say the machine is unreachable: %s", truncate(string(raw), 400))
	}
	if !strings.Contains(string(raw), `"fix":"`) {
		t.Error("an unreachable machine with no next step (FR-015)")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// postCommand posts a body and lifts both the status and the equivalent command.
// The package's `call` helper only lifts string fields, and a list is not one.
func postCommand(
	t *testing.T,
	ts *httptest.Server,
	path, body string,
) (int, []string, map[string]string) {
	t.Helper()
	resp := do(t, ts, http.MethodPost, path, testToken, body)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	fields := map[string]string{}
	var generic map[string]any
	if json.Unmarshal(raw, &generic) == nil {
		for key, value := range generic {
			if text, ok := value.(string); ok {
				fields[key] = text
			}
		}
	}
	var decoded struct {
		EquivalentCommand []string `json:"equivalent_command"`
	}
	_ = json.Unmarshal(raw, &decoded)
	return resp.StatusCode, decoded.EquivalentCommand, fields
}

// TestMain lets this package's tests stand in for herdr, so the remote path can be
// exercised without a second computer. See internal/testherdr.
func TestMain(m *testing.M) {
	testherdr.MaybeActAsHerdr()
	os.Exit(m.Run())
}
