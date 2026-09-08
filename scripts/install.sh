#!/bin/sh
# token-usage installer: downloads the latest release binary from GitHub
# Releases, verifies its sha256 checksum, and installs it locally.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/emmmdty/token-usage/main/scripts/install.sh | sh
#
# Environment:
#   TOKEN_USAGE_INSTALL_DIR   target directory (default: ~/.local/bin)
#   TOKEN_USAGE_VERSION       install a specific version, e.g. v0.7.0
set -eu

REPO="emmmdty/token-usage"
INSTALL_DIR="${TOKEN_USAGE_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${TOKEN_USAGE_VERSION:-}"

err() { printf 'install.sh: %s\n' "$1" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || err "required tool not found: $1"; }

fetch() { # fetch <url> <outfile>
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1" -o "$2"
    else
        wget -qO "$2" "$1"
    fi
}

need uname
need mktemp
command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 ||
    err "required tool not found: curl or wget"
command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1 ||
    err "required tool not found: sha256sum or shasum"

os=$(uname -s)
case "$os" in
    Linux) os="linux" ;;
    Darwin) os="darwin" ;;
    *) err "unsupported OS: $os" ;;
esac

arch=$(uname -m)
case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) err "unsupported architecture: $arch" ;;
esac

if [ -z "$VERSION" ]; then
    # Resolve the latest tag via the releases/latest redirect (no API rate
    # limits, unlike the GitHub REST API for unauthenticated clients).
    if command -v curl >/dev/null 2>&1; then
        VERSION=$(curl -fsSI -o /dev/null -w '%{redirect_url}' \
            "https://github.com/$REPO/releases/latest" | sed 's|.*/tag/||')
    else
        VERSION=$(wget -q -S --spider -O /dev/null \
            "https://github.com/$REPO/releases/latest" 2>&1 |
            sed -n 's|^ *[Ll]ocation: .*tag/\([^ ]*\).*$|\1|p' | tail -1)
    fi
    [ -n "$VERSION" ] || err "could not determine the latest release"
fi

asset="token-usage_${os}_${arch}"
base="https://github.com/$REPO/releases/download/$VERSION"
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

printf 'Installing token-usage %s (%s/%s) -> %s\n' "$VERSION" "$os" "$arch" "$INSTALL_DIR"
fetch "$base/$asset" "$tmpdir/$asset"
fetch "$base/checksums.txt" "$tmpdir/checksums.txt"

# Verify the checksum before trusting the binary.
if command -v sha256sum >/dev/null 2>&1; then
    hasher=sha256sum
else
    need shasum
    hasher="shasum -a 256"
fi
want=$(grep " $asset\$" "$tmpdir/checksums.txt" | awk '{print $1}')
[ -n "$want" ] || err "no checksum entry for $asset"
got=$($hasher "$tmpdir/$asset" | awk '{print $1}')
[ "$want" = "$got" ] || err "checksum mismatch for $asset (want $want, got $got)"

mkdir -p "$INSTALL_DIR"
install_target="$INSTALL_DIR/token-usage"
mv "$tmpdir/$asset" "$install_target"
chmod +x "$install_target"

case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) printf 'Note: %s is not on your PATH. Add it to your shell profile.\n' "$INSTALL_DIR" ;;
esac
printf 'Done. Run: token-usage --help\n'
