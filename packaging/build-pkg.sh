#!/bin/bash
# Build a macOS installer package (.pkg) for Tart Oven.
#
#   ./packaging/build-pkg.sh
#
# The script will prompt whether to sign/notarize. If yes, it will ask for:
#   - Your full name (as shown in your Developer ID certificates)
#   - Your Apple Team ID
#   - Your Apple ID email
#   - An app-specific password for notarization
#
# The resulting TartOven-<version>.pkg installs:
#   /Library/Application Support/Tart Oven/tart-oven   (the binary)
#   /Library/LaunchAgents/com.tartoven.agent.plist     (auto-start agent)
# and its postinstall loads the agent and opens http://127.0.0.1:9000.
set -euo pipefail

cd "$(dirname "$0")/.."   # repo root
REPO="$(pwd)"

PKG_ID="com.tartoven.pkg"
LABEL="com.tartoven.agent"
APPDIR="Library/Application Support/Tart Oven"

VERSION=$(sed -n 's/^const version = "\(.*\)"/\1/p' main.go)
[ -n "$VERSION" ] || { echo "could not read version from main.go"; exit 1; }
OUT="TartOven-${VERSION}.pkg"

# Ask about signing
echo ""
read -p "Do you want to sign and notarize the PKG? [y/N] " SIGN_ANSWER
SIGN_ANSWER=${SIGN_ANSWER:-n}

if [[ "$SIGN_ANSWER" =~ ^[Yy]$ ]]; then
    echo ""
    echo "Enter your Apple Developer signing details:"
    read -p "  Full name (as in certificate): " DEV_NAME
    read -p "  Team ID: " TEAM_ID
    read -p "  Apple ID email: " APPLE_EMAIL
    read -s -p "  App-specific password: " APP_PASSWORD
    echo ""

    APP_SIGN_IDENTITY="Developer ID Application: $DEV_NAME ($TEAM_ID)"
    PKG_SIGN_IDENTITY="Developer ID Installer: $DEV_NAME ($TEAM_ID)"
    DO_SIGN=true
else
    DO_SIGN=false
fi

echo ""
echo "==> Building tart-oven ${VERSION} (arm64)…"
BUILD="$(mktemp -d)"
GOOS=darwin GOARCH=arm64 go build -o "$BUILD/tart-oven" .

# Sign the binary with hardened runtime if signing is enabled
if [ "$DO_SIGN" = true ]; then
    echo "==> Signing binary with: $APP_SIGN_IDENTITY"
    codesign --force --options runtime --timestamp --sign "$APP_SIGN_IDENTITY" "$BUILD/tart-oven"
    echo "    Verifying signature…"
    codesign --verify --verbose "$BUILD/tart-oven"
fi

echo "==> Assembling payload…"
ROOT="$(mktemp -d)/root"
install -d "$ROOT/$APPDIR"
install -m 755 "$BUILD/tart-oven" "$ROOT/$APPDIR/tart-oven"
install -d "$ROOT/Library/LaunchAgents"
install -m 644 packaging/com.tartoven.agent.plist "$ROOT/Library/LaunchAgents/$LABEL.plist"

# Strip extended attributes so the payload doesn't carry ._AppleDouble clutter
# (the repo lives on synced storage that adds xattrs).
xattr -rc "$ROOT" 2>/dev/null || true

chmod +x packaging/scripts/postinstall

echo "==> Running pkgbuild…"
PKG_SIGN_ARGS=()
if [ "$DO_SIGN" = true ]; then
    PKG_SIGN_ARGS=(--sign "$PKG_SIGN_IDENTITY")
    echo "    signing PKG with: $PKG_SIGN_IDENTITY"
fi

pkgbuild \
    --root "$ROOT" \
    --identifier "$PKG_ID" \
    --version "$VERSION" \
    --scripts "$REPO/packaging/scripts" \
    --install-location "/" \
    ${PKG_SIGN_ARGS[@]+"${PKG_SIGN_ARGS[@]}"} \
    "$OUT"

echo "==> PKG built: $REPO/$OUT"

# Notarize and staple if signing is enabled
if [ "$DO_SIGN" = true ]; then
    echo ""
    echo "==> Submitting for notarization…"
    xcrun notarytool submit "$OUT" \
        --apple-id "$APPLE_EMAIL" \
        --team-id "$TEAM_ID" \
        --password "$APP_PASSWORD" \
        --wait

    echo ""
    echo "==> Stapling notarization ticket…"
    xcrun stapler staple "$OUT"
fi

echo ""
echo "==> Done: $REPO/$OUT"
echo "    Install: sudo installer -pkg \"$OUT\" -target /"
