package herdr

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Protocol compatibility policy.
//
// herdr is pre-1.0 and the bundled schema already drifts from the published
// docs (local 0.9.1 vs website 0.9.3). The plan therefore pins the *shape* we
// depend on, not a version string, and fails loudly outside the window rather
// than misreading a changed field (spec FR-015: explain, do not fail silently).
const (
	// KnownGoodProtocol is the protocol verified on this machine.
	KnownGoodProtocol uint32 = 22
	// MinProtocol / MaxProtocol bound the accepted window.
	MinProtocol uint32 = 20
	MaxProtocol uint32 = 40
)

// Compat is the result of a compatibility check.
type Compat struct {
	Version  string
	Protocol uint32
	OK       bool
	// Reason is a human-actionable explanation; empty when OK.
	Reason string
}

// CheckCompat asks the server directly (via ping) rather than shelling out, so
// it also proves the socket is reachable and responsive.
func CheckCompat(ctx context.Context, c *Client) (Compat, error) {
	var pong Pong
	if err := c.CallInto(ctx, MethodPing, nil, &pong); err != nil {
		return Compat{}, err
	}
	out := Compat{Version: pong.Version, Protocol: pong.Protocol, OK: true}
	switch {
	case pong.Protocol < MinProtocol:
		out.OK = false
		out.Reason = "herdr protocol " + strconv.FormatUint(uint64(pong.Protocol), 10) +
			" is older than the supported minimum " + strconv.FormatUint(uint64(MinProtocol), 10) +
			". Upgrade herdr, then re-run: herdr update"
	case pong.Protocol > MaxProtocol:
		out.OK = false
		out.Reason = "herdr protocol " + strconv.FormatUint(uint64(pong.Protocol), 10) +
			" is newer than this bridge understands (max " + strconv.FormatUint(uint64(MaxProtocol), 10) +
			"). Update the Perch bridge."
	}
	return out, nil
}

// CheckCompatWithTimeout is the convenience form used by doctor and run.
func CheckCompatWithTimeout(c *Client) (Compat, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return CheckCompat(ctx, c)
}

// ParseProtocol extracts the protocol number from `herdr status` output. Kept
// separate from CheckCompat so doctor can cross-check the CLI's own view
// against the socket's.
func ParseProtocol(statusText string) (uint32, bool) {
	for _, line := range strings.Split(statusText, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) != "protocol" {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
		if err != nil {
			continue
		}
		return uint32(n), true
	}
	return 0, false
}
