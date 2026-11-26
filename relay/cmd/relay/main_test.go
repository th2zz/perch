package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The gateway has exactly one endpoint and it is the one that runs in production,
// so it is worth more than the payload parsing tests alone: authentication,
// method, size, and the refusal of anything carrying content all have to hold at
// the HTTP boundary, not only inside a decoder.

func newTestServer() (*server, *httptest.Server) {
	s := &server{
		token: "relay-token",
		dev:   true, // dry-run, so nothing tries to reach FCM
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/push", s.handlePush)
	mux.HandleFunc("/healthz", s.handleHealth)
	return s, httptest.NewServer(mux)
}

func post(t *testing.T, ts *httptest.Server, token, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/push", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode, string(payload)
}

const goodSummary = `{"device_token":"tok","decision_id":"wA:p1@1","host_label":"dev-laptop",` +
	`"workspace_label":"mobile agent","agent":"codex","kind":"approve","created_at":1}`

func TestOnlyAuthorisedHostsMayPush(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	for _, token := range []string{"", "wrong-token"} {
		status, _ := post(t, ts, token, goodSummary)
		if status != http.StatusUnauthorized {
			t.Errorf("token %q = %d, want 401", token, status)
		}
	}
}

func TestAValidSummaryIsAccepted(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	status, body := post(t, ts, "relay-token", goodSummary)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, body)
	}
	// In dry-run there is no FCM call, and the response says which mode it was in
	// so a developer is never left wondering whether anything was delivered.
	if !strings.Contains(body, "dry-run") {
		t.Errorf("body = %s", body)
	}
}

func TestContentBearingPayloadsAreRefusedAtTheBoundary(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	withText := strings.Replace(goodSummary, `"created_at":1`,
		`"created_at":1,"text":"rm -rf / and the user's code"`, 1)
	status, body := post(t, ts, "relay-token", withText)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if !strings.Contains(body, "not allowed") {
		t.Errorf("the refusal should name the problem: %s", body)
	}
	// And it must not echo the content back, which would put the user's text into
	// our logs and into the host's error handling.
	if strings.Contains(body, "rm -rf") {
		t.Errorf("the refusal echoed the content it rejected: %s", body)
	}
}

func TestAnOversizedBodyIsRefusedWithoutReadingItAll(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	huge := `{"device_token":"` + strings.Repeat("a", 8<<10) + `"}`
	status, _ := post(t, ts, "relay-token", huge)
	if status != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", status)
	}
}

func TestHealthReportsTheMode(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["status"] != "ok" {
		t.Errorf("status = %v", decoded["status"])
	}
	// dry_run must be honest: a gateway with no service account cannot deliver, and
	// anything reading this should be able to tell that without guessing.
	if dry, _ := decoded["dry_run"].(bool); !dry {
		t.Errorf("dry_run = %v, want true with no service account", decoded["dry_run"])
	}
}
