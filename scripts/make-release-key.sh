#!/usr/bin/env bash
#
# Creates the Android release signing key once outside the repository.
#
# Usage: scripts/make-release-key.sh

set -euo pipefail

KEY_DIR="${PERCH_KEY_DIR:-$HOME/.perch}"
KEYSTORE="$KEY_DIR/keystore.jks"
PROPERTIES="$KEY_DIR/keystore.properties"

if [ -f "$KEYSTORE" ]; then
    echo "a signing key already exists at $KEYSTORE"
    echo "keeping it: replacing it would prevent upgrading existing app installs."
    exit 0
fi

KEYTOOL="$(command -v keytool || true)"
if [ -z "$KEYTOOL" ] && [ -n "${JAVA_HOME:-}" ] && [ -x "$JAVA_HOME/bin/keytool" ]; then
    KEYTOOL="$JAVA_HOME/bin/keytool"
fi
if [ -z "$KEYTOOL" ] && [ -x "$HOME/.local/opt/jdk-17.0.20.1+1/bin/keytool" ]; then
    KEYTOOL="$HOME/.local/opt/jdk-17.0.20.1+1/bin/keytool"
fi
if [ -z "$KEYTOOL" ] || [ ! -x "$KEYTOOL" ]; then
    echo "keytool not found; install JDK 17+ or set JAVA_HOME." >&2
    exit 1
fi

mkdir -p "$KEY_DIR"
chmod 700 "$KEY_DIR"

PASS="$(head -c 32 /dev/urandom | base64 | tr -d '/+=' | head -c 28)"

"$KEYTOOL" -genkeypair -keystore "$KEYSTORE" -storetype PKCS12 -alias perch \
    -keyalg RSA -keysize 4096 -validity 10000 \
    -storepass "$PASS" -keypass "$PASS" \
    -dname "CN=Perch, OU=Perch, O=Perch" >/dev/null
chmod 600 "$KEYSTORE"

cat > "$PROPERTIES" <<EOF
storeFile=$KEYSTORE
storePassword=$PASS
keyAlias=perch
keyPassword=$PASS
EOF
chmod 600 "$PROPERTIES"

echo "created $KEYSTORE"
echo "created $PROPERTIES (mode 600)"
echo
echo "Back up $KEY_DIR securely. Next:"
echo "  cd app && ln -sf $PROPERTIES keystore.properties && ./gradlew :app:assembleRelease"
