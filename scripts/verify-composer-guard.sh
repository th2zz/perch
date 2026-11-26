#!/usr/bin/env bash

set -uo pipefail

LAB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$LAB_DIR/.." && pwd)"
# shellcheck source=lib/scratch-session.sh
. "$LAB_DIR/lib/scratch-session.sh"

SESSION="perch-composer-$$"
PORT="${PERCH_TEST_PORT:-45997}"
STATE_DIR="$(mktemp -d -t perch-composer-XXXXXX)"
BRIDGE_BIN="$STATE_DIR/perch-bridge"
DRAFT="partial unsent draft — not submitted"
# Named per run rather than "probe": a name is a live handle, and a leftover agent from
# an interrupted run makes the next one fail with `agent_name_taken` — which is exactly
# how this script was first written and first broke.
AGENT="probe_$$"
PASS=0
FAIL=0

pass() { PASS=$((PASS + 1)); printf '  \033[32mok\033[0m   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
section() { printf '\n%s\n' "$1"; }

cleanup() {
    [ -n "${BRIDGE_PID:-}" ] && kill "$BRIDGE_PID" 2>/dev/null || true
    scratch_session_cleanup
    rm -r "$STATE_DIR" 2>/dev/null || true
}
trap cleanup EXIT

section "Perch · text waiting in the terminal's own input line"

# The scratch pane inherits the *calling* directory, and codex asks for trust when it
# starts inside a repository — a dialog that leaves the agent `blocked` and this script
# with nothing to measure. `/tmp` is where the measured, quiet start happens (this was
# found by getting it wrong twice: once with the repository as the cwd, and once by
# retrying `agent start` with the same name, which fails with `agent_name_taken` because
# the first attempt really did start something).
cd /tmp || exit 1
scratch_session_start "$SESSION"
case $? in
    0) pass "a scratch session started, separate from yours (herdr session $SESSION)" ;;
    2) echo "skip: $SCRATCH_ERROR"; exit 0 ;;
    *) echo "$SCRATCH_ERROR" >&2; exit 1 ;;
esac
PANE="$SCRATCH_PANE"

# An agent whose input line is one we have measured. codex starts in a few seconds and
# needs no login beyond what is already on the machine.
#
# One attempt, and the error is kept: `agent start` refuses to retry under the same name
# once it has launched something, and "could not start an agent" with no reason is the
# kind of failure that costs an afternoon.
if ! start_error="$(herdr --session "$SESSION" agent start "$AGENT" --kind codex --pane "$PANE" 2>&1)"; then
    case "$start_error" in
        *agent_pane_busy*)
            herdr --session "$SESSION" pane send-keys "$PANE" ctrl+c >/dev/null 2>&1
            sleep 1
            if ! start_error="$(herdr --session "$SESSION" agent start "$AGENT" --kind codex --pane "$PANE" 2>&1)"; then
                fail "could not start codex in the scratch pane: $start_error"
                exit 1
            fi
            ;;
        *)
            fail "could not start codex in the scratch pane: $start_error"
            exit 1
            ;;
    esac
fi
pass "codex started in the scratch pane"

# Wait for the prompt, and answer a startup dialog if one appears. A blocked agent is
# deliberately invisible to the guard (its "input line" is a menu), so without this the
# draft assertion below would fail for a reason that has nothing to do with the guard.
status=""
for _ in $(seq 1 20); do
    status="$(herdr --session "$SESSION" agent get "$AGENT" 2>/dev/null |
        python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["agent"]["agent_status"])' 2>/dev/null)"
    case "$status" in
        idle|done) break ;;
        blocked) herdr --session "$SESSION" agent send-keys "$AGENT" enter >/dev/null 2>&1 ;;
    esac
    sleep 1
done
if [ "$status" = "idle" ] || [ "$status" = "done" ]; then
    pass "codex is at its prompt ($status)"
else
    fail "codex never reached its prompt (last status: ${status:-unknown})"
    exit 1
fi

echo "building the bridge into a scratch directory..."
(cd "$REPO_ROOT/bridge" && go build -o "$BRIDGE_BIN" ./cmd/perch-bridge) || {
    fail "could not build the bridge"
    exit 1
}

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

unsent() {
    curl -s -H "Authorization: Bearer $TOKEN" "$BASE/v1/session?pane_id=$PANE&lines=20" |
        python3 -c 'import json,sys; print(json.load(sys.stdin).get("unsent") or "")'
}

section "An empty input line says nothing"

if [ -z "$(unsent)" ]; then
    pass "the box is empty (the placeholder is dim, so it is not text)"
else
    fail "reported text where there is none: $(unsent)"
fi

section "A sentence typed at the agent and not submitted is reported"

# `pane send-text` is exactly the fourth write class: literal text, no Enter. So this is
# the state a person is in when they type at their desk, get interrupted, and then pick
# up their phone.
herdr --session "$SESSION" pane send-text "$PANE" "$DRAFT" >/dev/null 2>&1
sleep 1

got="$(unsent)"
if [ "$got" = "$DRAFT" ]; then
    pass "the bridge reports it word for word: $got"
else
    fail "reported $(printf '%q' "$got"), wanted $(printf '%q' "$DRAFT")"
fi

section "And the screen itself is unchanged by the guard"

text="$(
    curl -s -H "Authorization: Bearer $TOKEN" "$BASE/v1/session?pane_id=$PANE&lines=20" |
        python3 -c 'import json,sys; print(json.load(sys.stdin).get("text") or "")'
)"
case "$text" in
    *"$DRAFT"*) pass "the raw screen still carries the sentence, escapes and all" ;;
    *) fail "the screen lost the text the guard found" ;;
esac

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
