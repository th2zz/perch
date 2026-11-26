package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The phone's link is the one part of this system nobody controls: a body that can be sent in a tenth of the
// bytes should be, and a WebSocket must never be wrapped in a compressor.
func TestResponsesAreCompressedOnlyWhenAskedAndNeverForWebSockets(t *testing.T) {
	body := strings.Repeat(`{"detail":"screen text that compresses well"},`, 200)
	handler := gzipResponses(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))

	// A client that asks for gzip gets gzip, and a client that does not, does not.
	asked := httptest.NewRequest(http.MethodGet, "/v1/snapshot", nil)
	asked.Header.Set("Accept-Encoding", "gzip, deflate")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, asked)
	if got := recorder.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("a client that asked for gzip got Content-Encoding %q", got)
	}
	if !strings.Contains(recorder.Header().Get("Vary"), "Accept-Encoding") {
		t.Error("a compressed answer must say it depends on Accept-Encoding")
	}
	plain, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatalf("the body is not gzip: %v", err)
	}
	decoded, _ := io.ReadAll(plain)
	if string(decoded) != body {
		t.Errorf("the decoded body is not what the handler wrote (%d vs %d bytes)", len(decoded), len(body))
	}
	if recorder.Body.Len() >= len(body)/2 {
		t.Errorf("compression saved almost nothing: %d bytes against %d", recorder.Body.Len(), len(body))
	}

	plainRecorder := httptest.NewRecorder()
	handler.ServeHTTP(plainRecorder, httptest.NewRequest(http.MethodGet, "/v1/snapshot", nil))
	if got := plainRecorder.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("a client that did not ask for gzip got Content-Encoding %q", got)
	}
	if plainRecorder.Body.String() != body {
		t.Error("a client that did not ask for gzip got a body it did not expect")
	}

	// An upgrade is passed through untouched — this is the terminal, and compressing it would break it.
	upgrade := httptest.NewRequest(http.MethodGet, "/v1/session/terminal", nil)
	upgrade.Header.Set("Accept-Encoding", "gzip")
	upgrade.Header.Set("Connection", "Upgrade")
	upgrade.Header.Set("Upgrade", "websocket")
	upgradeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(upgradeRecorder, upgrade)
	if got := upgradeRecorder.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("a WebSocket upgrade was compressed (Content-Encoding %q); the terminal would break", got)
	}
}
