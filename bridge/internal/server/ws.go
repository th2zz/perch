package server

// A minimal RFC 6455 server implementation.
//
// Why not a dependency: this is a single-purpose endpoint on a bridge that must
// install with one command and build offline. The server side of WebSocket is
// about a hundred lines — handshake, frame header, masking — and none of the
// browser-facing complexity (extensions, subprotocol negotiation, fragmented
// send) is needed here. The app uses a standard client, so the wire format is
// the contract, not this code.

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// ErrNotUpgrade means the request was not a WebSocket upgrade.
var ErrNotUpgrade = errors.New("not a websocket upgrade request")

// wsConn is a server-side WebSocket connection.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader

	wmu    sync.Mutex
	closed bool
}

// upgrade performs the handshake and takes over the connection.
func upgrade(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") ||
		!headerContains(r.Header.Get("Connection"), "upgrade") {
		return nil, ErrNotUpgrade
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("missing Sec-WebSocket-Key")
	}
	if v := r.Header.Get("Sec-WebSocket-Version"); v != "" && v != "13" {
		return nil, fmt.Errorf("unsupported websocket version %q", v)
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("response writer does not support hijacking")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	accept := wsAccept(key)
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := rw.WriteString(resp); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &wsConn{conn: conn, br: rw.Reader}, nil
}

func wsAccept(key string) string {
	h := sha1.New()
	_, _ = io.WriteString(h, key)
	_, _ = io.WriteString(h, wsGUID)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func headerContains(header, token string) bool {
	for _, part := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// WriteText sends a single unfragmented text frame.
// WriteBinary writes one binary frame.
//
// A terminal's bytes travel as bytes: wrapping them in base64 inside JSON would add a third again to
// every screen repaint for no benefit, and the phone's emulator wants exactly these bytes.
func (c *wsConn) WriteBinary(payload []byte) error {
	return c.writeFrame(opBinary, payload)
}

func (c *wsConn) WriteText(payload []byte) error {
	return c.writeFrame(opText, payload)
}

// WriteClose sends a close frame.
func (c *wsConn) WriteClose(code uint16, reason string) error {
	buf := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(buf, code)
	copy(buf[2:], reason)
	return c.writeFrame(opClose, buf)
}

func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	header := []byte{0x80 | opcode}
	n := len(payload)
	switch {
	case n < 126:
		header = append(header, byte(n))
	case n <= 0xFFFF:
		header = append(header, 126, byte(n>>8), byte(n))
	default:
		header = append(header, 127)
		var lenBuf [8]byte
		binary.BigEndian.PutUint64(lenBuf[:], uint64(n))
		header = append(header, lenBuf[:]...)
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(payload)
	return err
}

// ReadMessage returns the next application message. Control frames are handled
// internally; a returned opcode is opText or opBinary.
func (c *wsConn) ReadMessage() (byte, []byte, error) {
	for {
		opcode, payload, fin, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch opcode {
		case opPing:
			_ = c.writeFrame(opPong, payload)
			continue
		case opPong:
			continue
		case opClose:
			_ = c.WriteClose(1000, "")
			return 0, nil, io.EOF
		case opContinuation:
			return 0, nil, errors.New("unexpected continuation frame")
		}
		if !fin {
			// The app never fragments; treat it as a protocol error rather
			// than silently mis-assembling a partial instruction.
			return 0, nil, errors.New("fragmented frames are not supported")
		}
		return opcode, payload, nil
	}
}

func (c *wsConn) readFrame() (opcode byte, payload []byte, fin bool, err error) {
	var head [2]byte
	if _, err = io.ReadFull(c.br, head[:]); err != nil {
		return 0, nil, false, err
	}
	fin = head[0]&0x80 != 0
	opcode = head[0] & 0x0F
	masked := head[1]&0x80 != 0

	length := uint64(head[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, false, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, false, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	// A control frame may be at most 125 bytes; bound everything to keep a
	// misbehaving client from allocating unbounded memory.
	const maxFrame = 1 << 20
	if length > maxFrame {
		return 0, nil, false, fmt.Errorf("frame too large: %d bytes", length)
	}

	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return 0, nil, false, err
		}
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return 0, nil, false, err
	}
	// RFC 6455 §5.3: client-to-server frames MUST be masked.
	if !masked {
		return 0, nil, false, errors.New("client frame is not masked")
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return opcode, payload, fin, nil
}

// Close terminates the connection.
func (c *wsConn) Close() error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.conn.Close()
}
