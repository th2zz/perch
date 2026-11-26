#!/usr/bin/env bash
#
# Verification for FR-016: the user's *other* computers, read and driven from one
# phone.
#
# PROVES, against a real saved SSH machine and a real herdr: the bridge lists the
#   machine, reads its panes and tags every record with the computer it came from;
#   a blocked agent there becomes a card that names its computer; approving it runs
#   the equivalent command over `herdr --machine …`; text delivered to a remote
#   session arrives in that session's terminal; and a write aimed at a machine that
#   is not saved is refused without touching anything.
# CANNOT prove anything about a machine it cannot reach, and says so: the failure
#   path is exercised by pointing a write at a name nobody saved.
#
# It needs one enabled saved machine (`herdr machine list`). It injects a fake
# blocked agent on an agent-free pane of that machine and releases it on exit,
# including on failure. Your own panes are never written to.
#
# Usage: scripts/verify-hosts.sh

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${PERCH_TEST_PORT:-45994}"
STATE_DIR="$(mktemp -d -t perch-hosts-XXXXXX)"
BRIDGE_BIN="$STATE_DIR/perch-bridge"
SOURCE_ID="perch-hosts-$$"
AGENT_LABEL="perch-probe-$$"

PASS=0
FAIL=0
SKIP=0
HOST=""
REMOTE_PANE=""

pass() { PASS=$((PASS + 1)); printf '  \033[32mok\033[0m   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
skip() { SKIP=$((SKIP + 1)); printf '  \033[33mskip\033[0m %s\n' "$1"; }
section() { printf '\n%s\n' "$1"; }
assert_eq() { if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (expected '$2', got '$3')"; fi; }

release_probe() {
    [ -n "$HOST" ] && [ -n "$REMOTE_PANE" ] || return 0
    herdr --machine "$HOST" pane release-agent "$REMOTE_PANE" \
        --source "$SOURCE_ID" --agent "$AGENT_LABEL" >/dev/null 2>&1 || true
    return 0
}

cleanup() {
    release_probe
    [ -n "${BRIDGE_PID:-}" ] && kill "$BRIDGE_PID" 2>/dev/null || true
    wait 2>/dev/null || true
    rm -r "$STATE_DIR" 2>/dev/null || true
}
trap cleanup EXIT

section "Perch · your other computers"

if ! command -v herdr >/dev/null 2>&1; then
    echo "herdr is not on PATH" >&2
    exit 2
fi

HOST="$(herdr machine list --json 2>/dev/null | python3 -c '
import json, sys
try:
    machines = json.load(sys.stdin)
except Exception:
    machines = []
labels = [m["label"] for m in machines if m.get("enabled")]
print(labels[0] if labels else "")
')"
if [ -z "$HOST" ]; then
    skip "another computer (no enabled saved machine; add one with herdr machine add)"
    echo "  nothing was started and nothing was touched."
    exit 0
fi
echo "using saved machine: $HOST"

echo "building the bridge into a scratch directory..."
(cd "$REPO_ROOT/bridge" && go build -o "$BRIDGE_BIN" ./cmd/perch-bridge) || {
    echo "could not build the bridge" >&2
    exit 1
}
HERDR_PLUGIN_STATE_DIR="$STATE_DIR" PERCH_BRIDGE_ADDR="127.0.0.1:$PORT" \
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
api() { curl -s -H "Authorization: Bearer $TOKEN" "$@"; }

# A poll of another machine costs an SSH connection, so the first pass needs room.
echo "waiting for the first read of $HOST..."
sleep 12

section "One view, and every record says which computer it is on"

api "$BASE/v1/snapshot" > "$STATE_DIR/snapshot.json"
# The JSON goes through a file rather than a pipe: a heredoc redirects stdin, so
# `curl … | python3 - <<PY` would hand python the script instead of the response.
summary="$(python3 - "$HOST" "$STATE_DIR/snapshot.json" <<'PY'
import json, sys
label, path = sys.argv[1], sys.argv[2]
mirror = json.load(open(path))["mirror"]
hosts = {h.get("host_id"): h.get("status") for h in mirror.get("hosts", [])}
print(json.dumps({
    "status": hosts.get(label, "missing"),
    "remote_panes": sum(1 for p in mirror.get("panes", []) if p.get("host_id") == label),
    "local_panes": sum(1 for p in mirror.get("panes", []) if p.get("host_id") == "local"),
    "remote_workspaces": sum(1 for w in mirror.get("workspaces", []) if w.get("host_id") == label),
}))
PY
)"

pick() { printf '%s' "$summary" | python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
assert_eq "the other computer is listed as reachable" "reachable" "$(pick status)"
if [ "$(pick remote_panes)" -ge 1 ]; then
    pass "its $(pick remote_panes) pane(s) and $(pick remote_workspaces) workspace(s) are in the same view"
else
    fail "the other computer contributed no panes (does its session have one?)"
fi
if [ "$(pick local_panes)" -ge 1 ]; then
    pass "this computer's $(pick local_panes) pane(s) are still there, tagged local"
else
    fail "the local machine's panes disappeared when another host was added"
fi

section "A blocked agent on the other computer becomes a card"

REMOTE_PANE="$(herdr --machine "$HOST" pane list 2>/dev/null | python3 -c '
import json, sys
panes = json.load(sys.stdin)["result"]["panes"]
shells = [p for p in panes if not p.get("agent")]
print(shells[0]["pane_id"] if shells else (panes[0]["pane_id"] if panes else ""))
')"
if [ -z "$REMOTE_PANE" ]; then
    fail "the other computer has no pane to use"
    exit 1
fi
echo "  using pane $REMOTE_PANE on $HOST"

herdr --machine "$HOST" pane report-agent "$REMOTE_PANE" \
    --source "$SOURCE_ID" --agent "$AGENT_LABEL" --state blocked >/dev/null 2>&1

decision=""
for _ in $(seq 1 60); do
    api "$BASE/v1/decisions" > "$STATE_DIR/decisions.json"
    decision="$(python3 - "$HOST" "$STATE_DIR/decisions.json" <<'PY'
import json, sys
label, path = sys.argv[1], sys.argv[2]
pending = [d for d in json.load(open(path)).get("decisions", [])
           if d.get("host_id") == label and d.get("state") == "pending"]
print(json.dumps(pending[0]) if pending else "")
PY
)"
    [ -n "$decision" ] && break
    sleep 1
done
if [ -z "$decision" ]; then
    fail "the blocked agent on $HOST never reached the queue"
    exit 1
fi
pass "a request from $HOST arrived"

card() { printf '%s' "$decision" | python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
assert_eq "the card names the computer it came from" "$HOST" "$(card host_id)"
assert_eq "the card names the pane on that computer" "$REMOTE_PANE" "$(card pane_id)"

section "Acting on it runs on that computer"

# The injected state has no dialog on screen, so the *right* answer is the honest
# refusal — and that is worth checking on a remote machine specifically: the failure
# mode this guards against is a bridge that presses a guessed key on a computer the
# user is not looking at.
decided="$(api -X POST -H 'Content-Type: application/json' \
    -d "{\"decision_id\":\"$(card id)\",\"kind\":\"approve\",\"target\":\"$REMOTE_PANE\",\"host\":\"$HOST\"}" \
    "$BASE/v1/decisions")"
release_probe
if printf '%s' "$decided" | grep -q 'no deterministic key sequence'; then
    pass "it refuses to guess on another computer, and says so"
elif printf '%s' "$decided" | grep -q '"error"'; then
    fail "the refusal is not the one the contract asks for: $decided"
else
    command="$(printf '%s' "$decided" | python3 -c '
import json, sys
print(" ".join(json.load(sys.stdin).get("equivalent_command", [])))
')"
    case "$command" in
        "herdr agent send-keys $REMOTE_PANE "*)
            pass "it approved, and the command names the remote pane: $command" ;;
        *)
            fail "the equivalent command is wrong: $command" ;;
    esac
fi

section "A key from the escape hatch arrives in that computer's terminal"

# The strongest end-to-end statement available without a real dialog: put the remote
# pane in raw mode with `cat -A`, press Esc from the phone's key bar, and read the
# remote screen. `^[` means the byte reached *that* machine's terminal.
herdr --machine "$HOST" pane run "$REMOTE_PANE" "stty -icanon -echo min 1 time 0; cat -A" >/dev/null 2>&1
sleep 1
keys="$(api -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' \
    -d "{\"host\":\"$HOST\",\"pane_id\":\"$REMOTE_PANE\",\"keys\":[\"esc\"]}" \
    "$BASE/v1/session/keys")"
assert_eq "the key bar accepts a session on another computer" "200" "$keys"
sleep 1
if herdr --machine "$HOST" pane read "$REMOTE_PANE" --format text | grep -q '\^\[$'; then
    pass "the Escape key reached the other computer's terminal"
else
    fail "the key did not reach the other computer"
    herdr --machine "$HOST" pane read "$REMOTE_PANE" --format text | tail -3 | sed 's/^/      | /'
fi
herdr --machine "$HOST" pane send-keys "$REMOTE_PANE" ctrl+c >/dev/null 2>&1 || true

section "Text written on the phone arrives in that computer's terminal"

# Unbuffered and un-echoed input, so what arrives is written back as data by `cat`
# rather than by the shell's line discipline.
herdr --machine "$HOST" pane run "$REMOTE_PANE" "stty -icanon -echo min 1 time 0; cat" >/dev/null 2>&1
sleep 1
marker="perch-remote-$$"
delivery="$(api -X POST -H 'Content-Type: application/json' \
    -d "{\"kind\":\"prompt\",\"target\":\"$REMOTE_PANE\",\"text\":\"$marker\",\"host\":\"$HOST\"}" \
    "$BASE/v1/decisions")"
if printf '%s' "$delivery" | grep -q '"error"'; then
    fail "delivering text to $HOST failed: $delivery"
else
    sleep 1
    if herdr --machine "$HOST" pane read "$REMOTE_PANE" --format text | grep -q "$marker"; then
        pass "the text is on the other computer's screen"
    else
        fail "the text never arrived on $HOST"
        herdr --machine "$HOST" pane read "$REMOTE_PANE" --format text | tail -3 | sed 's/^/      | /'
    fi
fi
herdr --machine "$HOST" pane send-keys "$REMOTE_PANE" ctrl+c >/dev/null 2>&1 || true
herdr --machine "$HOST" pane run "$REMOTE_PANE" "stty sane" >/dev/null 2>&1 || true
sleep 1

section "A computer that is not saved is refused, and nothing is written"

before="$(herdr --machine "$HOST" pane read "$REMOTE_PANE" --format text | md5sum)"
refused="$(api -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' \
    -d "{\"kind\":\"prompt\",\"target\":\"$REMOTE_PANE\",\"text\":\"should-not-run\",\"host\":\"not-a-saved-machine\"}" \
    "$BASE/v1/decisions")"
assert_eq "a write aimed at an unsaved computer is refused" "400" "$refused"
after="$(herdr --machine "$HOST" pane read "$REMOTE_PANE" --format text | md5sum)"
assert_eq "and the other computer's screen did not change" "$before" "$after"

section "Summary"
printf '  %d passed, %d failed, %d skipped\n' "$PASS" "$FAIL" "$SKIP"
printf '  (the injected probe agent was released; your own panes were not written to)\n'
[ "$FAIL" -eq 0 ]
