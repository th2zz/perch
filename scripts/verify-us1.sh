#!/usr/bin/env bash

set -uo pipefail

# Refuse to run where Perch is installed and in use.
#
# This script's subject *is* the install/uninstall path, so running it is running an
# uninstall: it stops the service, removes the unit, and deletes the state directory
# — including the token the phone is paired with. Found the honest way, by running it
# here minutes after installing Perch for the user's own trial and watching the
# installed bridge vanish.
#
# A refusal rather than a clever restore, because a script that re-creates what it
# deleted is no longer verifying what uninstall leaves behind.
if [ "${1:-}" != "--force" ]; then
    existing_unit="$HOME/.config/systemd/user/perch-bridge.service"
    [ -f "$existing_unit" ] || existing_unit="$HOME/Library/LaunchAgents/dev.perch.bridge.plist"
    if [ -f "$existing_unit" ] || pgrep -f "perch-bridge run" >/dev/null 2>&1; then
        cat >&2 <<'EOF'
Perch is installed on this machine (a service unit, or a bridge that is running),
and this verification *is* an uninstall: it stops the service, removes the unit and
deletes the state directory — including the token your phone is paired with.

Remove the installed copy first, then re-run:

    perch-bridge uninstall-service

Or run this again with --force if tearing it down is what you want.
EOF
        exit 2
    fi
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${PERCH_TEST_PORT:-45995}"
WORK="$(mktemp -d -t perch-us1-XXXXXX)"
BRIDGE_BIN="$WORK/perch-bridge"
STATE_DIR="$WORK/state"

PASS=0
FAIL=0
SKIP=0

pass() { PASS=$((PASS + 1)); printf '  \033[32mok\033[0m   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
skip() { SKIP=$((SKIP + 1)); printf '  \033[33mskip\033[0m %s\n' "$1"; }
section() { printf '\n%s\n' "$1"; }

cleanup() {
    [ -n "${BRIDGE_PID:-}" ] && kill "$BRIDGE_PID" 2>/dev/null
    wait 2>/dev/null
    # Never leave the developer's herdr with our plugin linked.
    herdr plugin unlink perch.bridge >/dev/null 2>&1 || true
    rm -r "$WORK" 2>/dev/null || true
}
trap cleanup EXIT

section "Perch · User Story 1 verification (you see what you had, nothing changed)"

if ! command -v herdr >/dev/null 2>&1 || ! herdr status >/dev/null 2>&1; then
    echo "herdr must be installed and running. Start it and try again." >&2
    exit 2
fi

echo "building the bridge..."
(cd "$REPO_ROOT/bridge" && go build -o "$BRIDGE_BIN" ./cmd/perch-bridge) || exit 1

run_bridge() {
    HERDR_PLUGIN_STATE_DIR="$STATE_DIR" PERCH_BRIDGE_ADDR=":$PORT" \
        "$BRIDGE_BIN" run >>"$WORK/bridge.log" 2>&1 &
    BRIDGE_PID=$!
    for _ in $(seq 1 60); do
        [ -f "$STATE_DIR/pairing.json" ] && return 0
        sleep 0.25
    done
    return 1
}

stop_bridge() {
    [ -n "${BRIDGE_PID:-}" ] && kill "$BRIDGE_PID" 2>/dev/null
    wait "$BRIDGE_PID" 2>/dev/null
    BRIDGE_PID=""
}

token() {
    python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' \
        "$STATE_DIR/pairing.json"
}

# ---------------------------------------------------------------- 1. baseline

section "Before: what your computer looked like"

"$BRIDGE_BIN" fingerprint --out "$WORK/before.json" >/dev/null 2>&1
if [ -f "$WORK/before.json" ]; then
    pass "recorded a fingerprint of herdr's config and plugin registry"
else
    fail "could not record the baseline fingerprint"
    exit 1
fi

# What herdr itself says the user has, to compare against what Perch reports.
herdr workspace list >"$WORK/workspaces-cli.json"
CLI_LABELS="$(python3 -c '
import json, sys
data = json.load(open(sys.argv[1]))
print(",".join(sorted(w["label"] for w in data["result"]["workspaces"])))
' "$WORK/workspaces-cli.json")"
echo "  herdr has: $CLI_LABELS"

# ---------------------------------------------------------------- 2. install

section "Install: link the plugin and start the bridge"

if herdr plugin link "$REPO_ROOT/plugin" >"$WORK/link.log" 2>&1; then
    pass "herdr accepted the plugin"
else
    fail "herdr refused to link the plugin (see $WORK/link.log)"
fi

if run_bridge; then
    pass "the bridge started"
else
    fail "the bridge did not start"
    exit 1
fi

TOKEN="$(token)"
BASE="http://127.0.0.1:$PORT"

# ---------------------------------------------------------------- 3. compare

section "After install: did anything of yours change?"

"$BRIDGE_BIN" fingerprint --baseline "$WORK/before.json" >"$WORK/after-install.txt" 2>&1
compare_status=$?
sed 's/^/  /' "$WORK/after-install.txt"
if [ "$compare_status" -eq 0 ]; then
    pass "nothing changed except Perch's own registration"
else
    fail "installing changed something it should not have"
fi

# ---------------------------------------------------------------- 4. the view

section "The view: your existing projects, not a copy"

MIRROR_LABELS="$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE/v1/snapshot" | python3 -c '
import json, sys
data = json.load(sys.stdin)
print(",".join(sorted(w["label"] for w in data["mirror"]["workspaces"])))
')"
echo "  Perch shows: $MIRROR_LABELS"
if [ "$CLI_LABELS" = "$MIRROR_LABELS" ]; then
    pass "Perch shows exactly the workspaces herdr has"
else
    fail "Perch's list differs from herdr's (cli=$CLI_LABELS mirror=$MIRROR_LABELS)"
fi

PANE_COUNT="$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE/v1/snapshot" | python3 -c '
import json, sys
print(len(json.load(sys.stdin)["mirror"]["panes"]))
')"
[ "$PANE_COUNT" -gt 0 ] && pass "it can see $PANE_COUNT panes" || fail "no panes visible"

# ------------------------------------------------- 5. cache is not authority

section "Clear the cache and restart: is the view rebuilt from the computer?"

stop_bridge
rm -r "$STATE_DIR" 2>/dev/null
if run_bridge; then
    REBUILT="$(curl -s -H "Authorization: Bearer $(token)" "$BASE/v1/snapshot" | python3 -c '
import json, sys
data = json.load(sys.stdin)
print(",".join(sorted(w["label"] for w in data["mirror"]["workspaces"])))
')"
    if [ "$REBUILT" = "$CLI_LABELS" ]; then
        pass "after wiping everything, the view is identical again"
    else
        fail "the rebuilt view differs: $REBUILT"
    fi
else
    fail "the bridge did not come back after the state directory was removed"
fi

# ---------------------------------------------------------------- 6. uninstall

section "Uninstall: is the machine back to where it started?"

stop_bridge
herdr plugin unlink perch.bridge >"$WORK/unlink.log" 2>&1
sleep 1

# The product's own uninstall, not this script's approximation of it: it stops
# any supervisor, unlinks the plugin, removes every directory the bridge may have
# written, and then checks itself against the baseline it was handed.
# HERDR_PLUGIN_STATE_DIR is set for this call on purpose: in production herdr
# injects that directory, and this is the path that has to be purged there.
if HERDR_PLUGIN_STATE_DIR="$STATE_DIR" "$BRIDGE_BIN" uninstall-service \
    --baseline "$WORK/before.json" >"$WORK/uninstall.txt" 2>&1; then
    pass "uninstall-service reported a clean machine"
else
    fail "uninstall-service found something left behind"
    sed 's/^/       /' "$WORK/uninstall.txt"
fi

"$BRIDGE_BIN" fingerprint --baseline "$WORK/before.json" >"$WORK/after-uninstall.txt" 2>&1
compare_status=$?
sed 's/^/  /' "$WORK/after-uninstall.txt"
if [ "$compare_status" -eq 0 ]; then
    pass "herdr's configuration is byte-identical to before"
else
    fail "something was left behind in the herdr configuration"
fi

if herdr plugin list 2>/dev/null | grep -qi "perch"; then
    fail "the plugin is still registered"
else
    pass "no Perch plugin remains registered"
fi

if curl -s -o /dev/null --max-time 2 "http://127.0.0.1:$PORT/v1/health"; then
    fail "something is still listening on $PORT"
else
    pass "nothing is listening any more"
fi

if [ -d "$STATE_DIR" ]; then
    fail "the state directory is still there: $STATE_DIR"
else
    pass "the state directory is gone"
fi

# herdr creates a directory for a linked plugin's state and does not remove it on
# unlink, so the product's uninstall is what has to. Checking it here is the
# difference between "zero residue" as a claim and as a fact.
HERDR_PLUGIN_STATE="$HOME/.local/state/herdr/plugins/perch.bridge"
if [ -d "$HERDR_PLUGIN_STATE" ]; then
    fail "uninstall left herdr's plugin state directory behind: $HERDR_PLUGIN_STATE"
else
    pass "herdr's plugin state directory was removed too"
fi

section "Summary"
printf '  %d passed, %d failed, %d skipped\n' "$PASS" "$FAIL" "$SKIP"
[ "$FAIL" -eq 0 ]
