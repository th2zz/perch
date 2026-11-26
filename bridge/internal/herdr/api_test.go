package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// fakeHerdr stands up a socket that answers every request with the same canned
// frame, so a decoding bug can be tested without a real server.
func fakeHerdr(t *testing.T, response map[string]any) *Client {
	t.Helper()
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
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadBytes('\n')
					if err != nil {
						return
					}
					var request struct {
						ID string `json:"id"`
					}
					if err := json.Unmarshal(line, &request); err != nil {
						return
					}
					reply := map[string]any{"id": request.ID, "result": response}
					body, _ := json.Marshal(reply)
					if _, err := conn.Write(append(body, '\n')); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	return NewClient(path)
}

// TestReadUnwrapsThePanesReadEnvelope is the regression test for a bug that made
// every excerpt the product showed silently empty.
//
// The wire shape is `{"result":{"type":"pane_read","read":{...}}}`. Decoding that
// into the inner type *succeeds* and leaves every field zero, so the failure was
// invisible: no error, no log, just a card with no text on it.
func TestReadUnwrapsThePanesReadEnvelope(t *testing.T) {
	client := fakeHerdr(t, map[string]any{
		"type": "pane_read",
		"read": map[string]any{
			"pane_id":      "wA:p1",
			"workspace_id": "wA",
			"tab_id":       "wA:t1",
			"source":       "recent",
			"format":       "text",
			"text":         "Do you want to proceed?\n❯ 1. Yes\n  2. No",
			"revision":     42,
			"truncated":    false,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.ReadPane(ctx, "wA:p1", "recent", 10)
	if err != nil {
		t.Fatalf("ReadPane: %v", err)
	}
	if result.Text == "" {
		t.Fatal("the excerpt came back empty; the envelope is not being unwrapped")
	}
	if result.Text != "Do you want to proceed?\n❯ 1. Yes\n  2. No" {
		t.Errorf("text = %q", result.Text)
	}
	if result.PaneID != "wA:p1" || result.Revision != 42 {
		t.Errorf("fields came from the wrong level: %+v", result)
	}
}

// TestReadAgentUsesTheSameEnvelope: both methods share one result type, so both
// have to unwrap it.
func TestReadAgentUsesTheSameEnvelope(t *testing.T) {
	client := fakeHerdr(t, map[string]any{
		"type": "pane_read",
		"read": map[string]any{
			"pane_id": "wA:p1", "workspace_id": "wA", "tab_id": "wA:t1",
			"source": "recent", "format": "text", "text": "answer me", "revision": 7,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.ReadAgent(ctx, "wA:p1", "recent", 10)
	if err != nil {
		t.Fatalf("ReadAgent: %v", err)
	}
	if result.Text != "answer me" {
		t.Errorf("text = %q; agent.read shares the pane_read envelope", result.Text)
	}
}

// TestAnUnexpectedResultTypeIsAnError rather than a zero value: if herdr renames
// the result, the product must say so instead of showing blank cards for ever.
func TestAnUnexpectedResultTypeIsAnError(t *testing.T) {
	client := fakeHerdr(t, map[string]any{"type": "something_else"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.ReadPane(ctx, "wA:p1", "recent", 10); err == nil {
		t.Fatal("an unrecognised result type must be reported")
	}
	if _, err := client.ReadAgent(ctx, "wA:p1", "recent", 10); err == nil {
		t.Fatal("an unrecognised result type must be reported")
	}
}

// TestSnapshotAndListsKeepTheirOwnShapes guards the other results the product
// depends on, since they were written from the same schema and reviewed together.
func TestSnapshotAndListsKeepTheirOwnShapes(t *testing.T) {
	client := fakeHerdr(t, map[string]any{
		"type": "session_snapshot",
		"snapshot": map[string]any{
			"version": "0.9.1", "protocol": 22,
			"workspaces": []any{map[string]any{"workspace_id": "wA", "label": "project"}},
			"tabs":       []any{}, "panes": []any{}, "layouts": []any{}, "agents": []any{},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snapshot.Workspaces) != 1 || snapshot.Workspaces[0].Label != "project" {
		t.Errorf("snapshot lost its payload: %+v", snapshot)
	}
	if snapshot.Protocol != 22 {
		t.Errorf("protocol = %d", snapshot.Protocol)
	}
}

func TestTheSubscriptionSetFollowsPanesOutTheDoor(t *testing.T) {
	subs := AgentStatesSubscriptions([]string{"w1:p1"})

	types := map[string]int{}
	panes := map[string][]string{}
	for _, s := range subs {
		types[s.Type]++
		panes[s.Type] = append(panes[s.Type], s.PaneID)
	}

	for _, want := range []string{EventPaneCreated, EventPaneClosed, EventPaneExited, EventPaneAgentDetected} {
		if types[want] != 1 || panes[want][0] != "" {
			t.Errorf("%s must be subscribed once, globally (got %d, pane ids %v)", want, types[want], panes[want])
		}
	}
	if types[EventPaneAgentStatusChanged] != 1 || panes[EventPaneAgentStatusChanged][0] != "w1:p1" {
		t.Errorf("pane.agent_status_changed must be subscribed per pane (got %d, pane ids %v)",
			types[EventPaneAgentStatusChanged], panes[EventPaneAgentStatusChanged])
	}
}
