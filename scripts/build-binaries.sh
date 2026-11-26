#!/usr/bin/env bash
#
# Builds the bridge for the machines a user might put it on, so the target machine
# needs nothing installed.
#
# The alternative is telling someone to install a Go toolchain on a cloud VM in
# order to compile fifteen megabytes of Go — a toolchain they would then have to keep
# updating. Copying one file over SSH is the smaller ask, and it is the same file we
# verified here.
#
# Outputs into dist/ (git-ignored): one binary per platform plus a SHA256SUMS file,
# because a binary that crossed a network should be checkable at the other end.
#
# Usage: scripts/build-binaries.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$REPO_ROOT/dist"
VERSION="$(cd "$REPO_ROOT" && git describe --tags --always --dirty 2>/dev/null || echo dev)"

mkdir -p "$DIST"

# Linux covers WSL and every cloud VM; darwin covers both Mac architectures. arm64 is
# not decoration: Ampere and Graviton instances are arm64, and so is every recent
# Mac, so a build that only produces amd64 is a build half the users cannot run.
for target in "linux amd64" "linux arm64" "darwin amd64" "darwin arm64"; do
    set -- $target
    out="$DIST/perch-bridge-$1-$2"
    echo "building $1/$2..."
    (
        cd "$REPO_ROOT/bridge"
        GOOS=$1 GOARCH=$2 CGO_ENABLED=0 go build -trimpath -o "$out" ./cmd/perch-bridge
    )
done

(
    cd "$DIST"
    sha256sum perch-bridge-* > SHA256SUMS
)

echo
echo "built version $VERSION into dist/:"
ls -la "$DIST" | sed 's/^/  /'
echo
echo "On the target machine, one binary and one command:"
echo
echo "    scp dist/perch-bridge-linux-arm64 you@vm:~/.local/bin/perch-bridge"
echo "    ssh you@vm 'chmod +x ~/.local/bin/perch-bridge && ~/.local/bin/perch-bridge install-service'"
echo
echo "install-service sets up start-up for that platform by itself (see README)."
echo "dist/SHA256SUMS is there to check the file arrived intact: sha256sum -c SHA256SUMS"
