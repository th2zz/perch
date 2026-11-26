#!/usr/bin/env bash
#
# Node tests for the Android WebView terminal page (fit arithmetic and app↔page contract).
#
# Usage: scripts/check-terminal-page.sh

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if ! command -v node >/dev/null 2>&1; then
    echo "node is not installed; skipping terminal page tests"
    exit 0
fi

cd "$SCRIPT_DIR/terminal-page" || exit 1
node --test ./*.test.js || exit 1
if command -v agent-browser >/dev/null 2>&1; then
    bash "$SCRIPT_DIR/verify-terminal-viewport.sh"
else
    echo "terminal viewport check skipped: agent-browser is unavailable"
fi
