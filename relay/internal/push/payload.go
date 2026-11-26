// Package push is the notification gateway's core: validate a summary, then
// hand it to the vendor's push service.
//
// The gateway is stateless by construction. It keeps no history, exposes no
// query API, and stores nothing. That is not an optimisation — it is the
// property that lets the product claim "your code never leaves your machine"
// while still delivering a notification (constitution V.8).
package push

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Summary is the only shape the gateway accepts.
type Summary struct {
	DeviceToken    string `json:"device_token"`
	DecisionID     string `json:"decision_id"`
	HostLabel      string `json:"host_label"`
	WorkspaceLabel string `json:"workspace_label"`
	Agent          string `json:"agent"`
	Kind           string `json:"kind"`
	CreatedAt      int64  `json:"created_at"`
}

// allowedFields is the frozen key set. It is duplicated here on purpose: the
// gateway must be able to reject a payload even if a future bridge build starts
// sending more. A shared constant would make the guarantee depend on the
// sender's good behaviour.
var allowedFields = map[string]struct{}{
	"device_token":    {},
	"decision_id":     {},
	"host_label":      {},
	"workspace_label": {},
	"agent":           {},
	"kind":            {},
	"created_at":      {},
}

// MaxSummaryBytes bounds a request body. A summary is a few hundred bytes; a
// body larger than this is either a mistake or an attempt to smuggle terminal
// output through the gateway.
const MaxSummaryBytes = 4 << 10

// ErrDisallowedField means the payload carried something a push must never
// carry.
var ErrDisallowedField = errors.New("push payload contains a field that is not allowed")

// Decode validates and parses a request body.
//
// Unknown fields are rejected rather than ignored. This is the one place where
// being strict costs nothing and buys the privacy claim: if someone later adds
// a "text" field to the bridge's payload, the gateway refuses it instead of
// quietly forwarding code to Google's servers.
func Decode(body []byte) (Summary, error) {
	if len(body) > MaxSummaryBytes {
		return Summary{}, fmt.Errorf("payload is %d bytes, limit is %d", len(body), MaxSummaryBytes)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return Summary{}, fmt.Errorf("payload is not a JSON object: %w", err)
	}
	var offending []string
	for key := range raw {
		if _, ok := allowedFields[key]; !ok {
			offending = append(offending, key)
		}
	}
	if len(offending) > 0 {
		sort.Strings(offending)
		return Summary{}, fmt.Errorf("%w: %v", ErrDisallowedField, offending)
	}

	var s Summary
	dec := json.NewDecoder(bytesReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Summary{}, err
	}
	if s.DeviceToken == "" {
		return Summary{}, errors.New("device_token is required")
	}
	if s.DecisionID == "" {
		return Summary{}, errors.New("decision_id is required")
	}
	return s, nil
}

// AllowedFields returns the whitelist, sorted, for documentation and tests.
func AllowedFields() []string {
	out := make([]string, 0, len(allowedFields))
	for k := range allowedFields {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
