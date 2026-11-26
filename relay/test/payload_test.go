package test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/th2zz/perch/relay/internal/push"
)

// TestWhitelistIsExactlyTheSummary pins the key set. If someone adds a field to
// the bridge's payload, this test is the thing that notices.
func TestWhitelistIsExactlyTheSummary(t *testing.T) {
	want := []string{
		"agent", "created_at", "decision_id", "device_token",
		"host_label", "kind", "workspace_label",
	}
	got := push.AllowedFields()
	if len(got) != len(want) {
		t.Fatalf("allowed fields = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("allowed fields = %v, want %v", got, want)
		}
	}
}

// TestContentBearingFieldsAreRejected is the privacy invariant under attack:
// each of these payloads would leak user content to a third party.
func TestContentBearingFieldsAreRejected(t *testing.T) {
	base := `"device_token":"tok","decision_id":"d1","host_label":"h",` +
		`"workspace_label":"w","agent":"codex","kind":"approve","created_at":1`

	hostile := map[string]string{
		"prompt text":     `{` + base + `,"text":"rm -rf / and other code"}`,
		"terminal output": `{` + base + `,"output":"error: secret key=sk-live-1234"}`,
		"code":            `{` + base + `,"code":"package main"}`,
		"title":           `{` + base + `,"title":"internal project name"}`,
		"nested body":     `{` + base + `,"notification":{"body":"full transcript"}}`,
		"prompt_text":     `{` + base + `,"prompt_text":"user typed this"}`,
	}

	for name, payload := range hostile {
		t.Run(name, func(t *testing.T) {
			_, err := push.Decode([]byte(payload))
			if !errors.Is(err, push.ErrDisallowedField) {
				t.Fatalf("payload carrying %s was accepted: err = %v", name, err)
			}
			if !strings.Contains(err.Error(), "not allowed") {
				t.Errorf("error should name the problem: %v", err)
			}
		})
	}
}

// TestSummaryRoundTrips is the positive case: the legitimate payload works.
func TestSummaryRoundTrips(t *testing.T) {
	body := `{
	  "device_token": "tok",
	  "decision_id": "wA:p1@1",
	  "host_label": "dev-laptop",
	  "workspace_label": "mobile agent",
	  "agent": "codex",
	  "kind": "approve",
	  "created_at": 1791845850
	}`
	summary, err := push.Decode([]byte(body))
	if err != nil {
		t.Fatalf("a well-formed summary must be accepted: %v", err)
	}
	if summary.DecisionID != "wA:p1@1" || summary.Agent != "codex" {
		t.Fatalf("decoded summary lost fields: %+v", summary)
	}

	note := push.Render(summary)
	if !strings.Contains(note.Title, "codex") || !strings.Contains(note.Title, "mobile agent") {
		t.Errorf("notification must name the agent and the project: %q", note.Title)
	}
	if note.Data["decision_id"] != summary.DecisionID {
		t.Errorf("deep-link data must carry the decision id: %v", note.Data)
	}

	// The rendered notification must not smuggle anything the summary lacked.
	encoded, _ := json.Marshal(note)
	for _, forbidden := range []string{"prompt_text", `"output"`, `"code"`, "secret"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("rendered notification contains %q: %s", forbidden, encoded)
		}
	}
}

// TestOversizedPayloadRejected keeps the gateway from accepting a "summary" that
// is really a transcript.
func TestOversizedPayloadRejected(t *testing.T) {
	big := `{"device_token":"` + strings.Repeat("a", push.MaxSummaryBytes) + `"}`
	if _, err := push.Decode([]byte(big)); err == nil {
		t.Fatal("an oversized payload must be rejected")
	}
}

// TestMissingRequiredFieldsRejected covers the empty and half-filled shapes.
func TestMissingRequiredFieldsRejected(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"decision_id":"d1"}`,
		`{"device_token":"tok"}`,
	} {
		if _, err := push.Decode([]byte(body)); err == nil {
			t.Errorf("payload %s must be rejected", body)
		}
	}
}
