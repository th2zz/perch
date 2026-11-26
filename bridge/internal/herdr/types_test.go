package herdr

import "testing"

// herdr reports the session identity in two kinds, and the readers of it want only the one that
// names a session. A path read as an identity sends them looking for a session called after a file.
func TestOnlyAnIDNamesASession(t *testing.T) {
	cases := map[string]struct {
		session *AgentSession
		want    string
	}{
		"the id herdr read off codex": {
			&AgentSession{Source: "argv", Agent: "codex", Kind: "id", Value: "01a142dd-6a4f-7261-99c2-c1fd0d0cdcf7"},
			"01a142dd-6a4f-7261-99c2-c1fd0d0cdcf7",
		},
		"a path is not an identity": {
			&AgentSession{Source: "proc", Agent: "claude", Kind: "path", Value: "/home/x/.claude/projects/-home-x/828d.jsonl"},
			"",
		},
		"no registration at all": {nil, ""},
	}
	for name, tc := range cases {
		if got := tc.session.ID(); got != tc.want {
			t.Errorf("%s: ID() = %q, want %q", name, got, tc.want)
		}
	}
}
