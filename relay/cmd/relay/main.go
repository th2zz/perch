// Command relay is the Perch push gateway.
//
// Why this exists at all: sending an FCM message requires the product's
// service-account key, and copying that key onto every user's machine would let
// any single user push notifications to every other user. So the sending side is
// centralised, while the bridge — which holds the actual content — never leaves
// the user's computer.
//
// It is deliberately the least interesting service in the system:
//
//   - stateless: no database, no history, no query API;
//   - content-blind by construction: the payload type has no field for code,
//     terminal output or prompt text, and unknown fields are rejected;
//   - small enough to read in one sitting, which is the point.
//
// During development it runs anywhere, including the same machine as the
// bridge, because delivery depends on the phone's vendor channel rather than on
// where the message was sent from.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/th2zz/perch/relay/internal/push"
)

var version = "0.1.0-dev"

type server struct {
	token  string
	sender *push.FCMSender
	dev    bool
	log    *slog.Logger

	sent   atomic.Int64
	failed atomic.Int64
}

func main() {
	addr := flag.String("addr", envOr("PERCH_RELAY_ADDR", ":45998"), "listen address")
	keyPath := flag.String("service-account", os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
		"path to an FCM service-account JSON key")
	dryRun := flag.Bool("dry-run", os.Getenv("PERCH_RELAY_DRY_RUN") == "1",
		"log notifications instead of sending them (development)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	token := os.Getenv("PERCH_RELAY_TOKEN")
	if token == "" {
		logger.Error("PERCH_RELAY_TOKEN is required: hosts authenticate with it")
		os.Exit(2)
	}

	s := &server{token: token, dev: *dryRun, log: logger}
	if *keyPath != "" {
		data, err := os.ReadFile(*keyPath)
		if err != nil {
			logger.Error("cannot read service account", "path", *keyPath, "err", err)
			os.Exit(2)
		}
		sender, err := push.NewFCMSender(data)
		if err != nil {
			logger.Error("cannot use service account", "err", err)
			os.Exit(2)
		}
		s.sender = sender
		logger.Info("sending via FCM", "project", sender.ProjectID())
	} else {
		s.dev = true
		logger.Warn("no service account configured; running in dry-run mode, notifications will be logged, not delivered")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/push", s.handlePush)
	mux.HandleFunc("/healthz", s.handleHealth)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      20 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		logger.Error("listen failed", "addr", *addr, "err", err)
		os.Exit(1)
	}
	logger.Info("push gateway listening", "addr", ln.Addr().String(), "version", version)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("serve failed", "err", err)
		os.Exit(1)
	}
}

func (s *server) handlePush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	if !s.authorised(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorised"})
		return
	}

	// One byte over the limit, so "too large" is a fact about the request rather
	// than an assumption. The hand-rolled read this replaces treated any error —
	// including a connection that failed halfway — as the end of the body.
	body, err := io.ReadAll(io.LimitReader(r.Body, push.MaxSummaryBytes+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "could not read the request body: " + err.Error(),
		})
		return
	}
	if len(body) > push.MaxSummaryBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": "payload too large for a summary",
		})
		return
	}

	summary, err := push.Decode(body)
	if err != nil {
		// Deliberately echo the reason: a host sending the wrong shape is a
		// configuration problem, and the message names the offending field
		// without ever containing user content.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	note := push.Render(summary)
	if s.dev || s.sender == nil {
		s.log.Info("dry-run notification",
			"decision", summary.DecisionID, "host", summary.HostLabel,
			"project", summary.WorkspaceLabel, "agent", summary.Agent, "kind", summary.Kind,
			"title", note.Title)
		s.sent.Add(1)
		writeJSON(w, http.StatusOK, map[string]string{"status": "dry-run"})
		return
	}

	// Not r.Context(): if the host disconnects mid-delivery, the notification is
	// still worth sending. The user asked to be told; whether the host is still
	// listening is not part of that question.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
	defer cancel()
	if err := s.sender.Send(ctx, summary.DeviceToken, note); err != nil {
		s.failed.Add(1)
		s.log.Error("delivery failed", "decision", summary.DecisionID, "err", err)
		// 502 rather than 500: the failure is upstream, and the host should treat
		// it as retryable rather than as a bug in its own payload.
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	s.sent.Add(1)
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": version,
		"dry_run": s.dev || s.sender == nil,
		"sent":    s.sent.Load(),
		"failed":  s.failed.Load(),
	})
}

func (s *server) authorised(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return false
	}
	presented := strings.TrimPrefix(h, "Bearer ")
	return subtle.ConstantTimeCompare([]byte(presented), []byte(s.token)) == 1
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var _ = fmt.Sprintf
