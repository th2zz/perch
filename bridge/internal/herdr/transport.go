package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// The wire format is newline-delimited JSON in both directions, verified
// against herdr 0.9.1 by lab/bridge-probe.
//
// Request:  {"id": "...", "method": "...", "params": {...}}
// Success:  {"id": "...", "result": {"type": "...", ...}}
// Failure:  {"id": "...", "error": {"code": "...", "message": "..."}}

// EnvSocketPath / EnvBinPath are injected into plugin processes by herdr. The
// product must never hardcode the socket path (bridge-plugin contract §2).
const (
	EnvSocketPath = "HERDR_SOCKET_PATH"
	EnvBinPath    = "HERDR_BIN_PATH"
)

// DefaultSocketPath is only a fallback for CLI invocations made outside a
// herdr-spawned process.
func DefaultSocketPath() string {
	if p := os.Getenv(EnvSocketPath); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Program + ".sock"
	}
	return SocketPath(home)
}

// Error is a structured error returned by the herdr server.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// ErrNoEndpoints is returned when the socket is unreachable.
var ErrNoEndpoints = errors.New("herdr socket unreachable")

type request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
	Event  string          `json:"event"`
	Data   json.RawMessage `json:"data"`
}

// Client speaks the herdr socket API.
//
// Measured fact, 2026-10-13: herdr answers **exactly one request per connection
// and then closes it**. Sending a second request on the same connection fails with
// a broken pipe, for `ping`, `session.snapshot`, `pane.list` and `agent.list`
// alike. The first version of this file assumed a reusable connection — the usual
// shape for a request/response protocol — and it "worked" only because a
// retry-on-broken-connection path quietly reconnected every time. That cost a
// failed write per call, and, worse, it meant a request could be sent twice.
//
// So: one connection per call, and no retry once a request has been written.
// Connections are cheap on a Unix socket; a duplicated `agent.send_keys` is not.
type Client struct {
	path string

	// seq only names requests; it is atomic so calls can run concurrently.
	seq atomic.Uint64
}

// NewClient returns a lazy client for the given socket path. Pass "" to use
// DefaultSocketPath.
func NewClient(socketPath string) *Client {
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}
	return &Client{path: socketPath}
}

// Path returns the socket path this client talks to.
func (c *Client) Path() string { return c.path }

func (c *Client) dial() (net.Conn, *bufio.Reader, error) {
	conn, err := net.DialTimeout("unix", c.path, 5*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s: %w", ErrNoEndpoints, c.path, err)
	}
	return conn, bufio.NewReaderSize(conn, 1<<20), nil
}

// Close exists so callers can defer it. Each call owns its connection and closes
// it, so there is nothing left to release.
func (c *Client) Close() error { return nil }

// Call performs one request/response round trip on a connection of its own.
//
// The one thing this deliberately does not do is retry after a request has been
// written. Every write this product performs is a keystroke or a prompt to
// somebody's agent, and "the connection died after we sent it" cannot be told
// apart from "the connection died before they acted on it" — so a retry would
// sometimes press Enter twice. Failing, and telling the user, is the recoverable
// half of that trade.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if params == nil {
		params = struct{}{}
	}
	conn, reader, err := c.dial()
	if err != nil {
		// Only a connection that never opened is worth trying again: nothing was
		// sent, so a retry cannot duplicate anything. This is the case where herdr
		// is restarting or the socket is momentarily gone.
		if conn, reader, err = c.dial(); err != nil {
			return nil, err
		}
	}
	defer func() { _ = conn.Close() }()

	raw, err := c.exchange(ctx, conn, reader, method, params)
	return raw, err
}

func (c *Client) exchange(
	ctx context.Context,
	conn net.Conn,
	reader *bufio.Reader,
	method string,
	params any,
) (json.RawMessage, error) {
	id := "perch-" + strconv.FormatUint(c.seq.Add(1), 10)

	body, err := json.Marshal(request{ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	body = append(body, '\n')

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	if _, err := conn.Write(body); err != nil {
		return nil, fmt.Errorf("write %s: %w", method, err)
	}

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("call %s: connection closed by server", method)
			}
			return nil, fmt.Errorf("read %s: %w", method, err)
		}
		var resp response
		if err := json.Unmarshal(line, &resp); err != nil {
			return nil, fmt.Errorf("decode %s response: %w", method, err)
		}
		if resp.ID != id {
			// Unsolicited event frame on a call connection; ignore rather than
			// desynchronise. Subscriptions use their own connection.
			continue
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// CallInto performs a call and decodes result into out.
func (c *Client) CallInto(ctx context.Context, method string, params, out any) error {
	raw, err := c.Call(ctx, method, params)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s result: %w", method, err)
	}
	return nil
}

// ---------------------------------------------------------------- subscriptions

// Subscription mirrors the schema's Subscription union. Only the fields the
// bridge actually uses are modelled.
//
// Empirical constraint (lab/bridge-probe, confirmed against the 0.9.1 schema):
// pane.agent_status_changed is REJECTED without pane_id, while lifecycle events
// such as pane.created are accepted globally. The reconciler therefore
// subscribes once per pane for status and once globally for lifecycle.
type Subscription struct {
	Type        string `json:"type"`
	PaneID      string `json:"pane_id,omitempty"`
	AgentStatus string `json:"agent_status,omitempty"`
}

// Event is one pushed frame.
//
// Two shapes exist in the schema and both are handled:
//   - subscription_event (events.subscribe stream): kinds are limited to
//     pane.agent_status_changed / pane.output_matched / pane.scroll_changed.
//   - event (events.wait results, plugin [[events]] hooks): underscore kinds
//     such as pane_agent_status_changed.
//
// Because the submittable set of subscription kinds is wider than the
// documented delivery set, the reconciler never treats events as the source of
// truth for the pane inventory: lifecycle hints trigger a re-snapshot instead.
type Event struct {
	Kind       string
	Data       json.RawMessage
	ReceivedAt time.Time
}

// Stream is a live event subscription on its own connection.
type Stream struct {
	conn   net.Conn
	events chan Event
	errs   chan error

	closeOnce sync.Once
	done      chan struct{}
}

// NewStream wraps two channels as a stream.
//
// The socket client builds one from a connection, and this is how anything else can
// build one: a transport that reaches herdr another way (the remote-host reader, a
// future embedded one), and tests that need the reconciler's reconnection behaviour
// without a live server to break. `conn` may be nil, in which case Close only ends
// the stream.
// The channels are deliberately not owned: the caller closes them, which is what
// "the stream ended" means to the reader.
func NewStream(events chan Event, errs chan error) *Stream {
	return &Stream{events: events, errs: errs, done: make(chan struct{})}
}

// Events is the delivery channel. It is closed when the stream ends.
func (s *Stream) Events() <-chan Event { return s.events }

// Err yields at most one terminal error, then is closed.
func (s *Stream) Err() <-chan error { return s.errs }

// Close terminates the stream and its reader goroutine.
func (s *Stream) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.done)
		if s.conn != nil {
			err = s.conn.Close()
		}
	})
	return err
}

// Subscribe opens a dedicated connection, sends events.subscribe, waits for the
// subscription_started acknowledgement, then streams events.
//
// Waiting for the ack matters: it is the only proof that the subscription is
// live, so the caller can order "snapshot, then subscribe" without a window in
// which a status change is lost unnoticed.
func (c *Client) Subscribe(ctx context.Context, subs []Subscription) (*Stream, error) {
	conn, err := net.DialTimeout("unix", c.path, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrNoEndpoints, c.path, err)
	}

	id := "perch-sub-" + strconv.FormatUint(c.seq.Add(1), 10)

	body, err := json.Marshal(request{
		ID:     id,
		Method: MethodEventsSubscribe,
		Params: map[string]any{"subscriptions": subs},
	})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	body = append(body, '\n')

	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write(body); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("subscribe write: %w", err)
	}

	r := bufio.NewReaderSize(conn, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("subscribe ack: %w", err)
		}
		var resp response
		if err := json.Unmarshal(line, &resp); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("subscribe ack decode: %w", err)
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			_ = conn.Close()
			return nil, resp.Error
		}
		break
	}
	_ = conn.SetDeadline(time.Time{})

	s := &Stream{
		conn:   conn,
		events: make(chan Event, 256),
		errs:   make(chan error, 1),
		done:   make(chan struct{}),
	}
	go s.read(r)
	return s, nil
}

func (s *Stream) read(r *bufio.Reader) {
	defer close(s.events)
	defer close(s.errs)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			select {
			case <-s.done:
			default:
				s.errs <- fmt.Errorf("event stream ended: %w", err)
			}
			return
		}
		var resp response
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if resp.Error != nil {
			s.errs <- resp.Error
			return
		}
		if resp.Event == "" {
			continue
		}
		ev := Event{Kind: resp.Event, Data: resp.Data, ReceivedAt: time.Now()}
		select {
		case s.events <- ev:
		case <-s.done:
			return
		}
	}
}
