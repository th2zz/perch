package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
)

func terminalInputAPI(t *testing.T, fail bool) (*Client, <-chan request) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	calls := make(chan request, 32)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			var req request
			_ = json.Unmarshal(line, &req)
			calls <- req
			reply := map[string]any{"id": req.ID, "result": map[string]any{"type": "ok"}}
			if fail {
				reply = map[string]any{"id": req.ID, "error": map[string]string{"code": "pane_send_failed", "message": "gone"}}
			}
			_ = json.NewEncoder(conn).Encode(reply)
			_ = conn.Close()
		}
	}()
	return NewClient(path), calls
}

func TestTerminalViewerPreservesInputBytes(t *testing.T) {
	api, calls := terminalInputAPI(t, false)
	stream := &TerminalStream{viewer: &terminalViewer{ctx: context.Background(), api: api, target: "wA:p1"}}
	payload := []byte("café🙂\x00\x03\x1b[A\x1b[200~paste\nline\x1b[201~\r\x7f")
	for _, chunk := range [][]byte{payload[:1], payload[1:5], payload[5:]} {
		if err := stream.WriteInput(chunk); err != nil {
			t.Fatal(err)
		}
	}
	var got []byte
	for len(calls) > 0 {
		call := <-calls
		params := call.Params.(map[string]any)
		if call.Method != MethodPaneSendText || params["pane_id"] != "wA:p1" {
			t.Fatalf("wrong route: %+v", call)
		}
		got = append(got, []byte(params["text"].(string))...)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q, want %q", got, payload)
	}
	if len(stream.viewer.pending) != 0 {
		t.Fatalf("pending: %x", stream.viewer.pending)
	}
}

func TestTerminalViewerRejectsInvalidInputWithoutReplacement(t *testing.T) {
	api, calls := terminalInputAPI(t, false)
	stream := &TerminalStream{viewer: &terminalViewer{ctx: context.Background(), api: api, target: "wA:p1"}}
	if err := stream.WriteInput([]byte{'a', 0xff}); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	if len(calls) != 0 {
		t.Fatal("invalid frame partially submitted")
	}
	if err := stream.WriteInput([]byte{0xe4}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 || len(stream.viewer.pending) != 1 {
		t.Fatal("incomplete character submitted")
	}
	if err := stream.Resize(80, 24); err == nil {
		t.Fatal("resize discarded incomplete character")
	}
	stream.viewer.closed = true
	if err := stream.WriteInput([]byte("x")); err == nil {
		t.Fatal("closed viewer accepted input")
	}
}

func TestTerminalViewerDoesNotRetryFailedInput(t *testing.T) {
	api, calls := terminalInputAPI(t, true)
	stream := &TerminalStream{viewer: &terminalViewer{ctx: context.Background(), api: api, target: "wA:p1"}}
	if err := stream.WriteInput([]byte("\r")); err == nil {
		t.Fatal("input failure ignored")
	}
	if len(calls) != 1 {
		t.Fatalf("request count: %d", len(calls))
	}
}
