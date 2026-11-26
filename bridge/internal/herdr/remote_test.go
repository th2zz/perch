package herdr

import (
	"encoding/json"
	"testing"
)

// TestMachineListJSONIsTheRealShape pins the fixture to output captured from
// herdr 0.9.1 (`herdr machine list --json`, 2026-10-13). If a future herdr
// renames a field, this test is where that shows up as a one-line diff rather
// than as "my other computer disappeared".
func TestMachineListJSONIsTheRealShape(t *testing.T) {
	text := `[
  {
    "id": "1f895615b38cb8bfa65e0f99375d9b78",
    "label": "perch-standin",
    "target": "perch-standin",
    "session": "perch-standin",
    "enabled": true,
    "selected": false
  }
]`
	machines, ok := parseMachinesJSON(text)
	if !ok {
		t.Fatalf("the captured output did not parse as JSON")
	}
	if len(machines) != 1 {
		t.Fatalf("got %d machines, want 1", len(machines))
	}
	m := machines[0]
	if m.Label != "perch-standin" || m.Target != "perch-standin" || m.Session != "perch-standin" {
		t.Errorf("machine = %+v", m)
	}
	if !m.Enabled {
		t.Error("enabled was not read; a disabled machine would be polled anyway")
	}
}

func TestMachineListWithoutJSONStillParses(t *testing.T) {
	// A herdr whose flags differ must degrade to the table rather than to
	// "this host has no other computers".
	if _, ok := parseMachinesJSON("No saved SSH machines.\n"); ok {
		t.Error("the human-readable output was accepted as JSON")
	}
	if _, ok := parseMachinesJSON(""); ok {
		t.Error("empty output was accepted as JSON")
	}
	// Verbatim from `herdr machine list` on 0.9.1: tab separated, no header, and
	// the label is the second column — the first is herdr's internal id.
	machines := ParseMachineList("1f895615b38cb8bfa65e0f99375d9b78\tworkbox\tworkbox\tworkbox\tenabled\n" +
		"aabbccddeeff00112233445566778899\told-mac\told-mac\tdefault\tdisabled\n")
	if len(machines) != 2 {
		t.Fatalf("got %d machines, want 2: %+v", len(machines), machines)
	}
	if machines[0].Label != "workbox" || !machines[0].Enabled {
		t.Errorf("first machine = %+v", machines[0])
	}
	if machines[1].Enabled {
		t.Errorf("a disabled machine was read as enabled: %+v", machines[1])
	}
	if machines[0].ID == machines[0].Label {
		t.Errorf("the id was used as the label: %+v", machines[0])
	}
}

// TestRemoteSnapshotReadsTheCapturedEnvelope: the CLI's `api snapshot` wraps the
// session in the same envelope the socket returns, which is what lets one decoder
// serve both transports. Captured from the real CLI.
func TestRemoteSnapshotReadsTheCapturedEnvelope(t *testing.T) {
	text := `{"id":"cli:api:snapshot","result":{"snapshot":{"agents":[],` +
		`"focused_pane_id":"w1:p1","protocol":22,"version":"0.9.1",` +
		`"panes":[{"agent_status":"unknown","pane_id":"w1:p1","workspace_id":"w1",` +
		`"tab_id":"w1:t1","revision":1}],` +
		`"tabs":[],"workspaces":[{"label":"~","number":1,"pane_count":1,` +
		`"tab_count":1,"workspace_id":"w1","agent_status":"unknown"}]},` +
		`"type":"session_snapshot"}}`

	var out struct {
		Result struct {
			Type     string   `json:"type"`
			Snapshot Snapshot `json:"snapshot"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Result.Type != "session_snapshot" {
		t.Fatalf("type = %q", out.Result.Type)
	}
	if out.Result.Snapshot.Version != "0.9.1" || out.Result.Snapshot.Protocol != 22 {
		t.Errorf("version/protocol = %q/%d", out.Result.Snapshot.Version, out.Result.Snapshot.Protocol)
	}
	if len(out.Result.Snapshot.Panes) != 1 || out.Result.Snapshot.Panes[0].PaneID != "w1:p1" {
		t.Errorf("panes = %+v", out.Result.Snapshot.Panes)
	}
}
