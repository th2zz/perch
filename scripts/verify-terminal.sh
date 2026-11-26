#!/usr/bin/env bash

set -uo pipefail

LAB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$LAB_DIR/.." && pwd)"
# shellcheck source=lib/scratch-session.sh
. "$LAB_DIR/lib/scratch-session.sh"

SESSION="perch-term-$$"
PORT="${PERCH_TEST_PORT:-45994}"
STATE_DIR="$(mktemp -d -t perch-terminal-XXXXXX)"
BRIDGE_BIN="$STATE_DIR/perch-bridge"
MARKER="terminal-verify-$$"
PASS=0
FAIL=0
SKIP=0

pass() { PASS=$((PASS + 1)); printf '  \033[32mok\033[0m   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
skip() { SKIP=$((SKIP + 1)); printf '  \033[33mskip\033[0m %s\n' "$1"; }
section() { printf '\n%s\n' "$1"; }

cleanup() {
    [ -n "${BRIDGE_PID:-}" ] && kill "$BRIDGE_PID" 2>/dev/null || true
    scratch_session_cleanup
    rm -r "$STATE_DIR" 2>/dev/null || true
}
trap cleanup EXIT

section "Perch · a real terminal on a real pane"

cd /tmp || exit 1
scratch_session_start "$SESSION"
case $? in
    0) pass "a scratch session started (herdr session $SESSION)" ;;
    2) echo "skip: $SCRATCH_ERROR"; exit 0 ;;
    *) echo "$SCRATCH_ERROR" >&2; exit 1 ;;
esac
PANE="$SCRATCH_PANE"

(cd "$REPO_ROOT/bridge" && go build -o "$BRIDGE_BIN" ./cmd/perch-bridge) || {
    fail "could not build the bridge"
    exit 1
}
# HERDR_BIN_PATH points the bridge's own CLI calls at the user's herdr, which is how the terminal
# stream is opened. Without it the bridge would look for `herdr` on a PATH the service does not have.
HERDR_SOCKET_PATH="$SCRATCH_SOCKET" HERDR_PLUGIN_STATE_DIR="$STATE_DIR" \
    HERDR_BIN_PATH="$(command -v herdr)" PERCH_BRIDGE_ADDR="127.0.0.1:$PORT" \
    "$BRIDGE_BIN" run >"$STATE_DIR/bridge.log" 2>&1 &
BRIDGE_PID=$!
for _ in $(seq 1 40); do
    [ -f "$STATE_DIR/pairing.json" ] && break
    sleep 0.25
done
if [ ! -f "$STATE_DIR/pairing.json" ]; then
    fail "the bridge never wrote its pairing file (see $STATE_DIR/bridge.log)"
    exit 1
fi
TOKEN="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' "$STATE_DIR/pairing.json")"

# The pane's shape, before and after a client attaches. Equality is the whole claim of case 3: a phone
# that attaches must not move the program in the pane — that program is what somebody is looking at on
# their computer — and herdr does not undo a resize when the phone lets go. Rows are what the pane
# reports; the revision is included so a shape that happens to match by accident is harder to fake.
shape_now() {
    herdr --session "$SESSION" pane get "$PANE" |
        python3 -c 'import json,sys; p=json.load(sys.stdin)["result"]["pane"]; print("%s rows, revision %s" % (p["scroll"]["viewport_rows"], p.get("revision")))'
}
SHAPE_BEFORE="$(shape_now)"

# The phone itself, for the length of this script: a WebSocket client that speaks the wire the app
# will speak. Written here rather than shelled out to a tool, so the frames are exactly ours.
python3 - "$PORT" "$TOKEN" "$PANE" "$MARKER" <<'PY'
import base64, hashlib, json, os, socket, struct, sys, time

port, token, pane, marker = int(sys.argv[1]), sys.argv[2], sys.argv[3], sys.argv[4]

failures = []


def ws_connect(cols=None, rows=None):
    key = base64.b64encode(os.urandom(16)).decode()
    size = f"&cols={cols}&rows={rows}" if cols and rows else ""
    request = (
        # No cols/rows: this is what the app sends, and the point of it is that the pane must come out
        # of the visit unchanged. A size here would make this script the thing it is checking for.
        f"GET /v1/session/terminal?pane_id={pane}{size} HTTP/1.1\r\n"
        f"Host: 127.0.0.1:{port}\r\n"
        f"Authorization: Bearer {token}\r\n"
        "Upgrade: websocket\r\nConnection: Upgrade\r\n"
        f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n"
    )
    sock = socket.create_connection(("127.0.0.1", port), timeout=10)
    sock.sendall(request.encode())
    handshake = b""
    while b"\r\n\r\n" not in handshake:
        handshake += sock.recv(4096)
    if b"101" not in handshake.split(b"\r\n")[0]:
        raise SystemExit("handshake failed: " + handshake.split(b"\r\n")[0].decode())
    return sock


def send_frame(sock, opcode, payload):
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


def recv_frame(sock):
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


with ws_connect() as sock:
    # 1. Attaching gives a screen, and a shape. The terminal repaints when a client arrives — which is
    #    what makes reconnection free — and the bridge's first text frame says how big the pane is, so
    #    the phone can draw the computer's screen instead of asking for one of its own.
    screen = b""
    shape = None
    deadline = time.time() + 10
    while time.time() < deadline and (len(screen) < 200 or shape is None):
        opcode, payload = recv_frame(sock)
        if opcode == 2:
            screen += payload
        elif opcode == 1:
            try:
                parsed = json.loads(payload.decode())
            except Exception:
                continue
            if parsed.get("type") == "size":
                shape = parsed
    if b"\x1b[" in screen:
        print(f"  \033[32mok\033[0m   the attach repainted the screen ({len(screen)} bytes of escape-laden output)")
    else:
        failures.append(f"no repaint arrived (got {screen[:80]!r})")
    if shape and shape.get("cols", 0) > 1 and shape.get("rows", 0) > 1:
        print(f"  \033[32mok\033[0m   the pane's own size arrived with the stream ({shape['cols']}x{shape['rows']})")
    else:
        failures.append(f"the bridge did not report the pane's size (got {shape!r})")

    # 2. Typing goes to the pane: the shell runs it, and the *pane* is checked directly afterwards —
    #    a terminal that echoes keystrokes but runs nothing would pass a weaker test.
    send_frame(sock, 2, f"echo {marker}\r".encode())
    seen = b""
    deadline = time.time() + 10
    while time.time() < deadline and marker.encode() not in seen:
        opcode, payload = recv_frame(sock)
        if opcode == 2:
            seen += payload
    if marker.encode() in seen:
        print("  \033[32mok\033[0m   typing came back on the stream")
    else:
        failures.append("typed text never appeared on the stream")


print(json.dumps({"failures": failures}))
PY

section "And the pane itself, read directly"

if herdr --session "$SESSION" pane read "$PANE" --source visible --format text | grep -q "$MARKER"; then
    pass "the shell in the pane really ran the command typed through the socket"
else
    fail "the pane never ran the typed command — the bytes reached the stream and not the terminal"
fi

SHAPE_AFTER="$(shape_now)"
if [ "$SHAPE_BEFORE" = "$SHAPE_AFTER" ]; then
    pass "attaching left the pane exactly as it was (headless fixture; $SHAPE_AFTER)"
else
    fail "the pane changed shape when a client attached: $SHAPE_BEFORE -> $SHAPE_AFTER"
fi

section "Explicit resize with a TUI client attached (not verified here, #137)"

skip "explicit resize with a TUI client attached: tracked in #137"

ATTACH_REPORT="${PERCH_TERMINAL_ATTACH_REPORT:-/tmp/perch-terminal-attach-$$.json}"
if python3 "$LAB_DIR/probe-terminal-attach.py" --bridge-bin "$BRIDGE_BIN" --output "$ATTACH_REPORT" >"$ATTACH_REPORT.log" 2>&1; then
    pass "desktop TUI attach, input, window resize, release and re-entry preserved ($ATTACH_REPORT)"
else
    fail "desktop TUI terminal check failed ($ATTACH_REPORT.log)"
fi

printf '\n%d passed, %d failed, %d skipped\n' "$PASS" "$FAIL" "$SKIP"
[ "$FAIL" -eq 0 ]
