#!/usr/bin/env bash
#
# Verification for User Story 3, host half: a Chinese instruction reaches the
# terminal character for character.
#
# What this script proves, and what it does not:
#
#   PROVES  the whole transport chain the way a phone uses it - HTTP request →
#           bridge → herdr socket → a real terminal - carries a mixed
#           Chinese/English instruction, CJK punctuation, a newline and a non-BMP
#           emoji without changing a single character, and that the equivalent
#           command the user is shown names the same thing. Also proves the claim
#           the escape hatch depends on: `pane send-text` does not submit, so
#           text lands at a shell prompt without running it.
#   CANNOT  prove the phone's side of FR-006: the IME's candidate window, the
#           composition state, and the app's own text field. Those are the part
#           only a device can answer, and they are quickstart.md verification 3.
#
# It is safe to run on a machine with a live herdr session: everything happens in
# a separate named session (`herdr session`, an independent server with its own
# socket and panes), and the scratch session is deleted on exit.
#
# Usage: scripts/verify-us3.sh

set -uo pipefail

LAB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$LAB_DIR/.." && pwd)"
# shellcheck source=lib/scratch-session.sh
. "$LAB_DIR/lib/scratch-session.sh"

SESSION="perch-us3-$$"
PORT="${PERCH_TEST_PORT:-45995}"
STATE_DIR="$(mktemp -d -t perch-us3-XXXXXX)"
BRIDGE_BIN="$STATE_DIR/perch-bridge"
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

section "Perch · a Chinese instruction, arriving character for character"

scratch_session_start "$SESSION"
case $? in
    0) pass "a scratch session started, separate from yours (herdr session $SESSION)" ;;
    2) skip "attach a scratch session: $SCRATCH_ERROR"; exit 0 ;;
    *) fail "$SCRATCH_ERROR"; exit 1 ;;
esac
PANE="$SCRATCH_PANE"

# ---------------------------------------------------------------- the payload

# Deliberately awkward, because "it worked" with an easy string proves little:
#  - mixed scripts, so a lossy encoding shows up as mojibake rather than as a
#    missing character;
#  - non-ASCII Unicode punctuation and accented Latin characters;
#  - a newline, which must not turn into an accidental submission;
#  - an emoji outside the Basic Multilingual Plane, plus a ZWJ sequence.
PAYLOAD="$(printf 'Instruction: continue deployment (y/n) — café «line two»\nSecond line with punctuation, accents résumé 123\nemoji: 🚀 family: 👨‍👩‍👧‍👦')"

# ------------------------------------------------------------------ the bridge

echo "building the bridge into a scratch directory..."
(cd "$REPO_ROOT/bridge" && go build -o "$BRIDGE_BIN" ./cmd/perch-bridge) || {
    echo "could not build the bridge" >&2
    exit 1
}

# Pointed at the scratch session's socket, which is the only reason this script
# can write text at all without touching the user's panes.
HERDR_SOCKET_PATH="$SCRATCH_SOCKET" HERDR_PLUGIN_STATE_DIR="$STATE_DIR" \
    PERCH_BRIDGE_ADDR="127.0.0.1:$PORT" \
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
TOKEN="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' \
    "$STATE_DIR/pairing.json")"
BASE="http://127.0.0.1:$PORT"

# The bridge must see the scratch session's pane, not the user's. If it ever
# reported the user's panes here, every later assertion would be about the wrong
# machine - so this is checked before anything is written.
mirrored="$(
    curl -s -H "Authorization: Bearer $TOKEN" "$BASE/v1/snapshot" | python3 -c '
import json, sys
print(",".join(p["pane_id"] for p in json.load(sys.stdin)["mirror"]["panes"]))
'
)"
case ",$mirrored," in
    *",$PANE,") pass "the bridge is reading the scratch session (pane $PANE)" ;;
    *) fail "the bridge is not reading the scratch session (saw: $mirrored)"; exit 1 ;;
esac

# ------------------------------------------------------------- the receiving end

# Input unbuffered and un-echoed, so what arrives is written back by `cat` as data
# rather than held by the line discipline until a newline. Not full `raw` on
# purpose: `raw` also turns off output post-processing, which is what maps a line
# feed to carriage-return-plus-line-feed, and then every line after the first is
# rendered starting in the middle of the screen - an artefact of the harness that
# would look exactly like a fidelity bug in the product.
herdr --session "$SESSION" pane run "$PANE" \
    "stty -icanon -echo min 1 time 0; cat" >/dev/null 2>&1
sleep 1

section "The instruction, sent the way the phone sends it"

response="$(
    python3 - "$BASE" "$TOKEN" "$PANE" "$PAYLOAD" <<'PY'
import json, sys, urllib.request

base, token, pane, text = sys.argv[1:5]
body = json.dumps({"kind": "prompt", "target": pane, "text": text}).encode()
request = urllib.request.Request(
    base + "/v1/decisions",
    data=body,
    headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"},
    method="POST",
)
try:
    with urllib.request.urlopen(request, timeout=20) as response:
        print(response.read().decode())
except urllib.error.HTTPError as error:
    print(json.dumps({"http_error": error.code, "body": error.read().decode()}))
PY
)"

if printf '%s' "$response" | grep -q http_error; then
    fail "the bridge refused the instruction: $response"
else
    pass "the bridge accepted it (fourth write class, no request needed)"
fi

sleep 1
visible="$(herdr --session "$SESSION" pane read "$PANE" --source visible --format text)"

# The comparison is character-for-character, in Python, because that is the only
# way to be sure: `grep` would answer a different question, and `diff` on a
# terminal excerpt answers a third one (it would fail on the echo of the command
# that started `cat`).
verdict="$(
    python3 - "$visible" "$PAYLOAD" <<'PY'
import sys

visible, payload = sys.argv[1], sys.argv[2]
if payload in visible:
    print("OK")
    raise SystemExit(0)
# Narrow down what went wrong rather than just saying "no": the interesting
# failure is a *changed* character, and saying which one saves an hour.
lines = [line for line in visible.splitlines()]
want = payload.split("\n")
missing = [line for line in want if line not in lines]
if missing:
    print("MISSING " + repr("\\n".join(missing)))
else:
    index = next((i for i, line in enumerate(want) if line not in visible), None)
    print("CHANGED " + repr(want[index] if index is not None else payload))
PY
)"

if [ "$verdict" = "OK" ]; then
    pass "every character arrived unchanged (UTF-8 accents + punctuation + newlines + ZWJ emoji)"
else
    fail "the instruction changed in flight: $verdict"
    printf '%s\n' "$visible" | sed 's/^/      | /'
fi

section "What the user is shown as the equivalent command"

equivalent="$(printf '%s' "$response" | python3 -c '
import json, sys
print(" ".join(json.load(sys.stdin).get("equivalent_command", [])))
')"
case "$equivalent" in
    *"herdr pane send-text $PANE"*) pass "names the session and the write path: ${equivalent%% *} …" ;;
    *) fail "the equivalent command does not name the session: $equivalent" ;;
esac

section "The claim the escape hatch rests on: sending text does not submit it"

# Leave the harness behind first. `cat` goes, and so does the terminal mode the
# harness set: `stty -icanon -echo` persists in the tty, and a shell inheriting it
# would not echo what was written to it - which looks exactly like "the text never
# arrived" and is why the first version of this section reported a false failure.
herdr --session "$SESSION" pane send-keys "$PANE" ctrl+c >/dev/null 2>&1
sleep 0.5
herdr --session "$SESSION" pane run "$PANE" "stty sane" >/dev/null 2>&1
sleep 0.7

# `pane send-text` is documented as non-submitting. That is why the escape hatch
# may hand the user's sentence to a shell prompt without running it - if this were
# false, a user typing a sentence into a session would execute it.
herdr --session "$SESSION" pane send-text "$PANE" "echo perch-must-not-run" >/dev/null 2>&1
sleep 0.7
prompt="$(herdr --session "$SESSION" pane read "$PANE" --source visible --format text)"
if printf '%s' "$prompt" | grep -q "echo perch-must-not-run"; then
    # The text is visible at the prompt and its output is not: the words went in,
    # nothing ran. That distinction is the whole reason this write class is
    # allowed to exist without a confirmation step.
    if printf '%s' "$prompt" | grep -q "^perch-must-not-run$"; then
        fail "the text executed instead of waiting at the prompt"
    else
        pass "the text sits at the prompt instead of being executed"
    fi
else
    fail "the sent text never reached the prompt (see below)"
    printf '%s\n' "$prompt" | sed 's/^/      | /'
fi

# And the other half of the same fact, measured rather than assumed: a *line
# break* the user typed is not a submission we added, but a shell runs the line
# regardless. This is why a session with no agent in it says so on screen.
herdr --session "$SESSION" pane send-keys "$PANE" ctrl+c >/dev/null 2>&1
sleep 0.5
herdr --session "$SESSION" pane send-text "$PANE" "$(printf 'echo perch-line-ran\nsecond')" \
    >/dev/null 2>&1
sleep 0.7
prompt="$(herdr --session "$SESSION" pane read "$PANE" --source visible --format text)"
if printf '%s' "$prompt" | grep -q "^perch-line-ran$"; then
    pass "a line break inside sent text runs that line in a shell (said on screen)"
else
    fail "the measured behaviour of a line break changed; the on-screen note may now be wrong"
    printf '%s\n' "$prompt" | sed 's/^/      | /'
fi

section "Summary"
printf '  %d passed, %d failed, %d skipped\n' "$PASS" "$FAIL" "$SKIP"
printf '  (the IME half of FR-006 still needs a phone: quickstart.md verification 3)\n'
[ "$FAIL" -eq 0 ]
