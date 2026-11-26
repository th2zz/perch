#!/usr/bin/env bash
#
# Checks the systemd unit the installer writes, with systemd's own verifier.
#
# This is the portable half of "does it survive a reboot on a cloud Linux VM?": the
# unit is the same file on WSL and on a server, and `systemd-analyze verify` is the
# authority on whether systemd will accept it. What this cannot check is the other
# half — that the machine actually boots and comes back — which needs a real reboot
# on a real VM.
#
# Usage: scripts/check-unit.sh

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if ! command -v systemd-analyze >/dev/null 2>&1; then
    echo "systemd-analyze is not here; nothing to check (this is not a systemd machine)"
    exit 0
fi

WORK="$(mktemp -d -t perch-unit-XXXXXX)"
trap 'rm -r "$WORK" 2>/dev/null' EXIT

echo "building the bridge..."
(cd "$REPO_ROOT/bridge" && go build -o "$WORK/perch-bridge" ./cmd/perch-bridge) || exit 1

# The unit is rendered the way the installer renders it, so the thing being
# verified is the artefact, not a copy of it.
unit="$WORK/perch-bridge.service"
"$WORK/perch-bridge" render-unit --out "$unit" 2>/dev/null || {
    echo "the bridge has no render-unit command; falling back to running the installer" >&2
    exit 2
}

output="$(systemd-analyze verify "$unit" 2>&1)"
status=$?
if [ $status -ne 0 ]; then
    echo "systemd rejects the unit:" >&2
    printf '%s\n' "$output" | sed 's/^/  /' >&2
    exit 1
fi

# Warnings are worth reading but not worth failing on: they are how systemd says
# "this will work, but you should know something".
if [ -n "$output" ]; then
    printf 'systemd warns about it (not fatal):\n'
    printf '%s\n' "$output" | sed 's/^/  /'
else
    printf 'systemd accepts the unit as written: %s\n' "$unit"
fi
