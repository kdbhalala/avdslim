#!/usr/bin/env bash
set -e

REPO="kdbhalala/avdslim"
VERSION="v1.0.15"

# Detect OS and Arch
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin)
    OS_NAME="darwin"
    ;;
  Linux)
    OS_NAME="linux"
    ;;
  *)
    echo "❌ Unsupported OS: $OS"
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64|amd64)
    ARCH_NAME="amd64"
    ;;
  arm64|aarch64)
    ARCH_NAME="arm64"
    ;;
  *)
    echo "❌ Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

TARBALL="avdslim_${VERSION}_${OS_NAME}_${ARCH_NAME}.tar.gz"

echo "⚡ Installing AVD-SLIM (${VERSION}) for ${OS_NAME}/${ARCH_NAME}..."

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

fetch() { # fetch <asset>
  if command -v gh >/dev/null 2>&1 &&
    gh release download "$VERSION" --repo "$REPO" -p "$1" -D "$TMPDIR" >/dev/null 2>&1; then
    return 0
  fi
  curl -fsSL --connect-timeout 10 --retry 3 "https://github.com/${REPO}/releases/download/${VERSION}/$1" -o "$TMPDIR/$1"
}

fetch "$TARBALL"
fetch checksums.txt

# Refuse to install a binary that does not match the release's checksums.txt.
EXPECTED=$(awk -v f="$TARBALL" '$2 == f { print $1 }' "$TMPDIR/checksums.txt")
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "$TMPDIR/$TARBALL" | awk '{ print $1 }')
else
  ACTUAL=$(shasum -a 256 "$TMPDIR/$TARBALL" | awk '{ print $1 }')
fi
if [ -z "$EXPECTED" ] || [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "❌ Checksum mismatch for $TARBALL (expected ${EXPECTED:-none}, got $ACTUAL). Not installing."
  exit 1
fi
echo "✓ Checksum verified"

tar -xzf "$TMPDIR/$TARBALL" -C "$TMPDIR"

# Determine target install directory
if [ -d "/opt/homebrew/bin" ] && [ -w "/opt/homebrew/bin" ]; then
  DEST="/opt/homebrew/bin"
elif [ -d "/usr/local/bin" ] && [ -w "/usr/local/bin" ]; then
  DEST="/usr/local/bin"
elif [ -d "$HOME/.local/bin" ]; then
  DEST="$HOME/.local/bin"
else
  mkdir -p "$HOME/.local/bin"
  DEST="$HOME/.local/bin"
fi

BIN_PATH=$(find "$TMPDIR" -type f -name "avdslim" | head -n 1)
if [ -z "$BIN_PATH" ]; then
  echo "❌ Could not find avdslim binary in downloaded archive."
  exit 1
fi

mv "$BIN_PATH" "$DEST/avdslim"
chmod +x "$DEST/avdslim"

echo "✅ AVD-SLIM installed successfully to $DEST/avdslim"
"$DEST/avdslim" version
