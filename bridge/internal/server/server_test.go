package server

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/th2zz/perch/bridge/internal/core"
	"github.com/th2zz/perch/bridge/internal/herdr"
	"github.com/th2zz/perch/bridge/internal/state"
	"github.com/th2zz/perch/bridge/internal/testherdr"
)

const testToken = "test-token-that-is-long-enough"

// newTestServer builds a real server over a state directory of its own and a
// socket path that does not exist, so every herdr-dependent path fails honestly
// instead of reaching the developer's live session.
func newTestServer(t *testing.T) (*httptest.Server, *core.Core) {
	t.Helper()
	return newTestServerWithSocket(t, filepath.Join(t.TempDir(), "absent.sock"))
}

// newTestServerWithSocket points the bridge at a given socket path, so a test can
// stand up a fake herdr and exercise the whole request/response chain hermetically
// — no developer's live session, and no way to type into a real pane by accident.
func newTestServerWithSocket(t *testing.T, socketPath string) (*httptest.Server, *core.Core) {
	t.Helper()
	ts, c, _ := newTestServerWithSocketAndLogs(t, socketPath)
	return ts, c
}

// newTestServerWithSocketAndLogs is newTestServerWithSocket with the bridge's log kept,
// so a test can ask what a person reading bridge.log would have seen.
func newTestServerWithSocketAndLogs(t *testing.T, socketPath string) (*httptest.Server, *core.Core, *syncBuffer) {
	t.Helper()
	t.Setenv(state.EnvStateDir, t.TempDir())
	t.Setenv("HERDR_SOCKET_PATH", socketPath)

	c, err := core.New(core.Options{DisableSubscriptions: true})
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	logs := &syncBuffer{}
	srv, err := New(c, Options{
		Addr: "127.0.0.1:0", Token: testToken, Version: "test",
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	// Registered after the temporary directories, so it runs before them (cleanups are last-in,
	// first-out) and waits for the background work a request may have started. Without this a test
	// could end while a follow-up read was still writing its cache to disk, and Go's removal of the
	// directory would fail with "directory not empty" — a flake with no visible cause.
	t.Cleanup(c.Close)
	return ts, c, logs
}

// syncBuffer is a buffer the bridge can write its log into while the test reads it:
// handlers log from their own goroutines, and the race detector is watching.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// waitForLog waits for a line the bridge writes from another goroutine.
func waitForLog(t *testing.T, logs *syncBuffer, substr string) {
	t.Helper()
	waitForLogCount(t, logs, substr, 1)
}

// waitForLogCount waits until the log holds a substring n times, for the tests that
// open more than one stream and need to say which stream they are talking about.
func waitForLogCount(t *testing.T, logs *syncBuffer, substr string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(logs.String(), substr) >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the log never said %q %d times; it said:\n%s", substr, n, logs.String())
}

// fakeHerdr is a socket that answers like the real one and remembers what it was
// asked to do, which is how a write can be verified without a real pane.
type fakeHerdr struct {
	mu                sync.Mutex
	calls             []map[string]any
	fails             map[string]map[string]string
	delays            map[string]time.Duration
	agents            map[string]string
	suppressAgent     bool
	processInfo       map[string]any
	processInfoByPane map[string]map[string]any
	busyAgentStarts   int
	agentLifetime     time.Duration
	// clients are the connections the fake is still serving, so cleanup can end them *before* the
	// test's temporary directory is removed. A handler mid-write while Go removes that directory is
	// what produced the occasional "TempDir RemoveAll cleanup: directory not empty" — a flake that
	// sat in the release checklist's single command, which is the worst place for one: a summary
	// that is sometimes red for no reason teaches people to ignore it.
	clients []net.Conn
}

func (f *fakeHerdr) fail(method, code, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fails == nil {
		f.fails = map[string]map[string]string{}
	}
	f.fails[method] = map[string]string{"code": code, "message": message}
}

func (f *fakeHerdr) failure(method string) (map[string]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	failure, ok := f.fails[method]
	return failure, ok
}

func (f *fakeHerdr) delay(method string, duration time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.delays == nil {
		f.delays = map[string]time.Duration{}
	}
	f.delays[method] = duration
}

func (f *fakeHerdr) delayFor(method string) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.delays[method]
}

func (f *fakeHerdr) setAgent(paneID, kind string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.agents == nil {
		f.agents = map[string]string{}
	}
	f.agents[paneID] = kind
}

func (f *fakeHerdr) agentFor(paneID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.agents[paneID]
}

func (f *fakeHerdr) exitAgentAfter(duration time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agentLifetime = duration
}

func (f *fakeHerdr) agentLifetimeFor() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.agentLifetime
}

func (f *fakeHerdr) clearAgent(paneID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.agents, paneID)
}

func (f *fakeHerdr) suppressAgentStart() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suppressAgent = true
}

func (f *fakeHerdr) suppressingAgent() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.suppressAgent
}

func (f *fakeHerdr) setProcessInfo(info map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.processInfo = info
}

func (f *fakeHerdr) setProcessInfoFor(paneID string, info map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.processInfoByPane == nil {
		f.processInfoByPane = map[string]map[string]any{}
	}
	f.processInfoByPane[paneID] = info
}

func (f *fakeHerdr) process(paneID string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if info := f.processInfoByPane[paneID]; info != nil {
		out := cloneMap(info)
		out["pane_id"] = paneID
		return out
	}
	if f.processInfo == nil {
		return nil
	}
	out := cloneMap(f.processInfo)
	out["pane_id"] = paneID
	return out
}

func (f *fakeHerdr) busyAgentStart(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.busyAgentStarts = n
}

func (f *fakeHerdr) takeBusyAgentStart() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.busyAgentStarts <= 0 {
		return false
	}
	f.busyAgentStarts--
	return true
}

func (f *fakeHerdr) clearBusyAgentStarts() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.busyAgentStarts = 0
}

func (f *fakeHerdr) add(conn net.Conn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clients = append(f.clients, conn)
}

// closeClients ends every connection the fake is serving.
func (f *fakeHerdr) closeClients() {
	f.mu.Lock()
	clients := f.clients
	f.clients = nil
	f.mu.Unlock()
	for _, conn := range clients {
		_ = conn.Close()
	}
}

func (f *fakeHerdr) record(call map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeHerdr) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, call := range f.calls {
		if method, ok := call["method"].(string); ok {
			out = append(out, method)
		}
	}
	return out
}

func (f *fakeHerdr) paramsFor(method string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if call["method"] == method {
			if params, ok := call["params"].(map[string]any); ok {
				return params
			}
		}
	}
	return nil
}

// startFakeHerdr serves the socket and returns it with its path.
// Variadic so that a test with one pane reads as it always did, and a test that needs
// scale (forty projects, two hundred sessions) can hand over a list.
func startFakeHerdr(t *testing.T, panes ...map[string]any) (*fakeHerdr, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	fake := &fakeHerdr{}
	var serving sync.WaitGroup
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			fake.add(conn)
			serving.Add(1)
			go func(conn net.Conn) {
				defer serving.Done()
				defer func() { _ = conn.Close() }()
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadBytes('\n')
					if err != nil {
						return
					}
					var call map[string]any
					if err := json.Unmarshal(line, &call); err != nil {
						return
					}
					fake.record(call)
					// A slow write is what makes the concurrency test meaningful: it
					// widens the window in which two callers could both decide to act.
					if call["method"] == "agent.send_keys" {
						time.Sleep(120 * time.Millisecond)
					}
					method := call["method"].(string)
					params, _ := call["params"].(map[string]any)
					if delay := fake.delayFor(method); delay > 0 {
						time.Sleep(delay)
					}
					reply := map[string]any{"id": call["id"]}
					if failure, failed := fake.failure(method); failed {
						reply["error"] = failure
					} else if method == "agent.start" && fake.takeBusyAgentStart() {
						reply["error"] = map[string]string{
							"code": "agent_pane_busy", "message": "agent target pane is not an available shell",
						}
					} else {
						if method == "pane.send_keys" {
							keys, _ := params["keys"].([]any)
							for _, key := range keys {
								if key == "ctrl+c" {
									fake.clearBusyAgentStarts()
								}
							}
						}
						if method == "agent.start" && !fake.suppressingAgent() {
							paneID, _ := params["pane_id"].(string)
							kind, _ := params["kind"].(string)
							fake.setAgent(paneID, kind)
							if lifetime := fake.agentLifetimeFor(); lifetime > 0 {
								time.AfterFunc(lifetime, func() { fake.clearAgent(paneID) })
							}
						}
						if method == "pane.send_input" {
							text, _ := params["text"].(string)
							if strings.Contains(text, "dsh-tui") {
								paneID, _ := params["pane_id"].(string)
								fake.setAgent(paneID, "dsh-tui")
								if lifetime := fake.agentLifetimeFor(); lifetime > 0 {
									time.AfterFunc(lifetime, func() { fake.clearAgent(paneID) })
								}
							}
						}
						reply["result"] = resultFor(method, params, panes, fake)
					}
					body, _ := json.Marshal(reply)
					if _, err := conn.Write(append(body, '\n')); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	// Stop serving before the test's temporary directory is removed. Cleanups run last-registered
	// first, and `t.TempDir` registered its removal when it was called — so this runs in time.
	t.Cleanup(func() {
		_ = listener.Close()
		fake.closeClients()
		serving.Wait()
	})
	return fake, path
}

func resultFor(method string, params map[string]any, panes []map[string]any, fake *fakeHerdr) map[string]any {
	switch method {
	case "pane.get":
		paneID, _ := params["pane_id"].(string)
		found := map[string]any{}
		for _, candidate := range panes {
			if candidate["pane_id"] == paneID {
				found = candidate
				break
			}
		}
		if len(found) == 0 {
			found = map[string]any{"pane_id": paneID, "workspace_id": "wN", "tab_id": "wN:t1"}
		}
		if kind := fake.agentFor(paneID); kind != "" {
			found = cloneMap(found)
			found["agent"] = kind
		}
		return map[string]any{"type": "pane_info", "pane": found}
	case "pane.process_info":
		paneID, _ := params["pane_id"].(string)
		process := fake.process(paneID)
		if process == nil {
			return map[string]any{"type": "pane_process_info", "process_info": map[string]any{
				"foreground_processes": []any{},
			}}
		}
		return map[string]any{"type": "pane_process_info", "process_info": map[string]any{
			"foreground_processes": []any{process},
		}}
	case "session.snapshot":
		// One pane, one workspace, one tab: the shapes the bridge's own decoder
		// requires. Tests that need something else stand up their own fake socket.
		return map[string]any{
			"type": "session_snapshot",
			"snapshot": map[string]any{
				"version": "0.9.1", "protocol": 22,
				"panes":      anyPanes(panes),
				"workspaces": anyWorkspaces(panes),
				"tabs":       []any{},
				"agents":     []any{},
			},
		}
	case "pane.read":
		return map[string]any{
			"type": "pane_read",
			"read": map[string]any{
				"pane_id": "wX:p1", "workspace_id": "wX", "tab_id": "wX:t1",
				"source": "recent", "format": "text", "text": "what now?", "revision": 3,
			},
		}
	case "workspace.create":
		return map[string]any{
			"type": "workspace_created",
			"workspace": map[string]any{
				"workspace_id": "wN", "number": 1, "label": "new", "pane_count": 1,
				"tab_count": 1, "active_tab_id": "wN:t1", "agent_status": "unknown",
			},
			"tab": map[string]any{
				"tab_id": "wN:t1", "workspace_id": "wN", "number": 1, "label": "1",
				"pane_count": 1, "agent_status": "unknown",
			},
			"root_pane": map[string]any{
				"pane_id": "wN:p1", "workspace_id": "wN", "tab_id": "wN:t1",
				"cwd": "/tmp", "foreground_cwd": "/tmp", "agent_status": "unknown",
			},
		}
	case "tab.create":
		return map[string]any{
			"type": "tab_created",
			"tab": map[string]any{
				"tab_id": "wN:t2", "workspace_id": "wN", "number": 2, "label": "codex-2",
				"pane_count": 1, "agent_status": "unknown",
			},
			"root_pane": map[string]any{
				"pane_id": "wN:p2", "workspace_id": "wN", "tab_id": "wN:t2",
				"cwd": "/tmp", "foreground_cwd": "/tmp", "agent_status": "unknown",
			},
		}
	case "agent.start":
		paneID, _ := params["pane_id"].(string)
		kind, _ := params["kind"].(string)
		return map[string]any{
			"type": "agent_started",
			"agent": map[string]any{
				"agent": kind, "pane_id": paneID, "workspace_id": "wN",
				"tab_id": "wN:t1", "agent_status": "idle", "interactive_ready": true,
			},
			"argv": []any{kind},
		}
	default:
		return map[string]any{"type": "ok"}
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func do(t *testing.T, ts *httptest.Server, method, path, token, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response) map[string]string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		if text, ok := value.(string); ok {
			out[key] = text
		}
	}
	return out
}

// statusOnly performs a request and closes the body, for the many assertions
// that only care about the status code.
func statusOnly(t *testing.T, ts *httptest.Server, method, path, token, body string) int {
	t.Helper()
	status, _ := call(t, ts, method, path, token, body)
	return status
}

// call is the single place that owns the response body's lifetime.
func call(t *testing.T, ts *httptest.Server, method, path, token, body string) (int, map[string]string) {
	t.Helper()
	resp := do(t, ts, method, path, token, body)
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	out := map[string]string{}
	var generic map[string]any
	if json.Unmarshal(raw, &generic) == nil {
		for key, value := range generic {
			if text, ok := value.(string); ok {
				out[key] = text
			}
		}
	}
	return resp.StatusCode, out
}

// TestEverythingRequiresTheToken is the first line of defence: the bridge binds
// every interface, so an unauthenticated endpoint would be an open door on
// whatever network the user happens to be on.
func TestEverythingRequiresTheToken(t *testing.T) {
	ts, _ := newTestServer(t)

	for _, path := range []string{
		"/v1/health", "/v1/snapshot", "/v1/decisions", "/v1/value", "/v1/stream",
		"/v1/session", "/v1/session/keys", "/v1/harnesses", "/v1/folders",
		"/v1/feedback/screenshot",
	} {
		status, body := call(t, ts, http.MethodGet, path, "", "")
		if status != http.StatusUnauthorized {
			t.Errorf("GET %s without a token = %d, want 401", path, status)
		}
		if body["fix"] == "" {
			t.Errorf("GET %s: a refusal must tell the user what to do", path)
		}
	}

	// A wrong token must fail the same way as a missing one.
	if status := statusOnly(t, ts, http.MethodGet, "/v1/health", "not-the-token", ""); status != http.StatusUnauthorized {
		t.Errorf("wrong token = %d, want 401", status)
	}
}

// TestOnlyTheFourWriteKindsAreAccepted is constitution IV checked at the wire.
func TestOnlyTheFourWriteKindsAreAccepted(t *testing.T) {
	ts, _ := newTestServer(t)

	cases := []struct {
		name string
		body string
		want int
	}{
		{
			name: "a fifth kind",
			body: `{"decision_id":"d","kind":"rename","target":"wA:p1"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "approve carrying text",
			body: `{"decision_id":"d","kind":"approve","target":"wA:p1","text":"rm -rf /"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "answer without text",
			body: `{"decision_id":"d","kind":"answer","target":"wA:p1"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "no target",
			body: `{"decision_id":"d","kind":"prompt","text":"hello"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "an unknown field, so a future app cannot smuggle intent",
			body: `{"decision_id":"d","kind":"approve","target":"wA:p1","workspace":"wB"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "not json at all",
			body: `<xml/>`,
			want: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if status := statusOnly(t, ts, http.MethodPost, "/v1/decisions", testToken, tc.body); status != tc.want {
				t.Fatalf("status = %d, want %d", status, tc.want)
			}
		})
	}
}

// TestUnreachableHerdrIsExplainedNotSwallowed covers spec FR-015: the user is
// told what is wrong and what to do, rather than being shown an empty screen.
func TestUnreachableHerdrIsExplainedNotSwallowed(t *testing.T) {
	ts, _ := newTestServer(t)

	resp := do(t, ts, http.MethodGet, "/v1/snapshot", testToken, "")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	if !strings.Contains(body["error"], "herdr") {
		t.Errorf("the reason should name herdr: %q", body["error"])
	}
	if !strings.Contains(body["fix"], "doctor") {
		t.Errorf("the fix should be actionable: %q", body["fix"])
	}

	// Health, by contrast, must answer: its whole job is to describe the problem.
	resp = do(t, ts, http.MethodGet, "/v1/health", testToken, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}
	defer func() { _ = resp.Body.Close() }()
	var health struct {
		Health struct {
			Overall string `json:"overall"`
			Items   []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Fix    string `json:"fix"`
			} `json:"items"`
		} `json:"health"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if health.Health.Overall != "fail" {
		t.Errorf("overall = %q, want fail while herdr is unreachable", health.Health.Overall)
	}
	var sawSocketFailure bool
	for _, item := range health.Health.Items {
		if item.ID == "socket" {
			sawSocketFailure = item.Status == "fail" && item.Fix != ""
		}
	}
	if !sawSocketFailure {
		t.Error("the socket check must fail and carry a fix")
	}
}

// TestPushRegistration covers the handshake that lets the computer reach the
// phone, including the half-configured state the user will actually hit.
func TestPushRegistration(t *testing.T) {
	ts, c := newTestServer(t)

	if status := statusOnly(t, ts, http.MethodPost, "/v1/push/register", testToken, `{"device_token":"fcm-abc"}`); status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	cfg, err := c.PushConfig()
	if err != nil {
		t.Fatalf("push config: %v", err)
	}
	if cfg.DeviceToken != "fcm-abc" {
		t.Errorf("device token = %q", cfg.DeviceToken)
	}
	// A token without a gateway is not "configured": saying so would hide the
	// half-done setup from the check-up.
	if c.PushConfigured() {
		t.Error("a device token alone must not count as configured")
	}

	for _, body := range []string{`{}`, `{"device_token":"   "}`, `{"token":"wrong-field"}`} {
		if status := statusOnly(t, ts, http.MethodPost, "/v1/push/register", testToken, body); status != http.StatusBadRequest {
			t.Errorf("body %s = %d, want 400", body, status)
		}
	}
}

// TestStreamRejectsPlainRequests: a browser or a curl hitting the stream endpoint
// should get a sentence, not a broken connection.
func TestStreamRejectsPlainRequests(t *testing.T) {
	ts, _ := newTestServer(t)
	status, body := call(t, ts, http.MethodGet, "/v1/stream", testToken, "")
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	// The message must say what the endpoint needs, in whatever words the
	// transport layer chose.
	if !strings.Contains(strings.ToLower(body["error"]), "websocket") {
		t.Errorf("error should name the requirement: %q", body["error"])
	}
}

// TestStreamLogsAttachAndEnd: #130. A phone whose notification says "not connected" and a
// bridge the phone never reached look the same from the phone; on 2026-10-17 bridge.log
// could not tell them apart either, because /v1/stream logged nothing in either case. The
// next occurrence is only diagnosable if that changes: one line when a stream attaches and
// one when it ends, with the address and the reason.
//
// Both ways a phone can leave are exercised, because they take different paths through the
// reader: a close frame (what a well-behaved client sends) and the socket simply dying.
func TestStreamLogsAttachAndEnd(t *testing.T) {
	ts, _, logs := newTestServerWithSocketAndLogs(t, filepath.Join(t.TempDir(), "absent.sock"))

	// A phone that leaves politely.
	polite := dialStream(t, ts.URL)
	waitForLogCount(t, logs, "event stream attached", 1)
	if !strings.Contains(logs.String(), "remote=127.0.0.1:") {
		t.Errorf("the attach line does not carry the client's address:\n%s", logs.String())
	}
	writeMaskedCloseFrame(t, polite)
	waitForLogCount(t, logs, "event stream ended", 1)
	if got := strings.Count(logs.String(), "client closed"); got != 1 {
		t.Errorf("a close frame should end the stream as the client's doing, saw %d such lines:\n%s", got, logs.String())
	}
	// The ending line carries the address too: with more than one device paired, an
	// ending nobody can attribute is as useless as no line at all.
	if got := strings.Count(logs.String(), "remote=127.0.0.1:"); got != 2 {
		t.Errorf("want the address on both the attach and the end line, saw %d lines with it:\n%s", got, logs.String())
	}

	// A phone that vanishes: the socket dies with no close frame.
	abrupt := dialStream(t, ts.URL)
	waitForLogCount(t, logs, "event stream attached", 2)
	_ = abrupt.Close()
	waitForLogCount(t, logs, "event stream ended", 2)
	if got := strings.Count(logs.String(), "client closed"); got != 2 {
		t.Errorf("a dropped socket should end the stream as the client's doing, saw %d such lines:\n%s", got, logs.String())
	}
}

func TestStreamsEndWithTheBridge(t *testing.T) {
	t.Setenv(state.EnvStateDir, t.TempDir())
	t.Setenv("HERDR_SOCKET_PATH", filepath.Join(t.TempDir(), "absent.sock"))

	c, err := core.New(core.Options{DisableSubscriptions: true})
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	t.Cleanup(c.Close)
	logs := &syncBuffer{}
	srv, err := New(c, Options{
		Addr: "127.0.0.1:0", Token: testToken, Version: "test",
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = srv.Run(ctx); close(done) }()

	conn := dialStream(t, "http://"+waitForBridgeAddr(t, logs))
	t.Cleanup(func() { _ = conn.Close() })
	waitForLog(t, logs, "event stream attached")

	// systemd asks the bridge to stop, with the phone still attached.
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return with a stream attached; a restart would need a kill")
	}
	// Run's return is the promise that the goodbye is already written: a restart that
	// races its own log leaves nothing to read after the redeploy it was needed for.
	if !strings.Contains(logs.String(), "event stream ended") {
		t.Errorf("Run returned before the stream wrote its goodbye:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "the bridge is stopping") {
		t.Errorf("the stream did not say the bridge was why it ended:\n%s", logs.String())
	}
}

// waitForBridgeAddr reads back what Run announced, since Run refuses to pretend the
// configured :0 is an address anyone can dial.
func waitForBridgeAddr(t *testing.T, logs *syncBuffer) string {
	t.Helper()
	waitForLog(t, logs, "bridge listening")
	for _, field := range strings.Fields(logs.String()) {
		if addr, ok := strings.CutPrefix(field, "addr="); ok {
			return addr
		}
	}
	t.Fatalf("the listening line names no address:\n%s", logs.String())
	return ""
}

// TestWriteFailureDefersToTheReader pins the tie-break #130's review asked for: when a
// write fails as the client dies, the description comes from the reader, which is the one
// that can say "client closed" — the write error only says how the bridge noticed.
func TestWriteFailureDefersToTheReader(t *testing.T) {
	readErr := make(chan error, 1)
	readErr <- io.EOF
	if got := writeFailureReason("heartbeat failed", errors.New("broken pipe"), readErr); got != "client closed: EOF" {
		t.Errorf("reason = %q, want the reader's account", got)
	}

	// A phone that is merely not reading sends the reader nothing, and then the write
	// error is all there is to report.
	silent := make(chan error)
	if got := writeFailureReason("heartbeat failed", errors.New("i/o timeout"), silent); got != "heartbeat failed: i/o timeout" {
		t.Errorf("reason = %q, want the write error when the reader saw nothing", got)
	}
}

// writeMaskedCloseFrame sends the one frame a well-behaved client sends when it leaves.
func writeMaskedCloseFrame(t *testing.T, conn net.Conn) {
	t.Helper()
	mask := []byte{0x5a, 0x5b, 0x5c, 0x5d}
	frame := []byte{0x88, 0x80} // FIN + opClose, masked, empty payload
	frame = append(frame, mask...)
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write close frame: %v", err)
	}
}

// dialStream opens a raw WebSocket to the stream endpoint of baseURL and returns the
// connection once the handshake response (101 Switching Protocols) has been read.
func dialStream(t *testing.T, baseURL string) net.Conn {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	conn, err := net.DialTimeout("tcp", parsed.Host, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	handshake := fmt.Sprintf(
		"GET /v1/stream?token=%s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\n"+
			"Connection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		testToken, parsed.Host, key,
	)
	if _, err := io.WriteString(conn, handshake); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	if !strings.Contains(statusLine, "101") {
		t.Fatalf("status line = %q, want 101", strings.TrimSpace(statusLine))
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read headers: %v", err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	return conn
}

// TestWebSocketHandshakeAndControlFrames exercises the hand-written RFC 6455
// server: the accept key, and the masking rules that a naive implementation gets
// wrong (client frames are masked, server frames are not).
func TestWebSocketHandshakeAndControlFrames(t *testing.T) {
	ts, _ := newTestServer(t)
	parsed, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	conn, err := net.DialTimeout("tcp", parsed.Host, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	handshake := fmt.Sprintf(
		"GET /v1/stream?token=%s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\n"+
			"Connection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		testToken, parsed.Host, key,
	)
	if _, err := io.WriteString(conn, handshake); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	if !strings.Contains(statusLine, "101") {
		t.Fatalf("status line = %q, want 101", strings.TrimSpace(statusLine))
	}

	wantAccept := wsAcceptForTest(key)
	var gotAccept string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read headers: %v", err)
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		if name, value, ok := strings.Cut(trimmed, ":"); ok &&
			strings.EqualFold(strings.TrimSpace(name), "Sec-WebSocket-Accept") {
			gotAccept = strings.TrimSpace(value)
		}
	}
	if gotAccept != wantAccept {
		t.Fatalf("Sec-WebSocket-Accept = %q, want %q", gotAccept, wantAccept)
	}

	// A masked ping must come back as an unmasked pong carrying the same payload.
	payload := []byte("hi")
	mask := []byte{0x11, 0x22, 0x33, 0x44}
	frame := []byte{0x89, byte(0x80 | len(payload))}
	frame = append(frame, mask...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		t.Fatalf("read pong header: %v", err)
	}
	if opcode := header[0] & 0x0F; opcode != 0x0A {
		t.Fatalf("opcode = %#x, want pong (0xA)", opcode)
	}
	if header[1]&0x80 != 0 {
		t.Fatal("server frames must not be masked")
	}
	length := int(header[1] & 0x7F)
	if length != len(payload) {
		t.Fatalf("pong length = %d, want %d", length, len(payload))
	}
	got := make([]byte, length)
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatalf("read pong payload: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("pong payload = %q, want %q", got, payload)
	}
}

func wsAcceptForTest(key string) string {
	h := sha1.New()
	_, _ = io.WriteString(h, key)
	_, _ = io.WriteString(h, wsGUID)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// TestEscapeHatchDeliversTextWithoutARequest covers the escape hatch's write
// half: the user typed something into a session that has no pending request.
//
// It runs against a fake herdr on purpose. The real alternative is typing into
// somebody's actual shell, which is exactly the sort of thing a test should never
// do to a machine it does not own.
func TestEscapeHatchDeliversTextWithoutARequest(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent_status": "unknown", "revision": 1,
	}
	fake, socketPath := startFakeHerdr(t, pane)
	ts, _ := newTestServerWithSocket(t, socketPath)

	status, body := call(t, ts, http.MethodPost, "/v1/decisions", testToken,
		`{"kind":"prompt","target":"wX:p1","text":"npm test"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%v), want 200: delivering text needs no request", status, body)
	}

	// The write must be the fourth class, on the pane, with the text intact.
	sent := fake.paramsFor("pane.send_text")
	if sent == nil {
		t.Fatalf("no pane.send_text was issued; methods seen: %v", fake.methods())
	}
	if sent["text"] != "npm test" {
		t.Errorf("text = %v, want the user's words verbatim", sent["text"])
	}
	if sent["pane_id"] != "wX:p1" {
		t.Errorf("pane = %v", sent["pane_id"])
	}
}

func TestEscapeHatchDeliversTextToAPaneLaunchedHarness(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent": "dsh-tui",
		"agent_status": "idle", "revision": 1,
	}
	fake, socketPath := startFakeHerdr(t, pane)
	ts, _ := newTestServerWithSocket(t, socketPath)

	status, body := call(t, ts, http.MethodPost, "/v1/decisions", testToken,
		`{"kind":"prompt","target":"wX:p1","text":"hello"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%v), want 200", status, body)
	}
	sent := fake.paramsFor("pane.send_input")
	if sent == nil {
		t.Fatalf("no pane.send_input was issued; methods seen: %v", fake.methods())
	}
	if sent["text"] != "hello" {
		t.Errorf("text = %v, want the user's words", sent["text"])
	}
	keys, _ := sent["keys"].([]any)
	if len(keys) != 1 || keys[0] != "enter" {
		t.Errorf("keys = %v, want Enter", sent["keys"])
	}
	if fake.paramsFor("agent.prompt") != nil {
		t.Error("a pane-launched harness was sent through agent.prompt")
	}
}

func TestEscapeHatchRefusesTextIntoAFirstRunWizard(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent": "dsh-tui",
		"agent_status": "idle", "revision": 1,
	}
	fake, socketPath := startFakeHerdrWithText(
		t, pane, "Welcome to dsh-TUI · Step 1 of 4\nContinue with Enter",
	)
	ts, _ := newTestServerWithSocket(t, socketPath)

	status, body := call(t, ts, http.MethodPost, "/v1/decisions", testToken,
		`{"kind":"prompt","target":"wX:p1","text":"hello"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d (%v), want 400: a wizard takes no message", status, body)
	}
	if !strings.Contains(body["fix"], "terminal") {
		t.Errorf("fix = %q, want the terminal instruction", body["fix"])
	}
	if contains(fake.methods(), "pane.send_text") {
		t.Errorf("text was typed into a first-run wizard: %v", fake.methods())
	}
}

func TestEscapeHatchDoesNotSendWhenTheScreenCannotBeRead(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent": "codex",
		"agent_status": "idle", "revision": 1,
	}
	fake, socketPath := startFakeHerdr(t, pane)
	fake.fail("pane.read", "pane_read_failed", "screen unavailable")
	ts, _ := newTestServerWithSocket(t, socketPath)

	status, body := call(t, ts, http.MethodPost, "/v1/decisions", testToken,
		`{"kind":"prompt","target":"wX:p1","text":"hello"}`)
	if status == http.StatusOK {
		t.Fatalf("status = %d (%v), want a refusal", status, body)
	}
	if contains(fake.methods(), "pane.send_text") {
		t.Errorf("text was sent without reading the screen: %v", fake.methods())
	}
}

func TestEscapeHatchDoesNotSendWhenTheScreenIsEmpty(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent": "codex",
		"agent_status": "idle", "revision": 1,
	}
	fake, socketPath := startFakeHerdrWithText(t, pane, "")
	ts, _ := newTestServerWithSocket(t, socketPath)

	status, body := call(t, ts, http.MethodPost, "/v1/decisions", testToken,
		`{"kind":"prompt","target":"wX:p1","text":"hello"}`)
	if status == http.StatusOK {
		t.Fatalf("status = %d (%v), want a refusal", status, body)
	}
	if contains(fake.methods(), "pane.send_text") {
		t.Errorf("text was sent before the screen painted: %v", fake.methods())
	}
}

// TestStandaloneWritesAreLimitedToText is the boundary of the concession above:
// approve and reject answer a request the computer raised, so they still need one.
func TestStandaloneWritesAreLimitedToText(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent_status": "blocked", "revision": 1,
	}
	fake, socketPath := startFakeHerdr(t, pane)
	ts, _ := newTestServerWithSocket(t, socketPath)

	for _, body := range []string{
		`{"kind":"approve","target":"wX:p1"}`,
		`{"kind":"reject","target":"wX:p1"}`,
		// And a fifth kind is still a fifth kind.
		`{"kind":"rename","target":"wX:p1"}`,
	} {
		status, payload := call(t, ts, http.MethodPost, "/v1/decisions", testToken, body)
		if status != http.StatusBadRequest {
			t.Errorf("body %s = %d (%v), want 400", body, status, payload)
		}
	}

	// Nothing may have been sent to the machine.
	for _, method := range fake.methods() {
		if strings.HasPrefix(method, "pane.send") || strings.HasPrefix(method, "agent.send") {
			t.Errorf("a refused request still wrote to herdr: %s", method)
		}
	}
}

// TestSessionViewReportsWhatMayBeDone: the escape hatch must offer exactly what
// the cards would, decided by the same table rather than by the app's opinion.
func TestSessionViewReportsWhatMayBeDone(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent": "codex",
		"agent_status": "blocked", "revision": 9,
	}
	_, socketPath := startFakeHerdr(t, pane)
	ts, _ := newTestServerWithSocket(t, socketPath)

	status, _ := call(t, ts, http.MethodGet, "/v1/session?pane_id=wX:p1&lines=10", testToken, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	// A session with no rule named cannot be driven, so nothing may be offered —
	// but text delivery always can be, because it is the user typing.
	var view struct {
		Text           string `json:"text"`
		Approvable     bool   `json:"approvable"`
		Rejectable     bool   `json:"rejectable"`
		Answerable     bool   `json:"answerable"`
		CanDeliverText bool   `json:"can_deliver_text"`
		Unavailable    string `json:"unavailable"`
	}
	raw := getJSON(t, ts, "/v1/session?pane_id=wX:p1&lines=10", testToken)
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Text == "" {
		t.Error("the escape hatch is useless without the text it exists to show")
	}
	if view.Approvable || view.Rejectable || view.Answerable {
		t.Errorf("nothing should be offered when no rule names the dialog: %+v", view)
	}
	if !view.CanDeliverText {
		t.Error("delivering text is always available")
	}
	if view.Unavailable == "" {
		t.Error("the user must be told why nothing can be pressed")
	}
}

func TestSessionViewReportsWhatTheTerminalIsHolding(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent": "codex",
		"agent_status": "idle", "revision": 9,
	}
	// A capture of codex's input line holding a draft, with the escape sequences the
	// terminal really sends (core/composer_test.go has the full set).
	fake, socketPath := startFakeHerdrWithText(t, pane, "\x1b[1m› \x1b[0mpartial unsent draft")
	ts, _ := newTestServerWithSocket(t, socketPath)

	raw := getJSON(t, ts, "/v1/session?pane_id=wX:p1&lines=10", testToken)
	var view struct {
		Unsent string `json:"unsent"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Unsent != "partial unsent draft" {
		t.Errorf("unsent = %q, want the sentence the terminal is holding", view.Unsent)
	}
	if !strings.Contains(view.Text, "partial unsent draft") {
		t.Errorf("the screen itself must still carry the text: %q", view.Text)
	}
	if n := countMethod(fake.methods(), "pane.read"); n != 2 {
		t.Errorf("pane.read calls = %d, want 2 (the screen, then the input line)", n)
	}
}

// TestSessionViewSaysNothingAboutAgentsItHasNotCaptured: an agent whose input line
// nobody has measured gets no answer, and costs no extra read.
//
// Silence is the correct failure here. A warning in the main interface that is wrong
// teaches the user to ignore the warning, and the honest answer to "is there text in
// that box?" for an agent we have never looked at is "we do not know" — said by saying
// nothing at all.
func TestSessionViewSaysNothingAboutAgentsItHasNotCaptured(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent": "pi",
		"agent_status": "idle", "revision": 9,
	}
	fake, socketPath := startFakeHerdrWithText(t, pane, "› partial unsent draft")
	ts, _ := newTestServerWithSocket(t, socketPath)

	raw := getJSON(t, ts, "/v1/session?pane_id=wX:p1&lines=10", testToken)
	var view struct {
		Unsent string `json:"unsent"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Unsent != "" {
		t.Errorf("an agent nobody captured must not be guessed at: unsent = %q", view.Unsent)
	}
	if n := countMethod(fake.methods(), "pane.read"); n != 1 {
		t.Errorf("pane.read calls = %d, want 1 (the screen only)", n)
	}
}

func TestSessionViewReportsAHarnessStuckInSetup(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent": "dsh-tui",
		"agent_status": "idle", "revision": 9,
	}
	_, socketPath := startFakeHerdrWithText(
		t, pane, "Welcome to dsh-TUI · Step 1 of 4\nContinue with Enter",
	)
	ts, _ := newTestServerWithSocket(t, socketPath)

	raw := getJSON(t, ts, "/v1/session?pane_id=wX:p1&lines=40", testToken)
	var view struct {
		SetupRequired  bool   `json:"setup_required"`
		SetupReason    string `json:"setup_reason"`
		SetupFix       string `json:"setup_fix"`
		CanDeliverText bool   `json:"can_deliver_text"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !view.SetupRequired {
		t.Error("an onboarding pane was not reported as needing setup")
	}
	if view.CanDeliverText {
		t.Error("text may not be delivered into a first-run wizard")
	}
	if view.SetupReason == "" || view.SetupFix == "" {
		t.Errorf("setup notice is incomplete: %+v", view)
	}
}

func TestSessionViewDoesNotReportANormalHarnessAsSetup(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent": "dsh-tui",
		"agent_status": "idle", "revision": 9,
	}
	_, socketPath := startFakeHerdrWithText(t, pane, "Session ba447a16\n❯ ")
	ts, _ := newTestServerWithSocket(t, socketPath)

	raw := getJSON(t, ts, "/v1/session?pane_id=wX:p1&lines=40", testToken)
	var view struct {
		SetupRequired  bool `json:"setup_required"`
		CanDeliverText bool `json:"can_deliver_text"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.SetupRequired {
		t.Error("a normal launchpad was reported as setup")
	}
	if !view.CanDeliverText {
		t.Error("a ready session must accept text")
	}
}

func countMethod(methods []string, name string) int {
	n := 0
	for _, method := range methods {
		if method == name {
			n++
		}
	}
	return n
}

// TestSessionNeedsAPaneID keeps the endpoint from guessing which session to read.
func TestSessionNeedsAPaneID(t *testing.T) {
	ts, _ := newTestServer(t)
	status, body := call(t, ts, http.MethodGet, "/v1/session", testToken, "")
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if body["fix"] == "" {
		t.Error("a refusal must say what to do instead")
	}
}

func getJSON(t *testing.T, ts *httptest.Server, path, token string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body
}

// TestCJKArrivesByteForByte is the transport half of spec SC-003, which requires
// a Chinese instruction to arrive with a 100% character match.
//
// The half the bridge is responsible for is the encoding path: JSON escaping,
// Unicode handling, and not "helpfully" normalising or re-wrapping anything. The
// other half — whether the terminal renders the characters correctly — needs a
// real agent, and is in the manual verification guide.
func TestCJKArrivesByteForByte(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent_status": "unknown", "revision": 1,
	}
	fake, socketPath := startFakeHerdr(t, pane)
	ts, _ := newTestServerWithSocket(t, socketPath)

	// Deliberately awkward: mixed scripts, CJK punctuation, a line break, an emoji
	// outside the basic plane, and a combining sequence. If anything in our path
	// re-encodes or normalises, one of these will come back different.
	text := "Rename the handler in src/café.ts to handleRequest\n" +
		"Then run npm test — note: “curly quotes”, guillemets «ok», and accents résumé.\n" +
		"emoji: 🐈‍⬛ family: 👩‍👩‍👧\n" +
		"combining: e\u0301 vs é"

	body, err := json.Marshal(map[string]string{
		"kind":   "prompt",
		"target": "wX:p1",
		"text":   text,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	status, payload := call(t, ts, http.MethodPost, "/v1/decisions", testToken, string(body))
	if status != http.StatusOK {
		t.Fatalf("status = %d (%v), want 200", status, payload)
	}

	sent := fake.paramsFor("pane.send_text")
	if sent == nil {
		t.Fatalf("nothing was delivered; methods seen: %v", fake.methods())
	}
	got, _ := sent["text"].(string)
	if got != text {
		t.Errorf("the text did not survive the round trip.\nwant: %q\n got: %q", text, got)
	}
	if len(got) != len(text) {
		t.Errorf("length changed: %d → %d bytes", len(text), len(got))
	}
}

// TestExcerptsKeepCJK makes sure the other direction is faithful too: what the
// computer is showing comes back to the phone unchanged, since that is what the
// card and the notification display.
func TestExcerptsKeepCJK(t *testing.T) {
	want := "Continue with deployment (y/n)?\n❯ 1. Yes — café\n  2. No"
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent_status": "unknown", "revision": 1,
	}
	_, socketPath := startFakeHerdrWithText(t, pane, want)
	ts, _ := newTestServerWithSocket(t, socketPath)

	raw := getJSON(t, ts, "/v1/session?pane_id=wX:p1", testToken)
	var view struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Text != want {
		t.Errorf("excerpt changed in flight.\nwant: %q\n got: %q", want, view.Text)
	}
}

// TestEscapeHatchKeyBarReachesThePane covers the one write that is not a decision.
//
// It is a fake herdr for the same reason the text-delivery test is: the real
// version of this test types into somebody's live shell.
func TestEscapeHatchKeyBarReachesThePane(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent_status": "blocked", "revision": 1,
	}
	fake, socketPath := startFakeHerdr(t, pane)
	ts, _ := newTestServerWithSocket(t, socketPath)

	resp := do(t, ts, http.MethodPost, "/v1/session/keys", testToken, `{"pane_id":"wX:p1","keys":["esc"]}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		t.Fatalf("status = %d (%s), want 200", resp.StatusCode, raw)
	}
	// Read as the app reads it: the equivalent command is a list of arguments,
	// not a sentence, because the app shows it argument by argument.
	var decoded struct {
		EquivalentCommand []string `json:"equivalent_command"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The write must be at the pane, and it must be the key the user pressed.
	// agent.send_keys is deliberately *not* the target here: it resolves the live
	// agent and refuses when that agent no longer owns the pane, whereas the
	// escape hatch is the user operating a terminal (see herdr-usage.md §3).
	sent := fake.paramsFor("pane.send_keys")
	if sent == nil {
		t.Fatalf("no pane.send_keys was issued; methods seen: %v", fake.methods())
	}
	if sent["pane_id"] != "wX:p1" {
		t.Errorf("pane = %v, want wX:p1", sent["pane_id"])
	}
	keys, _ := sent["keys"].([]any)
	if len(keys) != 1 || keys[0] != "esc" {
		t.Errorf("keys = %v, want exactly [esc]", sent["keys"])
	}
	if fake.paramsFor("agent.send_keys") != nil {
		t.Error("the key bar reached the agent rather than the pane it was opened on")
	}

	// The response has to be reproducible by hand, like every other write
	// (constitution II).
	wantCommand := "herdr pane send-keys wX:p1 esc"
	if got := strings.Join(decoded.EquivalentCommand, " "); got != wantCommand {
		t.Errorf("equivalent_command = %q, want %q", got, wantCommand)
	}
}

// TestEscapeHatchKeyBarRefusesTheRest is the boundary of that concession.
//
// Three separate promises are checked at once, because they are one promise: the
// bar is a fixed vocabulary (not a keyboard), it is bounded (not a way to pump
// input in), and a refusal writes nothing. herdr itself validates every key
// before writing any bytes — the source says so in as many words and encodes every
// key up front — so a name outside the vocabulary would be refused there too; the
// point of refusing here is that the product never asks herdr for a key it did not
// authorise.
func TestEscapeHatchKeyBarRefusesTheRest(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": false, "agent_status": "blocked", "revision": 1,
	}

	for _, tc := range []struct {
		name string
		body string
	}{
		{"a key the browser has, the bar does not", `{"pane_id":"wX:p1","keys":["f5"]}`},
		{"a word rather than a key", `{"pane_id":"wX:p1","keys":["please continue"]}`},
		{"a whole sentence", `{"pane_id":"wX:p1","keys":["y","e","s","enter","enter"]}`},
		{"no keys at all", `{"pane_id":"wX:p1","keys":[]}`},
		{"no session named", `{"keys":["enter"]}`},
		{"fields we did not agree on", `{"pane_id":"wX:p1","keys":["enter"],"text":"rm -rf"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, socketPath := startFakeHerdr(t, pane)
			ts, _ := newTestServerWithSocket(t, socketPath)

			status, body := call(t, ts, http.MethodPost, "/v1/session/keys", testToken, tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d (%v), want 400", status, body)
			}
			if body["error"] == "" {
				t.Error("a refusal with no reason; the user is left guessing")
			}
			if body["fix"] == "" {
				t.Error("a refusal with no next step (spec FR-015)")
			}

			// The important half: a refusal must not have written anything.
			for _, method := range fake.methods() {
				if strings.HasPrefix(method, "pane.send") || strings.HasPrefix(method, "agent.send") {
					t.Errorf("a refused request still wrote to herdr: %s", method)
				}
			}
		})
	}
}

// startFakeHerdrWithText is startFakeHerdr with control over what a read returns.
// anyPanes and anyWorkspaces shape whatever the test handed over, deduplicated by
// workspace, so one fake can serve both the single-pane tests and a scale test.
func anyPanes(panes []map[string]any) []any {
	out := make([]any, 0, len(panes))
	for _, pane := range panes {
		out = append(out, pane)
	}
	return out
}

func anyWorkspaces(panes []map[string]any) []any {
	seen := map[string]bool{}
	out := []any{}
	for _, pane := range panes {
		id, _ := pane["workspace_id"].(string)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, map[string]any{
			"workspace_id": id, "number": len(out) + 1, "label": id,
			"pane_count": 1, "tab_count": 1, "active_tab_id": id + ":t1",
			"agent_status": "idle",
		})
	}
	if len(out) == 0 {
		out = append(out, map[string]any{
			"workspace_id": "wX", "number": 1, "label": "project",
			"pane_count": 1, "tab_count": 1, "active_tab_id": "wX:t1",
			"agent_status": "idle",
		})
	}
	return out
}

func startFakeHerdrWithText(t *testing.T, pane map[string]any, text string) (*fakeHerdr, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	fake := &fakeHerdr{}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadBytes('\n')
					if err != nil {
						return
					}
					var call map[string]any
					if err := json.Unmarshal(line, &call); err != nil {
						return
					}
					fake.record(call)
					// A slow write is what makes the concurrency test meaningful: it
					// widens the window in which two callers could both decide to act.
					if call["method"] == "agent.send_keys" {
						time.Sleep(120 * time.Millisecond)
					}
					params, _ := call["params"].(map[string]any)
					result := resultFor(call["method"].(string), params, []map[string]any{pane}, fake)
					if call["method"] == "pane.read" {
						result["read"] = map[string]any{
							"pane_id": "wX:p1", "workspace_id": "wX", "tab_id": "wX:t1",
							"source": "recent", "format": "text", "text": text, "revision": 4,
						}
					}
					reply := map[string]any{"id": call["id"], "result": result}
					out, _ := json.Marshal(reply)
					if _, err := conn.Write(append(out, '\n')); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return fake, path
}

func TestHealthReportsTheWireContract(t *testing.T) {
	ts, _ := newTestServer(t)

	raw := getJSON(t, ts, "/v1/health", testToken)
	var health struct {
		BridgeVersion string `json:"bridge_version"`
		APIVersion    int    `json:"api_version"`
	}
	if err := json.Unmarshal(raw, &health); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if health.APIVersion != APIVersion {
		t.Errorf("api_version = %d, want %d", health.APIVersion, APIVersion)
	}
	if health.APIVersion <= 0 {
		t.Error("a version of zero means 'did not say', which the app treats as unknown")
	}
	if health.BridgeVersion == "" {
		t.Error("the human-readable version is missing too")
	}
}

// TestTwoTapsAtOncePressTheKeyOnce.
//
// The safety property behind every button in the product: the notification action and
// the card in the app are two entry points for one request, and a phone can deliver a
// single tap to both. Without an atomic claim, both callers see `pending`, both run
// the herdr command, and the agent's dialog receives Enter twice — the second one
// confirming whatever is highlighted after the first.
//
// The fake herdr is slow for the write on purpose, so the window in which that used
// to happen is wide rather than theoretical.
func TestTwoTapsAtOncePressTheKeyOnce(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": true, "agent": "codex", "display_agent": "codex",
		"agent_status": "blocked", "revision": 5,
	}
	// The shim must be installed *before* the core is built: the bridge resolves its
	// herdr binary once, at construction, so a later HERDR_BIN_PATH has no effect.
	explainDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(explainDir, "explain-local.txt"),
		[]byte("agent: codex\nstate: blocked\nmanifest: codex\nrule: live_strong_blocker\n"), 0o644); err != nil {
		t.Fatalf("write explain fixture: %v", err)
	}
	testherdr.Install(t, explainDir)

	fake, socketPath := startFakeHerdr(t, pane)
	ts, core := newTestServerWithSocket(t, socketPath)

	// Queue the request the way the daemon does when it reconciles a snapshot. The
	// subject here is what happens when two taps arrive at once, not how the request
	// got there, so the queue is seeded directly.
	blocked := herdr.StatusBlocked
	created := core.Queue().Reconcile([]herdr.Pane{{
		PaneID: "wX:p1", WorkspaceID: "wX", Agent: ptrTo("codex"), AgentStatus: blocked,
	}}, "local", nil, time.Now())
	if len(created.Created) != 1 {
		t.Fatalf("could not seed a request: %+v", created)
	}
	pending := core.Queue().List(false)
	if len(pending) != 1 {
		t.Fatalf("pending = %+v, want exactly one request", pending)
	}
	id := pending[0].ID

	// Two callers, at the same moment, for the same request.
	type result struct {
		status int
		body   map[string]string
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			status, body := call(t, ts, http.MethodPost, "/v1/decisions", testToken,
				`{"decision_id":"`+id+`","kind":"approve","target":"wX:p1"}`)
			results <- result{status: status, body: body}
		}()
	}
	close(start)
	first, second := <-results, <-results

	ok, conflict := 0, 0
	for _, r := range []result{first, second} {
		switch r.status {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
			if !strings.Contains(strings.ToLower(r.body["fix"]), "already") {
				t.Errorf("the second tap was refused without saying why: %v", r.body)
			}
		default:
			t.Errorf("unexpected status %d: %v", r.status, r.body)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("got %d accepted and %d refused, want exactly one of each", ok, conflict)
	}

	pressed := 0
	for _, method := range fake.methods() {
		if method == "agent.send_keys" {
			pressed++
		}
	}
	if pressed != 1 {
		t.Errorf("the key was pressed %d times, want exactly once", pressed)
	}
}

func ptrTo(value string) *string { return &value }

// TestAnActionAimedAtTheWrongSessionIsRefused.
//
// The bridge maps a request from the queue's record of which pane it belongs to, but
// executes the command the client asked for. Two sources for one address is one too
// many: a stale card, a bug, or a hand-written curl could have Enter pressed into a
// pane nobody was looking at. The queue wins, and the disagreement is refused.
func TestAnActionAimedAtTheWrongSessionIsRefused(t *testing.T) {
	pane := map[string]any{
		"pane_id": "wX:p1", "terminal_id": "term_x", "workspace_id": "wX",
		"tab_id": "wX:t1", "focused": true, "agent": "codex", "display_agent": "codex",
		"agent_status": "blocked", "revision": 5,
	}
	explainDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(explainDir, "explain-local.txt"),
		[]byte("agent: codex\nstate: blocked\nrule: live_strong_blocker\n"), 0o644); err != nil {
		t.Fatalf("write explain fixture: %v", err)
	}
	testherdr.Install(t, explainDir)

	fake, socketPath := startFakeHerdr(t, pane)
	ts, core := newTestServerWithSocket(t, socketPath)

	blocked := herdr.StatusBlocked
	created := core.Queue().Reconcile([]herdr.Pane{{
		PaneID: "wX:p1", WorkspaceID: "wX", Agent: ptrTo("codex"), AgentStatus: blocked,
	}}, "local", nil, time.Now())
	if len(created.Created) != 1 {
		t.Fatalf("could not seed a request: %+v", created)
	}
	id := created.Created[0].ID

	status, body := call(t, ts, http.MethodPost, "/v1/decisions", testToken,
		`{"decision_id":"`+id+`","kind":"approve","target":"wX:p9"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d (%v), want 400", status, body)
	}
	if !strings.Contains(body["error"], "wX:p1") || !strings.Contains(body["error"], "wX:p9") {
		t.Errorf("the refusal names neither session clearly: %v", body)
	}
	if body["fix"] == "" {
		t.Error("a refusal with no next step (FR-015)")
	}
	for _, method := range fake.methods() {
		if method == "agent.send_keys" {
			t.Errorf("keys were pressed despite the mismatch: %v", fake.methods())
		}
	}
	// And the request is still actionable: the claim was given back.
	if len(core.Queue().List(false)) != 1 {
		t.Errorf("the card disappeared after a refused action: %+v", core.Queue().List(false))
	}
}
