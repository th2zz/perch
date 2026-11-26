#!/usr/bin/env bash
#
# Checks what can be checked about the three backends without owning all three.
#
# The spec promises the same product on WSL, macOS and cloud Linux, and for a long
# time two of those were a claim on paper. Most of a portability claim is genuinely
# not checkable from one machine — "does it come back after a reboot on a Mac" needs
# a Mac. But two large parts of it are:
#
#   * **It compiles for the target.** A missing build tag, a syscall that only
#     exists on Linux, a `_linux.go` file referenced from shared code: all of those
#     are compile errors, and they are the failure mode that turns "we support
#     macOS" into a support ticket. Cross-compiling catches them here, today.
#   * **The startup file is well-formed.** The launchd plist is parsed as a real
#     property list, and the systemd unit is handed to systemd's own verifier.
#
# What it cannot check, and says so: that the machine actually boots and comes back.
# That is `scripts/verify-us1.sh` on the real thing.
#
# Usage: scripts/check-portability.sh

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d -t perch-portability-XXXXXX)"
trap 'rm -r "$WORK" 2>/dev/null' EXIT

PASS=0
FAIL=0
pass() { PASS=$((PASS + 1)); printf '  \033[32mok\033[0m   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
skip() { printf '  \033[33mskip\033[0m %s\n' "$1"; }
section() { printf '\n%s\n' "$1"; }

section "Perch · the three backends, as far as one machine can see"

# ------------------------------------------------------------------ compiles?

section "It builds for every backend we promise"
for target in "linux amd64" "linux arm64" "darwin amd64" "darwin arm64"; do
    set -- $target
    out="$WORK/perch-$1-$2"
    # Built from inside the bridge module: it is a Go module of its own, and `go
    # build` from the repository root cannot see it.
    if (cd "$REPO_ROOT/bridge" && GOOS=$1 GOARCH=$2 go build -o "$out" ./cmd/perch-bridge) 2>"$out.err"; then
        pass "builds for $1/$2 ($(du -h "$out" | cut -f1))"
    else
        fail "does not build for $1/$2"
        head -5 "$out.err" | sed 's/^/      /'
    fi
done

# ------------------------------------------------------------- startup files?

section "The two halves of the product agree on the wire contract"

# The bridge and the app are separate repositories of truth for one number that must
# match: the bridge declares it in Go, the app in Kotlin, and nothing else would notice
# if they drifted apart — the symptom would be a version check that never fires, which
# is worse than no check at all because it looks like it works.
bridge_api="$(sed -n 's/^const APIVersion = \([0-9]*\)$/\1/p' "$REPO_ROOT/bridge/internal/server/version.go")"
app_api="$(sed -n 's/.*const val API_VERSION = \([0-9]*\).*/\1/p' "$REPO_ROOT/app/app/src/main/java/dev/perch/console/core/herdrclient/ApiCompatibility.kt")"
if [ -z "$bridge_api" ] || [ -z "$app_api" ]; then
    fail "could not read both API versions (bridge='$bridge_api' app='$app_api'); the check itself is broken"
elif [ "$bridge_api" = "$app_api" ]; then
    pass "both halves declare wire contract $bridge_api"
else
    fail "the bridge speaks contract $bridge_api and the app speaks $app_api"
fi

section "The startup files are well-formed"

if command -v systemd-analyze >/dev/null 2>&1; then
    "$WORK/perch-linux-amd64" render-unit --out "$WORK/perch-bridge.service" >/dev/null
    if systemd-analyze verify "$WORK/perch-bridge.service" 2>"$WORK/unit.err"; then
        pass "systemd accepts the unit it would install"
    else
        fail "systemd rejects the unit"
        sed 's/^/      /' "$WORK/unit.err"
    fi
else
    skip "systemd-analyze (this machine has no systemd)"
fi

# The plist is rendered *by the code that would install it* — the same renderer the
# macOS build calls, asked for the macOS template explicitly — and then parsed with a
# real property-list reader. That is the difference between "the template looks
# right" and "the file is a valid launchd agent". Running the macOS binary here is
# not an option: Linux refuses to exec it.
"$WORK/perch-linux-amd64" render-unit --platform darwin --out "$WORK/dev.perch.bridge.plist" >/dev/null
python3 - "$WORK/dev.perch.bridge.plist" <<'PY'
import plistlib, sys

path = sys.argv[1]
try:
    with open(path, "rb") as handle:
        plist = plistlib.load(handle)
except Exception as error:  # noqa: BLE001 - any parse failure is the failure
    print(f"  \033[31mFAIL\033[0m the launchd plist does not parse: {error}")
    raise SystemExit(1)

problems = []
if plist.get("Label") != "dev.perch.bridge":
    problems.append(f"Label is {plist.get('Label')!r}")
args = plist.get("ProgramArguments") or []
if len(args) < 2 or not args[1] == "run":
    problems.append(f"ProgramArguments = {args!r}, want the bridge's `run`")
# A LaunchAgent that is not kept alive is a service that stays dead after a crash,
# which is the whole thing this file exists to prevent.
if plist.get("KeepAlive") not in (True,):
    problems.append("KeepAlive is not true")
if plist.get("RunAtLoad") not in (True,):
    problems.append("RunAtLoad is not true")
if not plist.get("StandardOutPath") or plist.get("StandardOutPath") != plist.get("StandardErrorPath"):
    problems.append("logs do not go to one file")

if problems:
    print("  \033[31mFAIL\033[0m " + "; ".join(problems))
    raise SystemExit(1)
print("  \033[32mok\033[0m   the launchd plist parses and says what it should")
PY
case $? in
    0) PASS=$((PASS + 1)) ;;
    *) FAIL=$((FAIL + 1)) ;;
esac

section "What this did not check"
cat <<'EOF'
  Booting. "It comes back after a reboot" needs the real machine: a Mac for the
  launchd agent, a VM for the systemd unit. On this machine that is
  scripts/verify-us1.sh; on a cloud VM it is the same script plus a reboot.
EOF

section "Summary"
printf '  %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
