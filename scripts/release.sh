#!/usr/bin/env bash
#
# Cut a Perch release: verify, build signed APK and bridge binaries, tag, and publish to GitHub Releases.
#
# Usage:
#   scripts/release.sh <version> <notes.md> [--repo owner/name] [--skip-checks] [--draft] [--dry-run]

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT" || exit 1

if ! command -v go >/dev/null 2>&1 && [ -d "$HOME/.local/opt/go/bin" ]; then
    export PATH="$HOME/.local/opt/go/bin:$PATH"
fi
if [ -z "${JAVA_HOME:-}" ] && [ -d "$HOME/.local/opt/jdk-17.0.20.1+1" ]; then
    export JAVA_HOME="$HOME/.local/opt/jdk-17.0.20.1+1"
fi
if [ -z "${ANDROID_HOME:-}" ] && [ -d "$HOME/.local/opt/android-sdk" ]; then
    export ANDROID_HOME="$HOME/.local/opt/android-sdk"
fi

REPO_SLUG="${PERCH_GITHUB_REPO:-th2zz/perch}"
VERSION=""
NOTES_FILE=""
SKIP_CHECKS=0
DRY_RUN=0
DRAFT=""

while [ $# -gt 0 ]; do
    case "$1" in
        --repo) REPO_SLUG="$2"; shift ;;
        --skip-checks) SKIP_CHECKS=1 ;;
        --dry-run) DRY_RUN=1 ;;
        --draft) DRAFT="--draft" ;;
        -h|--help) sed -n '2,8p' "$0"; exit 0 ;;
        -*) printf 'unknown flag: %s\n' "$1" >&2; exit 2 ;;
        *)
            if [ -z "$VERSION" ]; then VERSION="$1"
            elif [ -z "$NOTES_FILE" ]; then NOTES_FILE="$1"
            else printf 'unexpected argument: %s\n' "$1" >&2; exit 2
            fi ;;
    esac
    shift
done

die() { printf 'release: %s\n' "$1" >&2; exit 1; }

[ -n "$VERSION" ] && [ -n "$NOTES_FILE" ] || die "usage: scripts/release.sh <version> <notes.md> [--repo owner/name]"
printf '%s' "$VERSION" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$' || die "not semver: $VERSION"
[ -s "$NOTES_FILE" ] || die "notes file missing or empty: $NOTES_FILE"

GIT_VERSION="$(sed -n 's/.*versionName *= *"\([^"]*\)".*/\1/p' app/app/build.gradle.kts | head -1)"
[ "$GIT_VERSION" = "$VERSION" ] || die "app/app/build.gradle.kts versionName is $GIT_VERSION, expected $VERSION"

BRANCH="$(git rev-parse --abbrev-ref HEAD)"
[ "$BRANCH" = "main" ] || die "on branch $BRANCH, expected main"
[ -z "$(git status --porcelain)" ] || die "working tree is dirty - commit first"

if git rev-parse -q --verify "refs/tags/v$VERSION" >/dev/null; then
    die "tag v$VERSION already exists"
fi

if [ "$SKIP_CHECKS" -eq 0 ]; then
    bash scripts/check-all.sh --quick || die "verification suite failed"
fi

printf '\n== building release APK and bridge binaries\n'
(cd app && ./gradlew :app:assembleRelease --quiet) || die "gradle assembleRelease failed"
APK="$REPO_ROOT/app/app/build/outputs/apk/release/app-release.apk"
[ -f "$APK" ] || die "no signed APK at $APK"

bash scripts/build-binaries.sh || die "bridge cross-compilation failed"
cp "$APK" "$REPO_ROOT/dist/perch-$VERSION.apk"
(cd "$REPO_ROOT/dist" && sha256sum "perch-$VERSION.apk" perch-bridge-* > SHA256SUMS)

if [ "$DRY_RUN" -eq 1 ]; then
    printf '\nDRY RUN - artifacts built in dist/:\n'
    ls -la "$REPO_ROOT/dist"
    exit 0
fi

[ -n "${GITHUB_PERSONAL_ACCESS_TOKEN:-}" ] || die "set GITHUB_PERSONAL_ACCESS_TOKEN in the environment"

git tag -a "v$VERSION" -m "v$VERSION"
git push origin main || die "git push main failed"
git push origin "v$VERSION" || die "git push tag v$VERSION failed"

python3 scripts/gh-release.py "$VERSION" "$NOTES_FILE" \
    "$REPO_ROOT/dist/perch-$VERSION.apk" \
    "$REPO_ROOT/dist/perch-bridge-linux-amd64" \
    "$REPO_ROOT/dist/perch-bridge-linux-arm64" \
    "$REPO_ROOT/dist/perch-bridge-darwin-amd64" \
    "$REPO_ROOT/dist/perch-bridge-darwin-arm64" \
    "$REPO_ROOT/dist/SHA256SUMS" \
    --repo "$REPO_SLUG" $DRAFT
