#!/usr/bin/env bash
#
# Verification for User Story 2's host-side half: does a decision reach a client,
# and does the client's answer become exactly one herdr command?
#
# What this script proves, and what it does not:
#
#   PROVES  the bridge's HTTP/WebSocket contract end to end against the user's
#           real herdr: authentication, the decision queue, the push payload,
#           the WebSocket greeting, the honest refusal path, and the
#           "already handled on the computer" transition.
#   CANNOT  prove the lock-screen latency, the touch count, or the CJK input
#           path. Those need a real phone: the emulator has no vendor push
#           channel, no real Doze and a different keyboard. See quickstart.md
#           verification 2 for the manual checklist.
#
# It is safe to run on a machine with a live herdr session: the bridge runs on
# its own port with its own state directory, and the one agent state it injects
# is released on exit, including when the script fails.
#
# Usage: scripts/verify-us2.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${PERCH_TEST_PORT:-45997}"
RELAY_PORT="${PERCH_TEST_RELAY_PORT:-45996}"
STATE_DIR="$(mktemp -d -t perch-verify-XXXXXX)"
BRIDGE_BIN="$STATE_DIR/perch-bridge"
RELAY_BIN="$STATE_DIR/perch-relay"
# Unique per run. Two runs sharing a source id is how the first version of this
# script left a fake blocked agent behind on a real pane, which then made the
# next run's assertions meaningless.
SOURCE_ID="perch-verify-$$"
AGENT_LABEL="perch-probe-$$"

PASS=0
FAIL=0
SKIP=0

pass() { PASS=$((PASS + 1)); printf '  \033[32mok\033[0m   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
skip() { SKIP=$((SKIP + 1)); printf '  \033[33mskip\033[0m %s\n' "$1"; }
section() { printf '\n%s\n' "$1"; }

assert_eq() { # name expected actual
    if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (expected '$2', got '$3')"; fi
}
assert_contains() { # name haystack needle
    case "$2" in
        *"$3"*) pass "$1" ;;
        *) fail "$1 (expected to contain '$3', got '$2')" ;;
    esac
}

cleanup() {
    if [ -n "${TEST_PANE:-}" ]; then
        herdr pane release-agent "$TEST_PANE" --source "$SOURCE_ID" --agent "$AGENT_LABEL" \
            >/dev/null 2>&1 || true
    fi
    [ -n "${BRIDGE_PID:-}" ] && kill "$BRIDGE_PID" 2>/dev/null || true
    [ -n "${RELAY_PID:-}" ] && kill "$RELAY_PID" 2>/dev/null || true
    wait 2>/dev/null || true
    # The state directory is this script's own scratch space.
    rm -r "$STATE_DIR" 2>/dev/null || true
}
trap cleanup EXIT

# Anything this script injected is released no matter how it exits, and a
# leftover from an interrupted run is reported rather than silently inherited.
preflight_leftovers() {
    herdr pane list | python3 -c '
import json, sys
leftovers = [p for p in json.load(sys.stdin)["result"]["panes"]
             if (p.get("agent") or "").startswith("perch-probe")]
for p in leftovers:
    sys.stderr.write(
        "warning: pane %s still carries a leftover probe agent (%s); "
        "release it with: herdr pane release-agent %s --source <source> --agent %s\n"
        % (p["pane_id"], p["agent"], p["pane_id"], p["agent"]))
'
}

section "Perch · User Story 2 host-side verification"

# ----------------------------------------------------------------- preflight

if ! command -v herdr >/dev/null 2>&1; then
    echo "herdr is not on PATH; this verification needs a live herdr session." >&2
    exit 2
fi
if ! herdr status >/dev/null 2>&1; then
    echo "herdr is not running; start it and try again." >&2
    exit 2
fi

# Pick a pane with no agent in it, so injecting a state cannot disturb real work.
TEST_PANE="$(
    herdr pane list | python3 -c '
import json, sys
panes = json.load(sys.stdin)["result"]["panes"]
shells = [p for p in panes if not p.get("agent")]
print(shells[0]["pane_id"] if shells else "")
'
)"
if [ -z "$TEST_PANE" ]; then
    echo "no agent-free pane available; open a plain shell pane and re-run." >&2
    exit 2
fi
echo "using pane $TEST_PANE (no agent running there)"
preflight_leftovers

# ------------------------------------------------------------------ build

echo "building bridge and relay into a scratch directory..."
(cd "$REPO_ROOT/bridge" && go build -o "$BRIDGE_BIN" ./cmd/perch-bridge)
(cd "$REPO_ROOT/relay" && go build -o "$RELAY_BIN" ./cmd/relay)

# ------------------------------------------------------------------ start

# Bound on every interface, exactly as the product does by default. Testing on
# loopback only would pass while the phone could never reach the computer — the
# one failure that makes the whole product useless.
HERDR_PLUGIN_STATE_DIR="$STATE_DIR" PERCH_BRIDGE_ADDR=":$PORT" \
    "$BRIDGE_BIN" run >"$STATE_DIR/bridge.log" 2>&1 &
BRIDGE_PID=$!

PERCH_RELAY_TOKEN=verify-token "$RELAY_BIN" -addr "127.0.0.1:$RELAY_PORT" \
    >"$STATE_DIR/relay.log" 2>&1 &
RELAY_PID=$!

for _ in $(seq 1 40); do
    [ -f "$STATE_DIR/pairing.json" ] && break
    sleep 0.25
done
TOKEN="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' \
    "$STATE_DIR/pairing.json")"
BASE="http://127.0.0.1:$PORT"

# The address a phone outside the house would use.
TAILNET_IP=""
if command -v tailscale >/dev/null 2>&1; then
    TAILNET_IP="$(tailscale ip -4 2>/dev/null | head -1)"
fi

# ----------------------------------------------------------------- checks

section "Authentication"

status="$(curl -s -o /dev/null -w '%{http_code}' "$BASE/v1/health")"
assert_eq "a request without a token is refused" "401" "$status"

status="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer wrong" "$BASE/v1/health")"
assert_eq "a request with the wrong token is refused" "401" "$status"

section "Read-only access"

# Reachability on the user's own network is the requirement underneath every
# other one: if the phone cannot open a socket to the computer, nothing else in
# this script matters. Loopback proves the server works; the tailnet address
# proves the phone will find it.
if [ -n "$TAILNET_IP" ]; then
    status="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 \
        -H "Authorization: Bearer $TOKEN" "http://$TAILNET_IP:$PORT/v1/health")"
    assert_eq "reachable on the tailnet address ($TAILNET_IP)" "200" "$status"
else
    skip "tailnet reachability (tailscale is not installed here)"
fi

health="$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE/v1/health")"
overall="$(echo "$health" | python3 -c 'import json,sys; print(json.load(sys.stdin)["health"]["overall"])')"
protocol="$(echo "$health" | python3 -c 'import json,sys; print(json.load(sys.stdin)["health"]["protocol"])')"
echo "  herdr protocol reported: $protocol"
[ "$overall" = "fail" ] && fail "check-up reports a failure (see \`perch-bridge doctor\`)" \
    || pass "check-up does not report a failure"

snapshot="$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE/v1/snapshot")"
workspaces="$(echo "$snapshot" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["mirror"]["workspaces"]))')"
panes="$(echo "$snapshot" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["mirror"]["panes"]))')"
hosts="$(echo "$snapshot" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["mirror"]["hosts"]))')"
echo "  saw $workspaces workspaces, $panes panes, $hosts host(s)"
[ "$workspaces" -gt 0 ] && pass "the mirror is not empty" || fail "the mirror is empty"
[ "$hosts" -gt 0 ] && pass "the local machine is reported as a host" || fail "no host reported"

section "The decision queue"

herdr pane report-agent "$TEST_PANE" --source "$SOURCE_ID" --agent "$AGENT_LABEL" --state blocked \
    >/dev/null
echo "  injected a blocked state into $TEST_PANE"

decision=""
for _ in $(seq 1 30); do
    decision="$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE/v1/decisions" | python3 -c '
import json, sys
pending = [d for d in json.load(sys.stdin)["decisions"] if d["state"] == "pending"]
print(pending[0]["id"] if pending else "")
')"
    [ -n "$decision" ] && break
    sleep 0.4
done

if [ -z "$decision" ]; then
    fail "a blocked agent produced a decision request"
else
    pass "a blocked agent produced a decision request"
    fields="$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE/v1/decisions" | python3 -c '
import json, sys
d = [x for x in json.load(sys.stdin)["decisions"] if x["state"] == "pending"][0]
print(d["pane_id"], d["agent_kind"], d["suggested_kind"], int(d["approvable"]), int(d["rejectable"]))
')"
    read -r pane_id agent_kind suggested approvable rejectable <<<"$fields"
    assert_eq "the request points at the pane that is waiting" "$TEST_PANE" "$pane_id"
    assert_eq "the agent kind is carried through" "$AGENT_LABEL" "$agent_kind"
    assert_eq "the default action is approve" "approve" "$suggested"
    if [ "$approvable$rejectable" = "00" ]; then
        pass "an unrecognised prompt offers no buttons instead of guessing"
    else
        fail "an unrecognised prompt offered buttons; the mapper must refuse"
    fi
    reason="$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE/v1/decisions" | python3 -c '
import json, sys
d = [x for x in json.load(sys.stdin)["decisions"] if x["state"] == "pending"][0]
print(d.get("unmappable_reason") or "")
')"
    [ -n "$reason" ] && pass "the card explains why it cannot be pressed" \
        || fail "no explanation was attached to an unpressed-able request"
fi

section "Action equivalence and honest refusal"

if [ -n "$decision" ]; then
    body="{\"decision_id\":\"$decision\",\"kind\":\"approve\",\"target\":\"$TEST_PANE\"}"
    response="$(curl -s -w '\n%{http_code}' -X POST -H "Authorization: Bearer $TOKEN" \
        -H 'Content-Type: application/json' -d "$body" "$BASE/v1/decisions")"
    code="$(echo "$response" | tail -1)"
    payload="$(echo "$response" | sed '$d')"
    assert_eq "approving an unmappable prompt is refused, not guessed" "422" "$code"
    assert_contains "the refusal names the reason" "$payload" "no deterministic key sequence"
    assert_contains "the refusal gives the user a next step" "$payload" "escape hatch"
fi

section "Wire contract"

status="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{\"decision_id\":\"$decision\",\"kind\":\"rename\",\"target\":\"$TEST_PANE\"}" \
    "$BASE/v1/decisions")"
assert_eq "a fifth kind of write is rejected" "400" "$status"

status="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{\"decision_id\":\"$decision\",\"kind\":\"approve\",\"target\":\"$TEST_PANE\",\"text\":\"x\"}" \
    "$BASE/v1/decisions")"
assert_eq "approve carrying text is rejected" "400" "$status"

section "Push payloads carry a summary and nothing else"

herdr_unused=0
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -d '{"device_token":"verify-device"}' "$BASE/v1/push/register" >/dev/null
HERDR_PLUGIN_STATE_DIR="$STATE_DIR" "$BRIDGE_BIN" pair --with-push \
    --gateway "http://127.0.0.1:$RELAY_PORT/v1/push" --push-token verify-token >/dev/null 2>&1 || true

# A payload carrying content must be refused by the gateway itself.
hostile="$(curl -s -X POST -H 'Authorization: Bearer verify-token' -H 'Content-Type: application/json' \
    -d '{"device_token":"d","decision_id":"x","host_label":"h","workspace_label":"w",
         "agent":"codex","kind":"approve","created_at":1,"text":"rm -rf /"}' \
    "http://127.0.0.1:$RELAY_PORT/v1/push")"
assert_contains "the gateway refuses a payload carrying prompt text" "$hostile" "not allowed"

# The agent resumes while the phone is still looking at the card. The card must
# stop asking, because the user almost certainly answered on the computer.
section "Already handled on the computer"

herdr pane release-agent "$TEST_PANE" --source "$SOURCE_ID" --agent "$AGENT_LABEL" >/dev/null
resolved=""
for _ in $(seq 1 30); do
    resolved="$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE/v1/decisions?closed=1" | python3 -c "
import json, sys
match = [d for d in json.load(sys.stdin)['decisions'] if d['id'] == '$decision']
print(match[0]['state'] if match else '')
")"
    [ "$resolved" = "handled_elsewhere" ] && break
    sleep 0.4
done
assert_eq "a request the agent resumed past becomes handled_elsewhere" "handled_elsewhere" "$resolved"

pending_now="$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE/v1/decisions" | python3 -c '
import json, sys
print(len([d for d in json.load(sys.stdin)["decisions"] if d["state"] == "pending"]))
')"
assert_eq "the queue is empty again" "0" "$pending_now"

# The stream is the latency path: a change has to reach a connected client
# without anyone polling. The pane is released first so that the injection below
# is a real state change rather than a repeat of a state already held.
section "Live event stream"

if command -v node >/dev/null 2>&1; then
    set +e
    reason="$(
        node - "$BASE" "$TOKEN" "$TEST_PANE" "$SOURCE_ID" "$AGENT_LABEL" <<'NODE'
const [, , base, token, pane, source, label] = process.argv;
const { execFileSync } = require("node:child_process");
const ws = new WebSocket(`${base.replace("http", "ws")}/v1/stream?token=${token}`);
const seen = new Set();
let injected = false;
const finish = (reason) => { try { ws.close(); } catch {} console.log(reason); process.exit(0); };
ws.onmessage = (e) => {
  const ev = JSON.parse(String(e.data));
  seen.add(ev.type);
  if (ev.type === "snapshot" && !injected) {
    injected = true;
    execFileSync("herdr", ["pane", "report-agent", pane, "--source", source,
                           "--agent", label, "--state", "blocked"]);
    return;
  }
  if (ev.type === "decision") finish("OK");
};
ws.onerror = () => finish("the WebSocket failed to connect");
setTimeout(() => finish(`no decision event within 15s (saw: ${[...seen].join(", ") || "nothing"})`), 15000);
NODE
    )"
    set -e
    if [ "$reason" = "OK" ]; then
        pass "a decision arrives over the WebSocket without polling"
    else
        fail "live event stream: $reason"
    fi
    herdr pane release-agent "$TEST_PANE" --source "$SOURCE_ID" --agent "$AGENT_LABEL" \
        >/dev/null 2>&1 || true
else
    skip "live event stream (node is not installed)"
fi

section "Summary"
printf '  %d passed, %d failed, %d skipped\n' "$PASS" "$FAIL" "$SKIP"
printf '  (lock-screen latency, touch count and CJK input still need a real phone: quickstart.md)\n'
[ "$FAIL" -eq 0 ]
