package server

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

func TestTerminalSizePrecedesEachChangedScreen(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close(); _ = client.Close() }()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	ws := &wsConn{conn: server, br: bufio.NewReader(server)}
	stream := &herdr.TerminalStream{Out: io.NopCloser(strings.NewReader(
		"{\"bytes\":\"YQ==\",\"width\":128,\"height\":49}\n" +
			"{\"bytes\":\"Yg==\",\"width\":128,\"height\":49}\n" +
			"{\"bytes\":\"Yw==\",\"width\":144,\"height\":55}\n"))}
	done := make(chan error, 1)
	go func() { done <- pumpTerminalOut(ws, stream) }()
	for i, opcode := range []byte{opText, opBinary, opBinary, opText, opBinary} {
		header := make([]byte, 2)
		if _, err := io.ReadFull(client, header); err != nil {
			t.Fatal(err)
		}
		if header[0]&15 != opcode {
			t.Fatalf("frame %d opcode: %d, want %d", i, header[0]&15, opcode)
		}
		length := int(header[1])
		if length == 126 {
			if _, err := io.ReadFull(client, header); err != nil {
				t.Fatal(err)
			}
			length = int(binary.BigEndian.Uint16(header))
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(client, payload); err != nil {
			t.Fatal(err)
		}
		if opcode == opText {
			var shape struct {
				Type       string
				Cols, Rows int
			}
			if err := json.Unmarshal(payload, &shape); err != nil {
				t.Fatal(err)
			}
			cols, rows := 128, 49
			if i == 3 {
				cols, rows = 144, 55
			}
			if shape.Type != "size" || shape.Cols != cols || shape.Rows != rows {
				t.Fatalf("wrong shape: %+v", shape)
			}
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
