package herdr

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CLI wraps the herdr executable. The socket API covers almost everything the
// bridge needs; the CLI is used only where the server exposes no equivalent —
// currently `agent explain` (detection-rule introspection) and `status`
// (endpoint compatibility cross-check).
//
// The binary path comes from HERDR_BIN_PATH when available so we never guess
// where herdr lives (bridge-plugin contract §2).
type CLI struct {
	Bin string
	// Timeout bounds a single invocation; CLI calls are on the user-facing path
	// (doctor, decision submission) and must not hang the app.
	Timeout time.Duration
}

// NewCLI resolves the executable to use.
func NewCLI() *CLI {
	bin := os.Getenv(EnvBinPath)
	if bin == "" {
		bin = Program
	}
	return &CLI{Bin: bin, Timeout: 20 * time.Second}
}

// ErrCLIMissing reports that no herdr executable could be resolved.
var ErrCLIMissing = errors.New("herdr executable not found; set " + EnvBinPath)

// Run executes herdr with args and returns stdout. Stderr is folded into the
// error so failures stay explainable to the user (spec FR-015).
func (c *CLI) Run(ctx context.Context, args ...string) (string, error) {
	if c.Bin == "" {
		return "", ErrCLIMissing
	}
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = nil
	cmd.Env = os.Environ()
	// Detach from any spanning terminal so a TUI never attaches to our pipes.
	cmd.Env = append(cmd.Env, "HERDR_ENV=1")

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), errors.New("herdr " + strings.Join(args, " ") + ": " + msg)
	}
	return stdout.String(), nil
}

// RuntimeStatus is the subset of `herdr status` the product cares about.
type RuntimeStatus struct {
	ClientVersion       string
	ClientProtocol      uint32
	ClientProtocolSet   bool
	ServerStatus        string
	ServerVersion       string
	EndpointCompatible  *bool
	PrivateProtocol     uint32
	PrivateProtocolOK   *bool
	Socket              string
	RestartNeeded       *bool
	ServerBinaryIsStale *bool
}

// Status runs `herdr status` and parses it.
func (c *CLI) Status(ctx context.Context) (*RuntimeStatus, error) {
	text, err := c.Run(ctx, VerbStatus)
	if err != nil {
		return nil, err
	}
	return parseStatus(text), nil
}

func parseStatus(text string) *RuntimeStatus {
	st := &RuntimeStatus{}
	section := ""
	for _, raw := range strings.Split(text, "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indented := raw != strings.TrimLeft(raw, " \t")
		key, value, ok := strings.Cut(strings.TrimSpace(raw), ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !indented {
			section = key
			continue
		}
		switch section {
		case "client":
			switch key {
			case "version":
				st.ClientVersion = value
			case "protocol":
				if n, ok := atou32(value); ok {
					st.ClientProtocol, st.ClientProtocolSet = n, true
				}
			}
		case "server":
			switch key {
			case "status":
				st.ServerStatus = value
			case "version":
				st.ServerVersion = value
			case "endpoint_compatible":
				st.EndpointCompatible = parseYesNo(value)
			case "private_protocol":
				if n, ok := atou32(value); ok {
					st.PrivateProtocol = n
				}
			case "private_protocol_compatible":
				st.PrivateProtocolOK = parseYesNo(value)
			case "socket":
				st.Socket = value
			}
		case "update":
			switch key {
			case "restart_needed":
				st.RestartNeeded = parseYesNo(value)
			case "server_binary_stale":
				st.ServerBinaryIsStale = parseYesNo(value)
			}
		}
	}
	return st
}

func parseYesNo(v string) *bool {
	switch strings.ToLower(v) {
	case "yes", "true":
		b := true
		return &b
	case "no", "false":
		b := false
		return &b
	}
	return nil
}

func atou32(v string) (uint32, bool) {
	var n uint64
	if v == "" {
		return 0, false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + uint64(r-'0')
		if n > 1<<32 {
			return 0, false
		}
	}
	return uint32(n), true
}

// Detection is the parsed output of `herdr agent explain`. It is the evidence
// base for action equivalence: herdr tells us which detection rule matched, and
// the rule identifies the on-screen interaction, which is what determines the
// deterministic key sequence.
type Detection struct {
	Agent      string
	State      AgentStatus
	Manifest   string
	Rule       string // rule id, without the parenthesised detail
	RuleDetail string
	Evidence   string
}

// Explain runs `herdr agent explain <target>`.
func (c *CLI) Explain(ctx context.Context, target string) (*Detection, error) {
	text, err := c.Run(ctx, GroupAgent, VerbExplain, target)
	if err != nil {
		return nil, err
	}
	return ParseExplain(text), nil
}

// ParseExplain parses the explain output; it is exported for tests.
func ParseExplain(text string) *Detection {
	d := &Detection{}
	for _, raw := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(raw, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "agent":
			d.Agent = value
		case "state":
			d.State = AgentStatus(value)
		case "manifest":
			d.Manifest = value
		case "rule":
			if id, detail, found := strings.Cut(value, " ("); found {
				d.Rule = strings.TrimSpace(id)
				d.RuleDetail = strings.TrimSpace(strings.TrimSuffix(detail, ")"))
			} else {
				d.Rule = value
			}
		case "evidence":
			d.Evidence = strings.Trim(value, "\"")
		}
	}
	return d
}
