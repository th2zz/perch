package discover

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

func TestEmptyMirrorCollectionsEncodeAsArrays(t *testing.T) {
	for _, tc := range []struct {
		name string
		snap *herdr.Snapshot
	}{
		{name: "unreachable"},
		{name: "last project closed", snap: &herdr.Snapshot{}},
		{name: "last agent closed", snap: &herdr.Snapshot{Panes: []herdr.Pane{{PaneID: "w1:p1"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mirror := Build([]Placed{{Host: Host{HostID: LocalHostID}, Snap: tc.snap}}, time.Now())
			data, err := json.Marshal(mirror)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"hosts", "workspaces", "tabs", "panes", "agents"} {
				if len(fields[key]) == 0 || fields[key][0] != '[' {
					t.Errorf("%s = %s; Android requires an array", key, fields[key])
				}
			}
		})
	}
}
