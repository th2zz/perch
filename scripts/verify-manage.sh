#!/usr/bin/env bash

set -uo pipefail

LAB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$LAB_DIR/.." && pwd)"
export PATH="$HOME/.local/opt/go/bin:$PATH"
PORT="${PERCH_MANAGE_PORT:-45997}"
PASS=0
FAIL=0

STATE_DIR="$(mktemp -d -t perch-manage-XXXXXX)"
BRIDGE_BIN="$STATE_DIR/perch-bridge"

cleanup() {
    [ -n "${BRIDGE_PID:-}" ] && kill "$BRIDGE_PID" 2>/dev/null
    # Never leave a scratch workspace behind on somebody's machine: if the script dies half way, the
    # workspace it made goes with it.
    if [ -n "${WS_ID:-}" ]; then
        herdr workspace close "$WS_ID" >/dev/null 2>&1 || true
    fi
    if [ -n "${CREATED_WS_ID:-}" ]; then
        herdr workspace close "$CREATED_WS_ID" >/dev/null 2>&1 || true
    fi
    if [ -n "${DSH_WS_ID:-}" ]; then
        herdr workspace close "$DSH_WS_ID" >/dev/null 2>&1 || true
    fi
    rm -r "$STATE_DIR" 2>/dev/null
}
trap cleanup EXIT

pass() { PASS=$((PASS + 1)); printf '  \033[32mok\033[0m   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
say() { printf '\n\033[36m%s\033[0m\n' "$1"; }

if ! command -v herdr >/dev/null 2>&1; then
    echo "no herdr on this machine; nothing to verify against"
    exit 2
fi

(cd "$REPO_ROOT/bridge" && go build -o "$BRIDGE_BIN" ./cmd/perch-bridge) || { echo "the bridge did not build"; exit 1; }

say "a bridge of our own, against the herdr that is already running"
PERCH_BRIDGE_ADDR="127.0.0.1:$PORT" HERDR_PLUGIN_STATE_DIR="$STATE_DIR" \
    "$BRIDGE_BIN" run >"$STATE_DIR/bridge.log" 2>&1 &
BRIDGE_PID=$!
for _ in $(seq 1 60); do
    [ -f "$STATE_DIR/pairing.json" ] && break
    sleep 0.25
done
if [ ! -f "$STATE_DIR/pairing.json" ]; then
    echo "the bridge never started (see $STATE_DIR/bridge.log)"; exit 1
fi
TOKEN="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' "$STATE_DIR/pairing.json")"
pass "the bridge is up (and this is *our* instance, not the one the phone uses)"

# `herdr pane current` answers only *inside* a pane (it reads HERDR_ENV from the environment), and this script
# runs from a plain shell — where it fails. A check that silently compares two empty strings is worse than no
# check, so the focused pane is read from the snapshot, which is authoritative from anywhere.
focused_pane() {
    herdr api snapshot 2>/dev/null | python3 -c '
import json,sys
try:
    snap = json.load(sys.stdin)["result"]["snapshot"]
except Exception:
    print("")
    raise SystemExit
print(snap.get("focused_pane_id") or "")
'
}
# The scratch workspace is created first so that its own arrival cannot be mistaken for a focus change.
FOCUS_BEFORE="$(focused_pane)"

say "a scratch project, created without stealing anybody's focus"
CREATE="$(herdr workspace create --no-focus --label "perch-manage-test" --cwd /tmp 2>&1)"
# Found in the snapshot rather than parsed out of the command's own output: the snapshot is the authoritative
# shape (ids, labels, focus), and grepping a pretty-printed line is how a script ends up with `w[` as an id.
WS_ID="$(herdr api snapshot 2>/dev/null | python3 -c '
import json,sys
snap = json.load(sys.stdin)["result"]["snapshot"]
print(next((w["workspace_id"] for w in snap["workspaces"] if w.get("label") == "perch-manage-test"), ""))
')"
if [ -z "$WS_ID" ]; then
    echo "could not work out the scratch workspace's id; herdr said: $CREATE"
    exit 1
fi
pass "scratch project $WS_ID exists"

post() { # path, json
    curl -s -o "$STATE_DIR/out.json" -w '%{http_code}' \
        -X POST "http://127.0.0.1:$PORT$1" \
        -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
        --data "$2"
}

say "create a project and start sessions through the app's doors"
if command -v codex >/dev/null 2>&1; then
    CREATE_LABEL="perch-create-test-$$"
    CODE="$(post /v1/projects "{\"label\":\"$CREATE_LABEL\",\"cwd\":\"/tmp\",\"kind\":\"codex\",\"name\":\"perch-create-one\"}")"
    CREATED_WS_ID="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("workspace_id",""))' "$STATE_DIR/out.json" 2>/dev/null || true)"
    if [ "$CODE" = "200" ] && [ -n "$CREATED_WS_ID" ]; then
        pass "created project $CREATED_WS_ID: $(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("equivalent"))' "$STATE_DIR/out.json")"
    else
        fail "creating a project returned $CODE: $(head -c 240 "$STATE_DIR/out.json")"
    fi
    if [ -n "$CREATED_WS_ID" ] && herdr workspace list 2>/dev/null | grep -q "$CREATE_LABEL"; then
        pass "herdr's list shows the new project"
    else
        fail "herdr's list does not show the new project"
    fi

    CODE="$(post /v1/sessions "{\"workspace_id\":\"$CREATED_WS_ID\",\"kind\":\"codex\",\"name\":\"perch-create-two\"}")"
    SECOND_TAB="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("tab_id",""))' "$STATE_DIR/out.json" 2>/dev/null || true)"
    if [ "$CODE" = "200" ] && [ -n "$SECOND_TAB" ]; then
        pass "started a second session in $CREATED_WS_ID: $(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("equivalent"))' "$STATE_DIR/out.json")"
    else
        fail "starting a session returned $CODE: $(head -c 240 "$STATE_DIR/out.json")"
    fi
    sleep 2
    if herdr api snapshot 2>/dev/null | python3 -c "
import json,sys
snap = json.load(sys.stdin)['result']['snapshot']
panes = [p for p in snap['panes'] if p.get('workspace_id') == '$CREATED_WS_ID']
raise SystemExit(0 if len(panes) >= 2 else 1)
"; then
        pass "the new project has both sessions in herdr"
    else
        fail "the new project does not show both sessions in herdr"
    fi
    herdr workspace close "$CREATED_WS_ID" >/dev/null 2>&1 || true
    CREATED_WS_ID=""
else
    say "codex is not on PATH; skipping the creation half"
fi

say "dsh launches its CLI, not the web profile"
if command -v dsh-tui >/dev/null 2>&1; then
    DSH_LABEL="perch-dsh-test-$$"
    CODE="$(post /v1/projects "{\"label\":\"$DSH_LABEL\",\"cwd\":\"/tmp\",\"kind\":\"dsh-tui\",\"name\":\"perch-dsh\"}")"
    DSH_WS_ID="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("workspace_id",""))' "$STATE_DIR/out.json" 2>/dev/null || true)"
    if [ "$CODE" = "200" ] && [ -n "$DSH_WS_ID" ]; then
        pass "created dsh project $DSH_WS_ID: $(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("equivalent"))' "$STATE_DIR/out.json")"
    else
        fail "creating a dsh project returned $CODE: $(head -c 300 "$STATE_DIR/out.json")"
    fi
    if [ -n "$DSH_WS_ID" ]; then
        sleep 3
        if herdr api snapshot 2>/dev/null | python3 -c "import json,sys; snap=json.load(sys.stdin)['result']['snapshot']; raise SystemExit(0 if any(p.get('workspace_id')=='$DSH_WS_ID' and p.get('agent')=='dsh-tui' for p in snap['panes']) else 1)"; then
            pass "herdr reports the dsh pane as agent dsh-tui"
        else
            fail "herdr did not report the dsh pane as agent dsh-tui"
        fi
        herdr workspace close "$DSH_WS_ID" >/dev/null 2>&1 || true
        DSH_WS_ID=""
    fi
else
    say "dsh-tui is not on PATH; skipping the dsh half"
fi

say "rename a project through the app's door"
CODE="$(post /v1/rename "{\"target\":\"project\",\"id\":\"$WS_ID\",\"label\":\"perch-manage-renamed\"}")"
if [ "$CODE" = "200" ] && grep -q "perch-manage-renamed" "$STATE_DIR/out.json"; then
    pass "the bridge accepted it and reported: $(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("equivalent"))' "$STATE_DIR/out.json")"
else
    fail "rename returned $CODE: $(head -c 200 "$STATE_DIR/out.json")"
fi
if herdr workspace list 2>/dev/null | grep -q "perch-manage-renamed"; then
    pass "herdr's own list shows the new name — the rename really happened"
else
    fail "herdr's list does not show the new name"
fi

say "a bad name is refused here, in words a person can act on"
CODE="$(post /v1/rename "{\"target\":\"project\",\"id\":\"$WS_ID\",\"label\":\"   \"}")"
if [ "$CODE" = "400" ] && grep -q "cannot be empty" "$STATE_DIR/out.json"; then
    pass "an empty name was refused: $(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["error"])' "$STATE_DIR/out.json")"
else
    fail "an empty name returned $CODE: $(head -c 200 "$STATE_DIR/out.json")"
fi
CODE="$(post /v1/rename "{\"target\":\"project\",\"id\":\"$WS_ID\",\"label\":\"two\\nlines\"}")"
if [ "$CODE" = "400" ]; then
    pass "a name with a newline in it was refused"
else
    fail "a name with a newline returned $CODE"
fi
CODE="$(post /v1/rename "{\"target\":\"nonsense\",\"id\":\"$WS_ID\",\"label\":\"x\"}")"
if [ "$CODE" = "400" ]; then
    pass "an unknown target was refused rather than guessed"
else
    fail "an unknown target returned $CODE"
fi

say "a session: rename it, then close it"
pane_in_scratch() {
    herdr api snapshot 2>/dev/null | python3 -c "
import json,sys
snap = json.load(sys.stdin)['result']['snapshot']
print(' '.join(p['pane_id'] for p in snap['panes'] if p.get('workspace_id') == '$WS_ID'))
"
}
ROOT_PANE="$(pane_in_scratch | awk '{print $1}')"
if [ -z "$ROOT_PANE" ]; then
    # Measured: `workspace create` always brings a root pane with it, so this is not expected. If it ever
    # happens, the session half is skipped rather than worked around by focusing the scratch workspace —
    # moving somebody's cursor to run a test is not a trade this script gets to make.
    say "the scratch workspace has no pane of its own; skipping the session half"
fi

# **Measured, 2026-10-15, and it changed the product:** closing the *last* pane of a workspace closes the
# workspace with it — herdr has no such thing as an empty workspace, so `pane close` on the only pane takes
# the project away too. That is the difference between "close this session" and "close this project", and the
# app has to say so before somebody taps it on the last session of a project. So the scratch workspace gets a
# second pane, and the session half runs on that one — the project is then still there to be closed on purpose.
if [ -n "$ROOT_PANE" ]; then
    herdr pane split --pane "$ROOT_PANE" --direction down --cwd /tmp >/dev/null 2>&1 || true
    PANE_ID="$(pane_in_scratch | awk '{print $2}')"
    if [ -n "$PANE_ID" ]; then
        pass "a second session in the scratch project ($PANE_ID), so closing one does not take the project"
    else
        fail "could not split a second pane in the scratch workspace"
    fi
fi

if [ -z "$PANE_ID" ]; then
    fail "no pane to work with; skipping the session half"
else
    CODE="$(post /v1/rename "{\"target\":\"session\",\"id\":\"$PANE_ID\",\"label\":\"perch-renamed-pane\"}")"
    if [ "$CODE" = "200" ]; then
        pass "renamed session $PANE_ID: $(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("equivalent"))' "$STATE_DIR/out.json")"
    else
        fail "renaming the session returned $CODE: $(head -c 200 "$STATE_DIR/out.json")"
    fi

    CODE="$(post /v1/close "{\"target\":\"session\",\"id\":\"$PANE_ID\"}")"
    if [ "$CODE" = "200" ]; then
        pass "closed session $PANE_ID: $(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("equivalent"))' "$STATE_DIR/out.json")"
    else
        fail "closing the session returned $CODE: $(head -c 200 "$STATE_DIR/out.json")"
    fi
    if herdr pane list 2>/dev/null | grep -q "$PANE_ID"; then
        fail "the pane is still in herdr's list after being closed"
    else
        pass "and it is gone from herdr's list"
    fi
    if herdr api snapshot 2>/dev/null | python3 -c "
import json,sys
snap = json.load(sys.stdin)['result']['snapshot']
raise SystemExit(0 if any(w['workspace_id'] == '$WS_ID' for w in snap['workspaces']) else 1)
"; then
        pass "the project is still there — closing a session is not closing a project (as long as it was not the last one)"
    else
        fail "the project vanished when one of its sessions closed"
    fi
fi

say "closing a whole project: refused without confirmation, done with it"
CODE="$(post /v1/close "{\"target\":\"project\",\"id\":\"$WS_ID\"}")"
if [ "$CODE" = "400" ] && grep -q "cannot be undone" "$STATE_DIR/out.json"; then
    pass "refused: $(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["error"])' "$STATE_DIR/out.json")"
else
    fail "an unconfirmed project close returned $CODE: $(head -c 200 "$STATE_DIR/out.json")"
fi
if herdr workspace list 2>/dev/null | grep -q "$WS_ID"; then
    pass "and the project is still there after the refusal"
else
    fail "the project disappeared even though the close was refused"
fi

CODE="$(post /v1/close "{\"target\":\"project\",\"id\":\"$WS_ID\",\"confirmed\":true}")"
if [ "$CODE" = "200" ]; then
    pass "confirmed close accepted: $(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("equivalent"))' "$STATE_DIR/out.json")"
    WS_ID=""
else
    fail "a confirmed project close returned $CODE: $(head -c 200 "$STATE_DIR/out.json")"
fi

say "and nothing moved for the person at the computer"
FOCUS_AFTER="$(focused_pane)"
if [ -z "$FOCUS_BEFORE" ] || [ -z "$FOCUS_AFTER" ]; then
    # Refusing to call this a pass: "could not tell" is not "nothing moved".
    fail "could not work out which pane has focus, so this cannot be checked ($FOCUS_BEFORE -> $FOCUS_AFTER)"
elif [ "$FOCUS_BEFORE" = "$FOCUS_AFTER" ]; then
    pass "the focused pane is the same as before ($FOCUS_AFTER)"
else
    fail "focus moved: was $FOCUS_BEFORE, now $FOCUS_AFTER"
fi

printf '\n%s passed, %s failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
