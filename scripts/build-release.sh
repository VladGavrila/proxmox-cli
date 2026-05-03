#!/usr/bin/env bash
# Build, sign, and notarize pxve CLI binaries.
#
# Required env vars for signing/notarization:
#   DEVELOPER_ID_CERT_BASE64   — $(base64 -i /path/to/your-cert.p12)
#   DEVELOPER_ID_CERT_PASSWORD — .p12 password
#   NOTARIZE_APPLE_ID          — Apple ID for notarytool
#   NOTARIZE_TEAM_ID           — Team ID
#   NOTARIZE_APP_PASSWORD      — App-specific password
#
# Optional:
#   VERSION                    — override version string injected via ldflags

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
DIST_DIR="$REPO_ROOT/dist"
BINARY="pxve"

# Derive version from root.go if not overridden
VERSION="${VERSION:-$(grep 'var version' "$REPO_ROOT/cli/root.go" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')}"

echo "==> Building binaries (version $VERSION)..."
cd "$REPO_ROOT"
rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -ldflags "-s -w -X github.com/VladGavrila/matrixreq-cli/cli.Version=$VERSION" -o "$DIST_DIR/${BINARY}-macos-arm64" .

echo "==> Signing macOS binary..."
MACOS_BIN="$DIST_DIR/${BINARY}-macos-arm64"

if [[ -n "${DEVELOPER_ID_CERT_BASE64:-}" ]]; then
    KEYCHAIN_PATH="$TMPDIR/pxve-build.keychain"
    KEYCHAIN_PASSWORD="$(uuidgen)"

    security create-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
    security set-keychain-settings -lut 3600 "$KEYCHAIN_PATH"
    security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"

    echo "$DEVELOPER_ID_CERT_BASE64" | base64 --decode > "$TMPDIR/pxve-cert.p12"
    security import "$TMPDIR/pxve-cert.p12" \
        -k "$KEYCHAIN_PATH" \
        -P "${DEVELOPER_ID_CERT_PASSWORD}" \
        -T /usr/bin/codesign \
        -A

    security list-keychain -d user -s "$KEYCHAIN_PATH" "$(security list-keychains -d user | tr -d '"')"
    security set-key-partition-list -S "apple-tool:,apple:" -s -k "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"

    codesign \
        --force \
        --options runtime \
        --sign "Developer ID Application" \
        --timestamp \
        "$MACOS_BIN"
else
    echo "  Skipping code signing (DEVELOPER_ID_CERT_BASE64 not set)"
    codesign --force --sign - "$MACOS_BIN"
fi

echo "==> Creating zip archives..."
cd "$DIST_DIR"
zip "${BINARY}-macos-arm64.zip" "${BINARY}-macos-arm64"

if [[ -n "${NOTARIZE_APPLE_ID:-}" ]]; then
    echo "==> Notarizing macOS binary..."
    xcrun notarytool submit "${BINARY}-macos-arm64.zip" \
        --apple-id "$NOTARIZE_APPLE_ID" \
        --team-id "$NOTARIZE_TEAM_ID" \
        --password "$NOTARIZE_APP_PASSWORD" \
        --wait
    # Flat binaries cannot be stapled; Gatekeeper verifies notarization online.
else
    echo "  Skipping notarization (NOTARIZE_APPLE_ID not set)"
fi

echo "==> Done:"
echo "    $DIST_DIR/${BINARY}-macos-arm64.zip"
