#!/usr/bin/env bash
#
# Verification for the escape hatch's key bar: does every key in the bar's
# vocabulary actually arrive at a real terminal, as the byte a keyboard would send?
#
# What this script proves, and what it does not:
#
#   PROVES  the twelve key names the app offers are names herdr accepts, and that
#           each one lands as the expected sequence - measured, not read off a
#           table. This is the half of the contract that no unit test can hold:
#           the bridge can only refuse what it does not offer, and herdr is the
#           only thing that decides what a name means.
#   CANNOT  prove the app's button wiring or the layout — and since D34 (2026-10-14) there is no app
#           side to this endpoint at all: the phone's key row sends bytes into the real terminal
#           (`TerminalKeys` → `/v1/session/terminal`), covered by `TerminalKeysTest` and
#           `scripts/verify-terminal.sh`. What is left here is the *bridge's* vocabulary and its bounds,
#           which is still a constitutional claim (IV) and still worth a real herdr. Whether the
#           endpoint should exist at all is an open question — see #64.
#
# It is safe to run on a machine with a live herdr session, and that is the whole
# design of it: everything happens inside a *separate named session* (`herdr
# session`, which is an independent server with its own socket, panes and state).
# Your own panes are never touched, and the scratch session is deleted on exit.
#
# Usage: scripts/verify-keys.sh

set -uo pipefail

LAB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/scratch-session.sh
. "$LAB_DIR/lib/scratch-session.sh"

SESSION="perch-keys-$$"
PASS=0
FAIL=0
SKIP=0

pass() { PASS=$((PASS + 1)); printf '  \033[32mok\033[0m   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
skip() { SKIP=$((SKIP + 1)); printf '  \033[33mskip\033[0m %s\n' "$1"; }
section() { printf '\n%s\n' "$1"; }

trap scratch_session_cleanup EXIT

section "Perch · the key bar's vocabulary, measured against a real herdr"

scratch_session_start "$SESSION"
case $? in
    0) pass "a scratch session started, separate from yours (herdr session $SESSION)" ;;
    2) skip "attach a scratch session: $SCRATCH_ERROR"; exit 0 ;;
    *) fail "$SCRATCH_ERROR"; exit 1 ;;
esac
PANE="$SCRATCH_PANE"

# Raw mode, so each key arrives immediately and is echoed as data rather than
# interpreted by the line discipline: an uninterpreted terminal is the only way to
# tell "the byte arrived" from "the shell did something with it".
herdr --session "$SESSION" pane run "$PANE" \
    "stty raw -echo -icanon min 1 time 0; cat -A" >/dev/null 2>&1
sleep 1

read_pane() { herdr --session "$SESSION" pane read "$PANE" --source visible --format text 2>/dev/null | tail -1; }

previous="$(read_pane)"
section "Each key, and the bytes it produced"

# key|expected echo|witness. `cat -A` renders control bytes in caret notation, so
# the expectation is what a terminal shows for that byte - not a herdr-specific
# form. The third field exists for one key only: herdr's text read trims trailing
# whitespace, so a space on its own is not visible in the output until something
# follows it. Sending a witness right after makes the space measurable instead of
# guessed at.
while IFS='|' read -r key expected witness; do
    [ -z "$key" ] && continue
    herdr --session "$SESSION" pane send-keys "$PANE" "$key" >/dev/null 2>&1
    if [ -n "$witness" ]; then
        herdr --session "$SESSION" pane send-keys "$PANE" "$witness" >/dev/null 2>&1
        expected="$expected$witness"
    fi
    sleep 0.35
    current="$(read_pane)"
    delta="${current#"$previous"}"
    previous="$current"
    if [ "$delta" = "$expected" ]; then
        pass "$(printf '%-10s -> %q' "$key" "$delta")"
    else
        fail "$(printf '%-10s -> %q (expected %q)' "$key" "$delta" "$expected")"
    fi
done <<'KEYS'
esc|^[|
enter|^M|
y|y|
n|n|
up|^[[A|
down|^[[B|
left|^[[D|
right|^[[C|
tab|^I|
shift+tab|^[[Z|
space| |y
ctrl+c|^C|
KEYS

section "An unknown key name is refused, and nothing is written"
before_unknown="$(read_pane)"
if herdr --session "$SESSION" pane send-keys "$PANE" "not-a-key" >/dev/null 2>&1; then
    fail "herdr accepted a key name outside its own syntax"
else
    sleep 0.35
    if [ "$(read_pane)" = "$before_unknown" ]; then
        pass "an unsupported name is refused and no bytes reach the terminal"
    else
        fail "the refused key still wrote something to the terminal"
    fi
fi

section "Summary"
printf '  %d passed, %d failed, %d skipped\n' "$PASS" "$FAIL" "$SKIP"
printf '  (the scratch session was stopped and deleted; your own panes were not touched)\n'
[ "$FAIL" -eq 0 ]
