// Package server exposes the bridge to the phone.
//
// The transport is whatever the user's own reachability path is — Tailscale, a
// LAN, a VPN. The product never requires a tunnel of its own (research §5), so
// this package deals only in HTTP and WebSocket on the host's own interfaces.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/th2zz/perch/bridge/internal/actions"
	"github.com/th2zz/perch/bridge/internal/core"
	"github.com/th2zz/perch/bridge/internal/queue"
	"github.com/th2zz/perch/bridge/internal/shell"
)

// DefaultAddr is a high, unlikely-to-collide port on all interfaces. Binding
// everywhere is deliberate: the phone reaches the host over the user's own
// network, and a loopback-only bind would make the product useless on a
// tailnet. The token is therefore mandatory, not optional.
const DefaultAddr = ":45999"

// Options configures the server.
type Options struct {
	Addr   string
	Token  string
	Logger *slog.Logger
	// Version is reported by /v1/health for support purposes.
	Version string
}

// Server is the HTTP surface.
type Server struct {
	core *core.Core
	opts Options
	// shells is the host's own terminal: a shell, not a pane. Created lazily by its manager the first
	// time somebody opens one, so a user who never does pays nothing (not even a process).
	shells *shell.Manager
	// stopping is closed when Run's context is cancelled, so an attached stream can end
	// because the bridge is stopping rather than wait for its socket to be killed. On
	// 2026-10-17 the handler had no such path, and a redeploy's stream ending was
	// indistinguishable in bridge.log from a phone that had gone away (#130).
	stopping chan struct{}

	// streamsMu guards closing and orders it against registerStream; streams counts the
	// attached streams so Run can return only after their goodbye lines are written.
	streamsMu sync.Mutex
	streams   sync.WaitGroup
	closing   bool
}

// New builds a server.
func New(c *core.Core, opts Options) (*Server, error) {
	if opts.Addr == "" {
		opts.Addr = DefaultAddr
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Token == "" {
		return nil, errors.New("refusing to start without a pairing token")
	}
	return &Server{
		core:     c,
		opts:     opts,
		shells:   shell.NewManager(shell.DefaultOptions()),
		stopping: make(chan struct{}),
	}, nil
}

// Addr returns the configured listen address.
func (s *Server) Addr() string { return s.opts.Addr }

// Handler builds the route table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.auth(s.handleHealth))
	mux.HandleFunc("/v1/snapshot", s.auth(s.handleSnapshot))
	mux.HandleFunc("/v1/decisions", s.auth(s.handleDecisions))
	mux.HandleFunc("/v1/value", s.auth(s.handleValue))
	mux.HandleFunc("/v1/session", s.auth(s.handleSession))
	mux.HandleFunc("/v1/session/keys", s.auth(s.handleSessionKeys))
	mux.HandleFunc("/v1/session/usage", s.auth(s.handleSessionUsage))
	mux.HandleFunc("/v1/session/conversation", s.auth(s.handleSessionConversation))
	mux.HandleFunc("/v1/session/terminal", s.auth(s.handleTerminal))
	mux.HandleFunc("/v1/terminal", s.auth(s.handleShell))
	mux.HandleFunc("/v1/harnesses", s.auth(s.handleHarnesses))
	mux.HandleFunc("/v1/folders", s.auth(s.handleFolders))
	mux.HandleFunc("/v1/projects", s.auth(s.handleCreateProject))
	mux.HandleFunc("/v1/sessions", s.auth(s.handleCreateSession))
	mux.HandleFunc("/v1/rename", s.auth(s.handleRename))
	mux.HandleFunc("/v1/close", s.auth(s.handleClose))
	mux.HandleFunc("/v1/push/register", s.auth(s.handleRegisterPush))
	mux.HandleFunc("/v1/feedback/notified", s.auth(s.handleNotified))
	mux.HandleFunc("/v1/feedback/startup", s.auth(s.handleStartup))
	mux.HandleFunc("/v1/feedback/terminal", s.auth(s.handleTerminalReport))
	mux.HandleFunc("/v1/feedback/screenshot", s.auth(s.handleScreenshot))
	mux.HandleFunc("/v1/stream", s.auth(s.handleStream))
	// Compression wraps the whole surface rather than being added route by route: one place to be right
	// about WebSocket upgrades, and nothing to remember when a new endpoint appears. See `gzipResponses`.
	return gzipResponses(mux)
}

// Run serves until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.opts.Addr, err)
	}
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// A WebSocket connection is long lived, so no global write timeout.
	}
	// goodbyesWritten closes once stopping streams have had their moment. Run waits on it
	// when the stop came from ctx, so "Run returned" promises the log already says which
	// streams this restart ended (#130's review: otherwise the process can win the race).
	goodbyesWritten := make(chan struct{})
	go func() {
		<-ctx.Done()
		// Before anything else: streams end by this signal and say so. Shutdown neither
		// closes nor waits for a hijacked connection, so without this the log would never
		// learn why a stream ended during a restart.
		s.stopStreams()
		s.waitStreams(time.Second)
		close(goodbyesWritten)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	s.opts.Logger.Info("bridge listening", "addr", ln.Addr().String())
	err = srv.Serve(ln)
	if ctx.Err() != nil {
		<-goodbyesWritten
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// registerStream counts one attached stream, and returns the release for it.
//
// A stream that arrives once the bridge is already stopping releases nothing: it ends by
// itself on the next turn of its loop and must not hold the process open.
func (s *Server) registerStream() (release func()) {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	if s.closing {
		return func() {}
	}
	s.streams.Add(1)
	return s.streams.Done
}

// stopStreams tells every attached stream to end and forbids new registrations, so
// waiting for them cannot be raced by a stream that arrived a moment too late.
func (s *Server) stopStreams() {
	s.streamsMu.Lock()
	s.closing = true
	s.streamsMu.Unlock()
	close(s.stopping)
}

// waitStreams gives attached streams up to d to write their ending. They end within
// microseconds of being told to, so d is a backstop against one wedged goroutine holding
// the process hostage, not a protocol timeout.
func (s *Server) waitStreams(d time.Duration) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.streams.Wait()
	}()
	select {
	case <-done:
	case <-time.After(d):
	}
}

// ---------------------------------------------------------------- middleware

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorised(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "unauthorised",
				"fix":   "re-pair the app: run `perch-bridge pair` on the host",
			})
			return
		}
		// Any authenticated request proves the phone can reach us, which is what
		// the health check reports back to the user — and it is counted, because a
		// runaway refresh loop is visible as a number long before it is visible as a
		// warm laptop (see internal/core/requests.go).
		s.core.MarkClientSeen()
		s.core.NoteClientRequest()
		next(w, r)
	}
}

func (s *Server) authorised(r *http.Request) bool {
	token := s.opts.Token
	if token == "" {
		return false
	}
	presented := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		presented = strings.TrimPrefix(h, "Bearer ")
	} else if q := r.URL.Query().Get("token"); q != "" {
		presented = q
	}
	if presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(token)) == 1
}

// ---------------------------------------------------------------- handlers

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Timing at debug level: this endpoint's cost was once a mystery (a consistent
	// hundred milliseconds that nothing in the work explained), and a line saying
	// how long the *work* took is what separates "our code is slow" from "the request
	// waited somewhere else".
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	health := s.core.CheckHealth(ctx)
	s.opts.Logger.Debug("health handled", "work_ms", time.Since(started).Milliseconds())
	writeJSON(w, http.StatusOK, healthEnvelope{
		BridgeVersion: s.opts.Version,
		APIVersion:    APIVersion,
		Health:        health,
	})
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	m := s.core.Mirror()
	// A snapshot request is also the moment to make sure the mirror is fresh:
	// the app pulls a snapshot on every reconnect, and the whole point is that
	// it never has to trust a stale cache.
	if fresh, err := s.core.RefreshOnce(ctx); err == nil {
		m = fresh
		// The same read is the authoritative moment for the queue. The app is about to
		// be told what is waiting, and that answer has to come from the read that
		// produced the workspace list beside it (data-model §3) — otherwise a bridge
		// whose event stream is down serves "nothing needs you" to a phone whose
		// agents are waiting.
		s.core.ReconcileQueueFrom(m)
	} else if m == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "cannot read the herdr session: " + err.Error(),
			"fix":   "start herdr, then run `perch-bridge doctor`",
		})
		return
	}

	decisions := s.core.Queue().List(true)
	writeJSON(w, http.StatusOK, snapshotEnvelope{Mirror: m, Decisions: decisions})
}

func (s *Server) handleDecisions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		includeClosed := r.URL.Query().Get("closed") == "1"
		writeJSON(w, http.StatusOK, decisionsEnvelope{Decisions: s.core.Queue().List(includeClosed)})
	case http.MethodPost:
		s.handleDecide(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use GET or POST"})
	}
}

func (s *Server) handleDecide(w http.ResponseWriter, r *http.Request) {
	var req actions.Request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "the request body must be {decision_id, kind, target, text}",
			"fix":   "only the four decision kinds are accepted",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	res, err := s.core.Decide(ctx, req)
	if err != nil {
		// Every failure carries a reason and a next step (FR-015).
		writeJSON(w, statusFor(err), map[string]string{
			"error": err.Error(),
			"fix":   fixFor(err),
		})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleValue(w http.ResponseWriter, r *http.Request) {
	stats := s.core.Queue().ValueStatsSince(time.Now().AddDate(0, 0, -7))
	writeJSON(w, http.StatusOK, valueEnvelope{Week: stats})
}

// handleSession is the escape hatch's read half: the recent output of one
// session, plus what the user is allowed to do with it.
//
// Read-only, and it stays that way: the writes it enables (approve, reject,
// deliver text) all go through /v1/decisions, so there is still exactly one
// place where a write can be expressed and exactly one place that enforces the
// four write classes.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET only"})
		return
	}
	paneID := strings.TrimSpace(r.URL.Query().Get("pane_id"))
	if paneID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "pane_id is required",
			"fix":   "open the session from a request in the queue",
		})
		return
	}
	lines := 0
	if raw := r.URL.Query().Get("lines"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			lines = parsed
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	// The host is optional and defaults to this machine, so every existing caller
	// (and every stored pairing) keeps working unchanged; naming another computer
	// is what makes a session address unambiguous (spec FR-016).
	view, err := s.core.Session(ctx, r.URL.Query().Get("host"), paneID, lines)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": err.Error(),
			"fix":   "run `perch-bridge doctor` on the host for details",
		})
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleSessionUsage reports what the harness itself knows about a session.
//
// A separate endpoint from /v1/session on purpose: this one reads the harness's own record
// file, which can be tens of megabytes, so it is asked for when somebody opens the panel and
// never on a poll.
func (s *Server) handleSessionUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET only"})
		return
	}
	paneID := strings.TrimSpace(r.URL.Query().Get("pane_id"))
	if paneID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "pane_id is required",
			"fix":   "open the session first, then ask for its usage",
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	facts, err := s.core.Usage(ctx, r.URL.Query().Get("host"), paneID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": err.Error(),
			"fix":   "run `perch-bridge doctor` on the host for details",
		})
		return
	}
	// No facts is not an error: a harness nobody has taught us to read reports an empty
	// object, and the app says so in its own words.
	writeJSON(w, http.StatusOK, facts)
}

// handleSessionConversation is the reading face: the conversation as the agent's own record has it.
//
// Polled while a session is open — that is what makes an answer arrive as it is written — which is
// why the read underneath it is incremental. A codex session's record was 51 MB after four days on
// this machine, and re-parsing that every 0.8 seconds is the one thing this endpoint must not do
// (records.codexConversation tails it instead and keeps the tail).
//
// Read-only, like the usage panel beside it, and for the same reason: nothing this product shows
// gets to be a second way to write to the user's computer.
func (s *Server) handleSessionConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET only"})
		return
	}
	paneID := strings.TrimSpace(r.URL.Query().Get("pane_id"))
	if paneID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "pane_id is required",
			"fix":   "open the session first, then read its conversation",
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	view, err := s.core.Conversation(ctx, r.URL.Query().Get("host"), paneID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": err.Error(),
			"fix":   "run `perch-bridge doctor` on the host for details",
		})
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleSessionKeys is the escape hatch's key bar.
//
// It is the only write endpoint that is not the four decision classes, so it is
// kept as narrow as possible: a fixed vocabulary of terminal keys, at most four at
// once, and no way to reach it except by deliberately opening a session. The
// validation lives in the actions package next to the allow-list, so the contract
// and its enforcement are one file.
//
// **Nothing in the app calls this any more** (since D34 the phone's key row sends bytes into the real
// terminal, `/v1/session/terminal`), and it was on the list to delete on 2026-10-15. Counting the
// references first is what stopped that: `scripts/verify-hosts.sh` drives it as the *remote write probe*
// — press Esc on a pane on another machine and read `^[` back, which is the strongest available
// statement that a phone can act on a computer that is not this one — and `BridgeIntegrationTest`
// uses it to watch the bridge refuse a key it does not know. So it is not dead code; it is an API
// with no screen and one real verifier. If it is ever retired, the probe should move to the terminal
// socket first, because that is the path the product actually uses.
func (s *Server) handleSessionKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	var body struct {
		PaneID string   `json:"pane_id"`
		Keys   []string `json:"keys"`
		Host   string   `json:"host"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "expected {pane_id, keys}",
			"fix":   "reopen the session and use the keys offered there",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	equivalent, err := s.core.PressKeys(ctx, body.Host, body.PaneID, body.Keys)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
			// The only ways to arrive here are an app bug or a key herdr itself
			// refuses, and neither is fixable from a phone. Saying that beats a
			// cheerful "try again" that cannot work (spec FR-015) — and the honest
			// fallback is the one this product keeps coming back to: the computer.
			"fix": "nothing was sent; use your computer for this session",
		})
		return
	}
	writeJSON(w, http.StatusOK, keysEnvelope{Command: equivalent})
}

// handleRegisterPush accepts the phone's notification token.
//
// This is the only handshake the push path needs, and it is deliberately
// unidirectional: the token travels from the phone to the host, which then
// includes it in a summary sent to the gateway. The phone never talks to the
// gateway, and the gateway never learns which host or project the token belongs
// to beyond the summary it is delivering.
func (s *Server) handleRegisterPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	var body struct {
		DeviceToken string `json:"device_token"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "expected {device_token}",
			"fix":   "send the token your notification service gave you",
		})
		return
	}
	if strings.TrimSpace(body.DeviceToken) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "device_token is empty",
			"fix":   "the app must send a token before notifications can be delivered",
		})
		return
	}

	cfg, err := s.core.PushConfig()
	if err != nil {
		cfg = core.PushConfig{}
	}
	cfg.DeviceToken = body.DeviceToken
	if err := s.core.SavePushConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
			"fix":   "check that the state directory is writable",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"registered":      true,
		"push_configured": s.core.PushConfigured(),
		"note":            "notifications will start once a gateway is configured with `perch-bridge pair --with-push`",
	})
}

// handleNotified records that the phone actually showed the notification.
//
// This is the second half of the only measurement the product cannot take by
// itself: the bridge knows what it sent and when, but only the phone knows when
// it appeared. It is a report, not a request — the phone sends it best-effort
// and nothing depends on the answer.
func (s *Server) handleNotified(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	var body struct {
		DecisionID string `json:"decision_id"`
		ShownAfter int64  `json:"shown_after_ms"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || strings.TrimSpace(body.DecisionID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "expected {decision_id, shown_after_ms}",
			"fix":   "this endpoint only records what the phone already did",
		})
		return
	}
	// A negative or absurd figure would be a clock or unit mistake, and letting it
	// in would silently poison the very numbers the product is judged by.
	if body.ShownAfter < 0 || body.ShownAfter > 24*60*60*1000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "shown_after_ms must be a plausible millisecond duration",
			"fix":   "send the elapsed time between receiving the event and showing it",
		})
		return
	}

	known := s.core.NoteNotificationShown(body.DecisionID, body.ShownAfter)
	writeJSON(w, http.StatusOK, map[string]any{"recorded": known})
}

// handleStartup records how long the phone took to show its first screen.
//
// SC-007 promises the queue is on screen within two seconds of a cold start, and
// the only clock that can measure that is the phone's: from the process starting
// to the first frame the user can act on. Same shape as the notification report,
// and for the same reason — a promise about time needs a measurement, not an
// impression.
func (s *Server) handleStartup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	var body struct {
		StartupMillis int64 `json:"startup_ms"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "expected {startup_ms}",
			"fix":   "the app reports this itself; nothing for you to send",
		})
		return
	}
	// A figure outside a plausible range is a unit or clock mistake. Letting it in
	// would poison the one number that says whether the app feels fast.
	if body.StartupMillis <= 0 || body.StartupMillis > 120_000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "startup_ms must be a plausible millisecond duration",
			"fix":   "send the time from process start to the first usable screen",
		})
		return
	}
	s.core.NoteColdStart(body.StartupMillis)
	writeJSON(w, http.StatusOK, map[string]any{"recorded": true})
}

func (s *Server) handleTerminalReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	var body struct {
		PaneID string `json:"pane_id"`
		Event  string `json:"event"`
		Detail string `json:"detail"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "expected {pane_id, event, detail}",
			"fix":   "the app reports this itself; nothing for you to send",
		})
		return
	}
	s.opts.Logger.Info("terminal report",
		"pane", oneLine(body.PaneID, 40),
		"event", oneLine(body.Event, 40),
		"detail", oneLine(body.Detail, 240),
	)
	writeJSON(w, http.StatusOK, map[string]any{"recorded": true})
}

// oneLine is a string that is safe to put in a log line: one line, bounded, no control characters.
func oneLine(value string, limit int) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, value)
	cleaned = strings.TrimSpace(cleaned)
	if len(cleaned) > limit {
		// Byte-slicing a UTF-8 string can split a character; the log does not need the tail exactly.
		cleaned = strings.ToValidUTF8(cleaned[:limit], "") + "…"
	}
	return cleaned
}

func statusFor(err error) int {
	var setup *core.HarnessSetupError
	if errors.As(err, &setup) {
		return http.StatusBadRequest
	}
	switch {
	case errors.Is(err, actions.ErrUnknownKind),
		errors.Is(err, actions.ErrMissingText),
		errors.Is(err, actions.ErrUnexpectedText),
		errors.Is(err, actions.ErrMissingTarget),
		errors.Is(err, actions.ErrTargetMismatch):
		return http.StatusBadRequest
	case errors.Is(err, queue.ErrNotPending), errors.Is(err, queue.ErrInFlight):
		return http.StatusConflict
	case errors.Is(err, queue.ErrUnknownDecision):
		// 404, not 500: the bridge is fine, the phone's view is old.
		return http.StatusNotFound
	case errors.Is(err, actions.ErrNoDeterministicKeys):
		return http.StatusUnprocessableEntity
	case errors.Is(err, core.ErrHostUnavailable):
		// The phone named a computer this one does not have: a mismatch between
		// two views, so the client can fix it by refreshing rather than a bridge
		// failure it cannot act on.
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func fixFor(err error) string {
	var setup *core.HarnessSetupError
	if errors.As(err, &setup) {
		return setup.FixText()
	}
	switch {
	case errors.Is(err, queue.ErrNotPending):
		return "this request was already resolved, most likely on the computer"
	case errors.Is(err, queue.ErrInFlight):
		// Nothing is wrong and nothing needs doing: the earlier tap is on its way.
		return "your earlier tap is already being handled; the queue will update by itself"
	case errors.Is(err, queue.ErrUnknownDecision):
		return "refresh the queue; this request is no longer open"
	case errors.Is(err, actions.ErrNoDeterministicKeys):
		return "open the session's escape hatch and answer there"
	case errors.Is(err, actions.ErrNoAgent):
		return "herdr does not detect an agent in this session; send text instead"
	case errors.Is(err, actions.ErrUnknownKind):
		return "only approve, reject, answer and prompt are accepted; update Perch if this came from the app"
	case errors.Is(err, actions.ErrMissingText):
		return "type the reply before sending it"
	case errors.Is(err, actions.ErrUnexpectedText):
		return "approve and reject take no text; this looks like a client bug, please report it"
	case errors.Is(err, actions.ErrMissingTarget):
		return "reopen the request from the queue so the target is attached"
	case errors.Is(err, actions.ErrTargetMismatch):
		return "refresh the queue: that card is out of date, and this action would have gone to the wrong session"
	case errors.Is(err, core.ErrHostUnavailable):
		return "refresh the queue; that computer is not saved on this machine (add it with `herdr machine add`)"
	default:
		return "run `perch-bridge doctor` on the host for details"
	}
}

// ---------------------------------------------------------------- stream

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrade(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
			"fix":   "the stream endpoint requires a WebSocket upgrade",
		})
		return
	}
	defer func() { _ = conn.Close() }()

	// A stream Run knows about: it waits (bounded) for registered streams before
	// returning, so a restart's goodbye lines are written before the process is gone.
	release := s.registerStream()
	defer release()

	// #130: a phone that never opened a stream and a stream that is attached but
	// ignored look the same from the phone, and until 2026-10-17 bridge.log said
	// nothing about either. One line at attach and one at the exit, with the reason,
	// is what makes the next "not connected" report diagnosable.
	s.opts.Logger.Info("event stream attached", "remote", r.RemoteAddr)

	events, cancel := s.core.Watch()
	defer cancel()

	// Greet with the current state so a reconnecting client is immediately
	// consistent instead of waiting for the next change.
	if m := s.core.Mirror(); m != nil {
		_ = conn.WriteText(mustJSON(core.Event{Type: core.EventSnapshot, Mirror: m}))
	}

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	// The client is not expected to send anything, but we must read to notice a
	// close and to keep the connection honest.
	readErr := make(chan error, 1)
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				readErr <- err
				return
			}
		}
	}()

	// Every way out of this loop has to name a reason: the line below is the only
	// record of the exit, and an exit nobody recorded is the bug this exists for.
	//
	// r.Context() is deliberately not selected on: after an upgrade, net/http cancels
	// it from inside this handler's own read when the client dies, so it can only fire
	// at the same moment as readErr with less to say — its first version logged "the
	// server is stopping" for a phone that had simply gone away.
	reason := ""
	for reason == "" {
		select {
		case <-s.stopping:
			reason = "the bridge is stopping"
		case err := <-readErr:
			reason = "client closed: " + err.Error()
		case ev, ok := <-events:
			if !ok {
				reason = "the event feed closed"
			} else if err := conn.WriteText(mustJSON(ev)); err != nil {
				reason = writeFailureReason("write failed", err, readErr)
			}
		case <-heartbeat.C:
			if err := conn.WriteText(mustJSON(core.Event{Type: core.EventHeartbeat})); err != nil {
				reason = writeFailureReason("heartbeat failed", err, readErr)
			}
		}
	}
	s.opts.Logger.Info("event stream ended", "remote", r.RemoteAddr, "reason", reason)
}

// writeFailureReason names a write that failed, asking the reader first.
//
// A phone that has just gone away fails this write and the reader's read in the same
// moment, and a select with two ready channels decides between them by coin toss — which
// is how "write failed: broken pipe" could stand in for "client closed: EOF", the race
// #130's review named. The reader's error is the honest headline; the write error is its
// shadow. A phone that is alive but not reading says nothing to the reader, and then the
// write error is the whole story. The wait is one scheduler hop, not a protocol timeout:
// it decides nothing about the stream, only how its ending is described.
func writeFailureReason(what string, err error, readErr <-chan error) string {
	select {
	case rerr := <-readErr:
		return "client closed: " + rerr.Error()
	case <-time.After(50 * time.Millisecond):
		return what + ": " + err.Error()
	}
}

// ---------------------------------------------------------------- helpers

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"type":"error"}`)
	}
	return data
}
