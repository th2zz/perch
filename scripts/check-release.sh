#!/usr/bin/env bash
#
# Checks the release build, which is the one nobody looks at until they ship it.
#
# Minification is where a working app quietly stops working. Our client decodes
# every response with generated serializers; those serializers are referenced
# statically, so R8 is free to rename them — and if it ever decides they are
# unused instead, every request fails and the app has no way to say why. Nothing
# in the debug build, the unit tests, or the lint run would notice.
#
# So this builds the release app and asserts the serializers are still in it.
# It reads the R8 mapping file rather than the APK, because the point is to tell
# "renamed" (fine) from "removed" (fatal), and obfuscated names cannot do that.
#
# It also checks the artefact is *signed*. An unsigned release build passes every
# test and installs on nothing, and "unsigned" is exactly what a release looks like
# on a machine where the signing key was never created — a state that is invisible
# until someone tries to use the thing.
#
# Usage: scripts/check-release.sh

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MAPPING="$REPO_ROOT/app/app/build/outputs/mapping/release/mapping.txt"

# A floor, not an equality: one per serializable class the client decodes with,
# and a new model here means bumping this number deliberately rather than
# discovering it in the release build. (24 as of 2026-10-13, when the escape
# hatch's key bar added KeysRequest/KeysResponse.)
EXPECTED_SERIALIZERS=24

echo "building the release app (this takes a minute)..."
(cd "$REPO_ROOT/app" && ./gradlew :app:assembleRelease --quiet) || {
    echo "the release build failed" >&2
    exit 1
}

if [ ! -f "$MAPPING" ]; then
    echo "no mapping file at $MAPPING; is minification still enabled?" >&2
    exit 1
fi

found="$(grep -c 'core\.herdrclient\..*\$\$serializer ->' "$MAPPING" || true)"
echo "serializers kept: $found (expected $EXPECTED_SERIALIZERS)"

if [ "$found" -lt "$EXPECTED_SERIALIZERS" ]; then
    cat >&2 <<EOF

R8 removed at least one of the app's serializers.

Every response the app decodes would fail, and it would fail silently: there is no
exception the user can act on, just a screen that never fills in. Check the
keep rules in app/app/proguard-rules.pro, and the consumer rules shipped with
kotlinx-serialization.
EOF
    exit 1
fi

echo "release build keeps what it needs."
ls -la "$REPO_ROOT/app/app/build/outputs/apk/release/"*.apk 2>/dev/null | sed 's/^/  /'

# ------------------------------------------------------------------ signed?

APK="$REPO_ROOT/app/app/build/outputs/apk/release/app-release.apk"
if [ ! -f "$APK" ]; then
    # Only the unsigned artefact exists: the key has not been made yet, which is
    # fine for development and fatal for delivery.
    cat >&2 <<EOF

The release APK is unsigned (no app-release.apk), so nobody can install it.

Android refuses an unsigned package, and the signing key is the app's identity:
it cannot be recreated later without every installed copy being uninstalled first.
Create it once with:

  scripts/make-release-key.sh

then link it into the app and build again:

  cd app && ln -sf ~/.perch/keystore.properties keystore.properties && ./gradlew :app:assembleRelease
EOF
    exit 1
fi

APKSIGNER=""
for candidate in "$ANDROID_HOME"/build-tools/*/apksigner; do
    [ -x "$candidate" ] && APKSIGNER="$candidate"
done
if [ -z "$APKSIGNER" ]; then
    echo "apksigner not found under ANDROID_HOME; skipping the signature check" >&2
    exit 0
fi

if verify_output="$("$APKSIGNER" verify --print-certs "$APK" 2>&1)"; then
    # Two spellings across build-tools versions ("Signer #1 certificate DN:" and
    # "V2 Signer: certificate DN:"), and matching only one of them turns a working
    # release into a confusing empty line.
    signer="$(printf '%s\n' "$verify_output" | sed -n -E 's/^.*certificate DN: //p' | head -1)"
    echo "signed by: ${signer:-the app key}"
else
    echo "the release APK is not signed:" >&2
    printf '%s\n' "$verify_output" | sed 's/^/  /' >&2
    exit 1
fi
