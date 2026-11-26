#!/usr/bin/env bash
#
# Local verification suite for Perch.
#
# Usage: scripts/check-all.sh [--quick]

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if ! command -v go >/dev/null 2>&1 && [ -d "$HOME/.local/opt/go/bin" ]; then
    export PATH="$HOME/.local/opt/go/bin:$PATH"
fi
if [ -z "${JAVA_HOME:-}" ] && [ -d "$HOME/.local/opt/jdk-17.0.20.1+1" ]; then
    export JAVA_HOME="$HOME/.local/opt/jdk-17.0.20.1+1"
fi
if [ -z "${ANDROID_HOME:-}" ] && [ -d "$HOME/.local/opt/android-sdk" ]; then
    export ANDROID_HOME="$HOME/.local/opt/android-sdk"
fi

QUICK=0
[ "${1:-}" = "--quick" ] && QUICK=1

declare -a NAMES=() RESULTS=()
PASS=0
FAIL=0
SKIP=0

run() {
    local name="$1"
    shift
    local started took status
    started=$(date +%s)
    printf '\n== %s\n' "$name"
    CHECK_LOG="/tmp/perch-check-$((PASS + SKIP + FAIL + 1))-$(printf '%s' "$name" | tr ' /' '__').log"
    "$@" >"$CHECK_LOG" 2>&1
    status=$?
    took=$(( $(date +%s) - started ))
    case $status in
        0)
            NAMES+=("$name"); RESULTS+=("ok (${took}s)")
            PASS=$((PASS + 1))
            printf '   ok (%ss)\n' "$took" ;;
        2)
            NAMES+=("$name"); RESULTS+=("skipped (${took}s)")
            SKIP=$((SKIP + 1))
            printf '   skipped: %s\n' "$(grep -m1 -E '^[A-Z]' "$CHECK_LOG" | head -c 120)"
            ;;
        *)
            NAMES+=("$name"); RESULTS+=("FAILED (${took}s)")
            FAIL=$((FAIL + 1))
            printf '   FAILED (%ss) - tail of %s:\n' "$took" "$CHECK_LOG"
            tail -25 "$CHECK_LOG" | sed 's/^/     /'
            ;;
    esac
}

go_tests() { (cd "$REPO_ROOT/bridge" && go test ./... -count=1); }
relay_tests() { (cd "$REPO_ROOT/relay" && go test ./... -count=1); }
android_tests() { (cd "$REPO_ROOT/app" && ./gradlew :app:testDebugUnitTest --quiet); }
android_lint() { (cd "$REPO_ROOT/app" && ./gradlew :app:lintRelease --quiet); }
terminal_page() { (cd "$REPO_ROOT" && bash scripts/check-terminal-page.sh); }
unit_checks() { (cd "$REPO_ROOT" && bash scripts/check-unit.sh); }
portability() { (cd "$REPO_ROOT" && bash scripts/check-portability.sh); }
release_gate() { (cd "$REPO_ROOT" && bash scripts/check-release.sh); }

printf 'Perch verification suite\n'

run "bridge tests (Go)" go_tests
run "relay tests (Go, push whitelist)" relay_tests
run "app tests (Android)" android_tests
run "app lint (Android)" android_lint
run "terminal page tests (Node)" terminal_page
run "systemd unit check" unit_checks
[ "$QUICK" -eq 1 ] || run "portability (4 targets, startup files)" portability
[ "$QUICK" -eq 1 ] || run "release gate (R8 + signature)" release_gate

printf '\n== summary\n'
for i in "${!NAMES[@]}"; do
    printf '   %-48s %s\n' "${NAMES[$i]}" "${RESULTS[$i]}"
done
printf '\n   %d passed, %d skipped, %d failed\n' "$PASS" "$SKIP" "$FAIL"
[ "$FAIL" -eq 0 ]
