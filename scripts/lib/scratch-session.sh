#!/usr/bin/env bash
#
# A throwaway herdr session, so a verification can use the real thing without
# touching the user's own panes.
#
# `herdr session` starts an *independent* server: its own socket, panes,
# workspaces and state (it shares only the global config file). That is what makes
# it the right sandbox for anything that types into a terminal — the alternative
# is typing into somebody's shell, which a test must never do.
#
# Two facts about starting one are handled here and nowhere else:
#
#   * A named session is started as a headless server, then given one workspace.
#     Attaching a TUI client instead changes what the verifications see: with a
#     client attached, an explicit resize no longer takes effect (measured
#     2026-10-17, herdr 0.9.1 and 0.9.5).
#   * herdr refuses to start a second instance from inside one, and an inherited
#     HERDR_* environment points at the caller's own socket. Both have to be
#     cleared, or "the scratch session" would silently be the user's session.
#
# Usage:
#   source "$(dirname "$0")/lib/scratch-session.sh"
#   scratch_session_start "perch-keys-$$" || { echo "$SCRATCH_ERROR"; exit 2; }
#   trap scratch_session_cleanup EXIT
#   echo "$SCRATCH_PANE"

scratch_session_start() {
    SCRATCH_SESSION="$1"
    SCRATCH_ATTACH_PID=""
    SCRATCH_ERROR=""

    _scratch_start_failed() {
        timeout 5 herdr session stop "$SCRATCH_SESSION" >/dev/null 2>&1 || true
        timeout 5 herdr session delete "$SCRATCH_SESSION" >/dev/null 2>&1 || true
        if [ -n "$SCRATCH_ATTACH_PID" ]; then
            kill "$SCRATCH_ATTACH_PID" 2>/dev/null || true
            sleep 0.2
            kill -0 "$SCRATCH_ATTACH_PID" 2>/dev/null && kill -9 "$SCRATCH_ATTACH_PID" 2>/dev/null || true
        fi
        SCRATCH_SESSION=""
    }

    if ! command -v herdr >/dev/null 2>&1; then
        SCRATCH_ERROR="herdr is not on PATH"
        return 1
    fi

    env -u HERDR_ENV -u HERDR_PANE_ID -u HERDR_TAB_ID -u HERDR_WORKSPACE_ID \
        -u HERDR_SOCKET_PATH -u HERDR_BIN_PATH \
        herdr --session "$SCRATCH_SESSION" server \
        >/dev/null 2>&1 &
    SCRATCH_ATTACH_PID=$!

    SCRATCH_READY=""
    for _ in $(seq 1 80); do
        if herdr --session "$SCRATCH_SESSION" pane list >/dev/null 2>&1; then
            SCRATCH_READY=yes
            break
        fi
        sleep 0.25
    done
    if [ -z "$SCRATCH_READY" ]; then
        _scratch_start_failed
        SCRATCH_ERROR="the scratch session never answered on its own socket"
        return 3
    fi

    _scratch_first_pane() {
        herdr --session "$SCRATCH_SESSION" pane list | python3 -c '
import json, sys
panes = json.load(sys.stdin)["result"]["panes"]
print(panes[0]["pane_id"] if panes else "")
'
    }
    SCRATCH_PANE="$(_scratch_first_pane)"
    if [ -z "$SCRATCH_PANE" ]; then
        herdr --session "$SCRATCH_SESSION" workspace create --cwd "$PWD" >/dev/null 2>&1
        for _ in $(seq 1 40); do
            SCRATCH_PANE="$(_scratch_first_pane)"
            [ -n "$SCRATCH_PANE" ] && break
            sleep 0.1
        done
    fi
    if [ -z "$SCRATCH_PANE" ]; then
        _scratch_start_failed
        SCRATCH_ERROR="the scratch session has no pane"
        return 4
    fi

    SCRATCH_SOCKET="${HERDR_SCRATCH_SOCKET:-$HOME/.config/herdr/sessions/$SCRATCH_SESSION/herdr.sock}"
    return 0
}

# Stop the server, remove the session, then let the attaching client fall over. In
# that order: a leftover named session is invisible to the user but still on disk.
scratch_session_cleanup() {
    [ -n "${SCRATCH_SESSION:-}" ] || return 0
    timeout 10 herdr session stop "$SCRATCH_SESSION" >/dev/null 2>&1 || true
    timeout 10 herdr session delete "$SCRATCH_SESSION" >/dev/null 2>&1 || true
    if [ -n "${SCRATCH_ATTACH_PID:-}" ]; then
        kill "$SCRATCH_ATTACH_PID" 2>/dev/null || true
        for _ in $(seq 1 20); do
            kill -0 "$SCRATCH_ATTACH_PID" 2>/dev/null || break
            sleep 0.1
        done
        kill -9 "$SCRATCH_ATTACH_PID" 2>/dev/null || true
    fi
}
