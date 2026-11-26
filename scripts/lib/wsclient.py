"""A hand-written WebSocket client, for verifications that have to speak our own wire.

Deliberately not a library and deliberately not `websocat`: these scripts exist to check the frames the
*app* will send, so the frames should be written here, in front of whoever is reading the script. It is
also the reason there is one copy of this and not two — the terminal verifications grew a second copy
each time, and two clients that agree by accident are not evidence of anything.

Standard library only. Used as:

    sys.path.insert(0, os.path.join(os.path.dirname(__file__), "lib"))
    from wsclient import connect, send_text, send_binary, recv_frame, read_until
"""

import base64
import json
import os
import socket
import struct
import time

OPCODE_TEXT = 1
OPCODE_BINARY = 2
OPCODE_CLOSE = 8


def connect(port, token, path, host="127.0.0.1", timeout=10):
    """Open a WebSocket the way the app does: bearer token, upgrade, nothing else."""
    key = base64.b64encode(os.urandom(16)).decode()
    request = (
        f"GET {path} HTTP/1.1\r\n"
        f"Host: {host}:{port}\r\n"
        f"Authorization: Bearer {token}\r\n"
        "Upgrade: websocket\r\nConnection: Upgrade\r\n"
        f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n"
    )
    sock = socket.create_connection((host, port), timeout=timeout)
    sock.sendall(request.encode())
    handshake = b""
    while b"\r\n\r\n" not in handshake:
        chunk = sock.recv(4096)
        if not chunk:
            break
        handshake += chunk
    status = handshake.split(b"\r\n")[0]
    if b"101" not in status:
        sock.close()
        raise RuntimeError("handshake failed: " + status.decode(errors="replace"))
    return sock


def send_frame(sock, opcode, payload):
    if isinstance(payload, str):
        payload = payload.encode()
    mask = os.urandom(4)
    masked = bytes(byte ^ mask[i % 4] for i, byte in enumerate(payload))
    header = bytes([0x80 | opcode])
    length = len(payload)
    if length < 126:
        header += bytes([0x80 | length])
    elif length < (1 << 16):
        header += bytes([0x80 | 126]) + struct.pack(">H", length)
    else:
        header += bytes([0x80 | 127]) + struct.pack(">Q", length)
    sock.sendall(header + mask + masked)


def send_text(sock, text):
    send_frame(sock, OPCODE_TEXT, text)


def send_binary(sock, payload):
    send_frame(sock, OPCODE_BINARY, payload)


def recv_frame(sock):
    """One frame, or (None, b"") when the peer closed the socket."""
    head = sock.recv(2)
    if len(head) < 2:
        return None, b""
    opcode = head[0] & 0x0F
    length = head[1] & 0x7F
    if length == 126:
        length = struct.unpack(">H", sock.recv(2))[0]
    elif length == 127:
        length = struct.unpack(">Q", sock.recv(8))[0]
    payload = b""
    while len(payload) < length:
        chunk = sock.recv(length - len(payload))
        if not chunk:
            break
        payload += chunk
    return opcode, payload


def read_until(sock, wanted, within=10.0, collect_text=False):
    """Read until `wanted` (bytes) appears, or time runs out.

    Returns (bytes_seen, text_messages, closed):
      bytes_seen     everything that arrived on binary frames, in order
      text_messages  every text frame, parsed as JSON where it parses
      closed         True when the peer closed rather than the deadline expiring
    """
    if isinstance(wanted, str):
        wanted = wanted.encode()
    seen = b""
    texts = []
    deadline = time.time() + within
    while time.time() < deadline:
        if wanted and wanted in seen:
            return seen, texts, False
        if not wanted and seen:
            return seen, texts, False
        try:
            opcode, payload = recv_frame(sock)
        except socket.timeout:
            break
        if opcode is None:
            return seen, texts, True
        if opcode == OPCODE_BINARY:
            seen += payload
        elif opcode == OPCODE_TEXT:
            if collect_text:
                try:
                    texts.append(json.loads(payload.decode()))
                except Exception:
                    texts.append({"raw": payload.decode(errors="replace")})
        elif opcode == OPCODE_CLOSE:
            return seen, texts, True
    return seen, texts, False


def read_until_close(sock, within=10.0):
    """Read until the peer closes, collecting everything on the way.

    Needed for the end of a stream, because the last thing a bridge says (`{"type":"ended"}`, on a text
    frame) arrives *before* the close frame — and a helper that stops at the first bytes would return a
    replay and never see it. That is not a hypothetical: the first version of this verification asked with
    `read_until(sock, "")` and reported a missing 'ended' message depending on which frame won the race.
    """
    seen, texts, deadline = b"", [], time.time() + within
    while time.time() < deadline:
        try:
            opcode, payload = recv_frame(sock)
        except socket.timeout:
            break
        if opcode is None:
            return seen, texts, True
        if opcode == OPCODE_BINARY:
            seen += payload
        elif opcode == OPCODE_TEXT:
            try:
                texts.append(json.loads(payload.decode()))
            except Exception:
                texts.append({"raw": payload.decode(errors="replace")})
        elif opcode == OPCODE_CLOSE:
            return seen, texts, True
    return seen, texts, False
