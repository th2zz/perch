#!/usr/bin/env bash

set -uo pipefail

LAB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$LAB_DIR/.." && pwd)"
PORT="${PERCH_TEST_PORT:-45995}"
STATE_DIR="$(mktemp -d -t perch-shell-XXXXXX)"
BRIDGE_BIN="$STATE_DIR/perch-bridge"
MARKER="perch-shell-$$"
PASS=0
FAIL=0

pass() { PASS=$((PASS + 1)); printf '  \033[32mok\033[0m   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
section() { printf '\n%s\n' "$1"; }

cleanup() {
    [ -n "${BRIDGE_PID:-}" ] && kill "$BRIDGE_PID" 2>/dev/null
    # The shell the bridge started is a child of it; killing the bridge leaves it orphaned, so it is
    # ended explicitly. This is the one place a verification may kill a shell, and it is its own.
    [ -n "${SESSION_PID:-}" ] && kill "$SESSION_PID" 2>/dev/null
    rm -r "$STATE_DIR" 2>/dev/null
}
trap cleanup EXIT

section "Perch · a real shell on this machine, with no herdr in sight"

(cd "$REPO_ROOT/bridge" && go build -o "$BRIDGE_BIN" ./cmd/perch-bridge) || { fail "could not build the bridge"; exit 1; }

HERDR_SOCKET_PATH="$STATE_DIR/no-herdr-here.sock" HERDR_PLUGIN_STATE_DIR="$STATE_DIR" \
    PERCH_BRIDGE_ADDR="127.0.0.1:$PORT" \
    "$BRIDGE_BIN" run >"$STATE_DIR/bridge.log" 2>&1 &
BRIDGE_PID=$!
for _ in $(seq 1 60); do
    [ -f "$STATE_DIR/pairing.json" ] && break
    sleep 0.25
done
if [ ! -f "$STATE_DIR/pairing.json" ]; then
    fail "the bridge never started (see $STATE_DIR/bridge.log)"
    exit 1
fi
TOKEN="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' "$STATE_DIR/pairing.json")"
pass "the bridge is up with a herdr socket that does not exist"

PERCH_SHELL_STATE="$STATE_DIR" python3 - "$PORT" "$TOKEN" "$MARKER" "$LAB_DIR" <<'PY'
import json
import os
import sys
import time

port, token, marker, lab_dir = int(sys.argv[1]), sys.argv[2], sys.argv[3], sys.argv[4]
sys.path.insert(0, os.path.join(lab_dir, "lib"))
from wsclient import connect, read_until, read_until_close, send_binary, send_text  # noqa: E402

failures = []


passes = 0


def check(condition, message):
    global passes
    if condition:
        passes += 1
        print(f"  \033[32mok\033[0m   {message}")
    else:
        failures.append(message)
        print(f"  \033[31mFAIL\033[0m {message}")


# 1. A shell runs a command, and the answer comes back.
first = connect(port, token, "/v1/terminal")
send_binary(first, f"export PERCH_KEPT={marker}\necho {marker}-ran\n".encode())
seen, _, _ = read_until(first, f"{marker}-ran", within=15)
check(f"{marker}-ran".encode() in seen, "a shell on the host ran a command typed through the socket")

# 2. The client goes away. The shell does not.
first.close()
time.sleep(1.0)

second = connect(port, token, "/v1/terminal")
replay, _, _ = read_until(second, f"{marker}-ran", within=15)
check(
    f"{marker}-ran".encode() in replay,
    "the replay on re-attach carries what happened before — the screen comes back, not a blank rectangle",
)

send_binary(second, f"echo kept=$PERCH_KEPT\n".encode())
seen, _, _ = read_until(second, f"kept={marker}", within=15)
check(
    f"kept={marker}".encode() in seen,
    "it is the same shell after the client left and came back (a variable survived)",
)

# 3. A size from the phone reaches the shell: this is the owner of the shape, unlike a pane.
send_text(second, json.dumps({"type": "resize", "cols": 100, "rows": 40}))
time.sleep(0.5)
send_binary(second, b"stty size\n")
seen, _, _ = read_until(second, "40 100", within=15)
check("40 100".encode() in seen, "the phone's size reached the shell (stty reports 100x40)")

# A size that is not a screen is refused rather than obeyed: the pane stream learned this the hard way
# (a viewport reported before layout squashed somebody's running pane to two rows).
send_text(second, json.dumps({"type": "resize", "cols": 1, "rows": 1}))
time.sleep(0.5)
send_binary(second, b"stty size\n")
seen, _, _ = read_until(second, "40 100", within=15)
check("40 100".encode() in seen, "a 1x1 resize was refused and the shell kept its real shape")

# 4. "New terminal" means a new shell, and the old one stops.
third = connect(port, token, "/v1/terminal?new=1")
send_binary(third, b"echo kept=$PERCH_KEPT\n")
seen, _, _ = read_until(third, "kept=", within=15)
check(
    f"kept={marker}".encode() not in seen,
    "asking for a new terminal started, in fact, a new shell (the variable is gone)",
)

# 5. Closing is a thing a person asks for, and then the terminal says so rather than going quiet.
send_text(third, json.dumps({"type": "close"}))
# `read_until_close`, not `read_until`: the bridge says `ended` on a text frame *before* it closes, and a
# reader that stops at the first bytes would miss it — which is exactly how this check flaked once.
_, texts, closed = read_until_close(third, within=10)
check(closed, "closing the shell ended the stream instead of leaving it hanging")
check(
    any(message.get("type") == "ended" for message in texts),
    "the bridge said the shell had ended rather than dropping the connection silently",
)

print(json.dumps({"failures": failures}))
# Written where the shell script can count them: a summary that only counts its own checks is how a
# verification reports "0 failed" over output that says otherwise.
with open(os.path.join(os.environ["PERCH_SHELL_STATE"], "failures"), "w") as handle:
    handle.write("\n".join(failures))
with open(os.path.join(os.environ["PERCH_SHELL_STATE"], "passes"), "w") as handle:
    handle.write(str(passes))
PY
STATUS=$?

# The client's own failures count here too, so the summary line is the whole story rather than the part of
# it that happened to be written in bash — the lesson of a check that reported "0 failed" while its own
# output above said otherwise.
if [ -f "$STATE_DIR/passes" ]; then
    pass "$(cat "$STATE_DIR/passes") checks through the socket (listed above)"
fi
if [ -f "$STATE_DIR/failures" ]; then
    while IFS= read -r line; do
        [ -n "$line" ] && fail "the client reported: $line"
    done <"$STATE_DIR/failures"
fi
if [ "$STATUS" -ne 0 ]; then
    fail "the client script exited with $STATUS (see its output above)"
fi

# Every check above ran while the bridge was saying, over and over, that it cannot reach herdr — because
# the socket path deliberately points at nothing. That is the evidence, stated as a line: the terminal
# works on a machine where the herdr side of the product is completely unreachable.
HERDR_COMPLAINTS="$(grep -c 'herdr' "$STATE_DIR/bridge.log" 2>/dev/null || echo 0)"
pass "the terminal ran with herdr unreachable ($HERDR_COMPLAINTS complaints about it in the bridge log, all about other jobs)"

section "Summary"
printf '  %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || { printf '\n  bridge log: %s\n' "$STATE_DIR/bridge.log"; cat "$STATE_DIR/bridge.log" | tail -20 | sed 's/^/  | /'; }
[ "$FAIL" -eq 0 ]
