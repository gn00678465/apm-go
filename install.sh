#!/bin/sh
set -e

# apm-go installer (Linux / macOS) - release-download mode
#
# Downloads the apm-go binary for this platform from GitHub Releases,
# verifies its SHA256 checksum, installs it to ~/.local/bin, and
# ensures that directory is on PATH (appends to ~/.profile if not).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/gn00678465/apm-go/main/install.sh | sh
#
# Install a specific version:
#   curl -fsSL https://raw.githubusercontent.com/gn00678465/apm-go/main/install.sh | APM_GO_VERSION=0.2.1 sh

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

REPO="gn00678465/apm-go"
INSTALL_DIR="$HOME/.local/bin"
BINARY_NAME="apm-go"

fail() {
    printf "%bError: %s%b\n" "$RED" "$1" "$NC" >&2
    exit 1
}

# ---------------------------------------------------------------------------
# Stage 1 - Preflight: curl + platform detection
# ---------------------------------------------------------------------------

command -v curl >/dev/null 2>&1 || fail "curl not found in PATH."

case "$(uname -s)" in
    Linux)  os="linux" ;;
    Darwin) os="darwin" ;;
    *) fail "unsupported OS: $(uname -s) (supported: Linux, Darwin)" ;;
esac

case "$(uname -m)" in
    x86_64|amd64)  arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) fail "unsupported architecture: $(uname -m) (supported: x86_64, arm64)" ;;
esac

ASSET="$BINARY_NAME-$os-$arch"
if [ -n "${APM_GO_VERSION:-}" ]; then
    BASE_URL="https://github.com/$REPO/releases/download/v$APM_GO_VERSION"
else
    BASE_URL="https://github.com/$REPO/releases/latest/download"
fi

# ---------------------------------------------------------------------------
# Stage 2 - Download binary + checksums to a temp directory
# ---------------------------------------------------------------------------

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

printf "%bDownloading %s from %s ...%b\n" "$BLUE" "$ASSET" "$BASE_URL" "$NC"
curl -fsSL -o "$TMP_DIR/$ASSET" "$BASE_URL/$ASSET" \
    || fail "download failed: $BASE_URL/$ASSET"
curl -fsSL -o "$TMP_DIR/SHA256SUMS" "$BASE_URL/SHA256SUMS" \
    || fail "download failed: $BASE_URL/SHA256SUMS"

# ---------------------------------------------------------------------------
# Stage 3 - Verify checksum (fail-closed: no checksum tool = abort)
# ---------------------------------------------------------------------------

printf "%bVerifying checksum...%b\n" "$BLUE" "$NC"
cd "$TMP_DIR"
if command -v sha256sum >/dev/null 2>&1; then
    grep " $ASSET\$" SHA256SUMS | sha256sum -c - >/dev/null 2>&1 \
        || fail "SHA256 checksum mismatch for $ASSET."
elif command -v shasum >/dev/null 2>&1; then
    grep " $ASSET\$" SHA256SUMS | shasum -a 256 -c - >/dev/null 2>&1 \
        || fail "SHA256 checksum mismatch for $ASSET."
else
    fail "no sha256sum/shasum available; refusing to install unverified binary."
fi
cd - >/dev/null
printf "%bChecksum OK.%b\n" "$GREEN" "$NC"

# ---------------------------------------------------------------------------
# Stage 4 - Test the binary before installing
# ---------------------------------------------------------------------------

chmod +x "$TMP_DIR/$ASSET"
printf "%bTesting binary...%b\n" "$BLUE" "$NC"
if BINARY_TEST_OUTPUT=$("$TMP_DIR/$ASSET" --version 2>&1); then
    printf "%bBinary test successful: %s%b\n" "$GREEN" "$BINARY_TEST_OUTPUT" "$NC"
else
    printf "%bError: downloaded binary failed to run:%b\n" "$RED" "$NC" >&2
    echo "$BINARY_TEST_OUTPUT" >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# Stage 5 - Install
# ---------------------------------------------------------------------------

mkdir -p "$INSTALL_DIR"
cp "$TMP_DIR/$ASSET" "$INSTALL_DIR/$BINARY_NAME"
chmod +x "$INSTALL_DIR/$BINARY_NAME"

# ---------------------------------------------------------------------------
# Stage 6 - Ensure ~/.local/bin is on PATH
# ---------------------------------------------------------------------------

case ":$PATH:" in
    *":$INSTALL_DIR:"*)
        ;;
    *)
        printf '\nexport PATH="$HOME/.local/bin:$PATH"\n' >> "$HOME/.profile"
        printf "%bAdded %s to PATH via ~/.profile. Restart your shell to pick it up.%b\n" "$BLUE" "$INSTALL_DIR" "$NC"
        ;;
esac

echo ""
printf "%bapm-go installed successfully!%b\n" "$GREEN" "$NC"
printf "%bLocation: %s/%s%b\n" "$BLUE" "$INSTALL_DIR" "$BINARY_NAME" "$NC"
printf "%bRun 'apm-go --version' in a new terminal to verify the installation.%b\n" "$BLUE" "$NC"
