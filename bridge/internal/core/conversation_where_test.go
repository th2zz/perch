package core

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"

	"github.com/th2zz/perch/bridge/internal/discover"
	"github.com/th2zz/perch/bridge/internal/herdr"
)

// The registration herdr reports about a pane is what lets the record readers find a codex
// session's rollout; without the wiring under test here the conversation face silently stays
// empty. A reviewer removed both assignments and the whole suite stayed green — which is why
// these tests exist rather than the code merely being read.

func TestSessionWhereTakesTheRegisteredSessionFromTheMirror(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", filepath.Join(t.TempDir(), "off.sock"))
	c := newTestCore(t)
	const id = "01a142dd-6a4f-7261-99c2-c1fd0d0cdcf7"

	kind := "codex"
	c.mirror = &discover.Mirror{Panes: []discover.PlacedPane{{
		Pane: herdr.Pane{
			PaneID:       "wA:p1",
			Agent:        &kind,
			AgentSession: &herdr.AgentSession{Source: "proc", Agent: "codex", Kind: "id", Value: id},
		},
		HostID: discover.LocalHostID,
	}}}

	gotKind, where, err := c.sessionWhere(context.Background(), "wA:p1")
	if err != nil {
		t.Fatalf("sessionWhere: %v", err)
	}
	if gotKind != "codex" || where.SessionID != id {
		t.Errorf("kind/session = %q/%q, want codex/%s", gotKind, where.SessionID, id)
	}

	// A path is a location, not an identity: it must never travel as a session id.
	c.mirror = &discover.Mirror{Panes: []discover.PlacedPane{{
		Pane: herdr.Pane{
			PaneID: "wA:p1",
			Agent:  &kind,
			AgentSession: &herdr.AgentSession{
				Source: "proc", Agent: "claude", Kind: "path",
				Value: "/home/x/.claude/projects/-home-x/828d.jsonl",
			},
		},
		HostID: discover.LocalHostID,
	}}}
	if _, where, err = c.sessionWhere(context.Background(), "wA:p1"); err != nil {
		t.Fatalf("sessionWhere: %v", err)
	}
	if where.SessionID != "" {
		t.Errorf("a path travelled as a session id: %q", where.SessionID)
	}
}

func TestSessionWhereAsksHerdrWhenTheMirrorDoesNotKnowThePane(t *testing.T) {
	const id = "01a142dd-6a4f-7261-99c2-c1fd0d0cdcf7"
	path := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var req struct {
					ID     string `json:"id"`
					Method string `json:"method"`
				}
				if json.Unmarshal(line, &req) != nil {
					return
				}
				var result map[string]any
				switch req.Method {
				case herdr.MethodPaneGet:
					result = map[string]any{"pane": map[string]any{
						"pane_id": "wA:p1",
						"agent":   "codex",
						"agent_session": map[string]any{
							"source": "proc", "agent": "codex", "kind": "id", "value": id,
						},
					}}
				case herdr.MethodPaneProcessInfo:
					result = map[string]any{"process_info": map[string]any{"foreground_processes": []any{}}}
				default:
					result = map[string]any{}
				}
				reply, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
				_, _ = conn.Write(append(reply, '\n'))
			}(conn)
		}
	}()

	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_SOCKET_PATH", path)
	c, err := New(Options{DisableSubscriptions: true, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}

	kind, where, err := c.sessionWhere(context.Background(), "wA:p1")
	if err != nil {
		t.Fatalf("sessionWhere: %v", err)
	}
	if kind != "codex" || where.SessionID != id {
		t.Errorf("kind/session = %q/%q, want codex/%s", kind, where.SessionID, id)
	}
}
