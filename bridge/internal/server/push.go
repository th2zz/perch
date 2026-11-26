package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/th2zz/perch/bridge/internal/queue"
)

// PushSummary is the entire content of a notification payload.
//
// Constitution V.8: a push payload carries a *summary* — enough for the phone to
// render "codex in project X wants approval" — and nothing else. There is
// deliberately no field for terminal output, code, or prompt text, so a payload
// cannot carry them even by accident. The app fetches the detail over its own
// authenticated connection to the host, which never involves our gateway.
//
// The type is the whitelist; relay/test/payload_test.go asserts that the
// serialised key set never grows past it.
type PushSummary struct {
	DeviceToken    string `json:"device_token"`
	DecisionID     string `json:"decision_id"`
	HostLabel      string `json:"host_label"`
	WorkspaceLabel string `json:"workspace_label"`
	Agent          string `json:"agent"`
	Kind           string `json:"kind"`
	CreatedAt      int64  `json:"created_at"`
}

// PushAllowedFields is the frozen key set of a push payload.
var PushAllowedFields = []string{
	"device_token", "decision_id", "host_label", "workspace_label",
	"agent", "kind", "created_at",
}

// SummaryFor derives a payload from a decision and the local host label.
// PromptText and Title are intentionally dropped.
func SummaryFor(deviceToken, hostLabel string, d queue.Decision) PushSummary {
	agent := d.DisplayAgent
	if agent == "" {
		agent = d.AgentKind
	}
	return PushSummary{
		DeviceToken:    deviceToken,
		DecisionID:     d.ID,
		HostLabel:      hostLabel,
		WorkspaceLabel: d.WorkspaceLabel,
		Agent:          agent,
		Kind:           string(d.SuggestedKind),
		CreatedAt:      d.CreatedAt.Unix(),
	}
}

// Pusher delivers summaries to the push gateway.
//
// The gateway is a stateless service we operate, because sending FCM requires
// our service-account key and copying that key to every user's machine would
// let any one of them impersonate every user (research §10). During development
// it runs on the developer's own machine; the code does not change when it
// moves to a VPS.
type Pusher struct {
	GatewayURL string
	// GatewayToken authenticates the host to the gateway.
	GatewayToken string
	Client       *http.Client
}

// NewPusher builds a pusher with a sane timeout so a dead gateway cannot wedge
// the bridge.
func NewPusher(gatewayURL, gatewayToken string) *Pusher {
	return &Pusher{
		GatewayURL:   gatewayURL,
		GatewayToken: gatewayToken,
		Client:       &http.Client{Timeout: 10 * time.Second},
	}
}

// Send posts one summary. A failure is returned so the caller can log it; it
// must never block the decision path, because the app can still be opened
// manually.
func (p *Pusher) Send(ctx context.Context, summary PushSummary) error {
	if p == nil || p.GatewayURL == "" {
		return fmt.Errorf("no push gateway configured")
	}
	body, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.GatewayURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.GatewayToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.GatewayToken)
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("push gateway returned %s", resp.Status)
	}
	return nil
}
