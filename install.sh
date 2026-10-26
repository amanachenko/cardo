#!/bin/sh
# Cardo launcher installer for macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/amanachenko/cardo/main/install.sh | sh
#
# What it does: detect the platform, download the archive and checksums.txt for the same release,
# verify the SHA-256, unpack the binary, and tell you the one command that starts the installation.
# It does not use sudo, does not touch anything outside the install directory, and does not read
# any file in your project.
#
# Environment:
#   CARDO_VERSION   release tag to install (default: latest)
#   CARDO_BIN       install directory (default: $HOME/.local/bin)
#   CARDO_BASE_URL  override the download base (used by the offline smoke test)
set -eu

REPO="amanachenko/cardo"
if [ -n "${CARDO_BASE_URL:-}" ]; then
	BASE="$CARDO_BASE_URL"
elif [ -n "${CARDO_VERSION:-}" ]; then
	BASE="https://github.com/${REPO}/releases/download/${CARDO_VERSION}"
else
	BASE="https://github.com/${REPO}/releases/latest/download"
fi
BIN_DIR="${CARDO_BIN:-$HOME/.local/bin}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

say() { printf '%s\n' "$*"; }
die() { printf 'cardo installer: %s\n' "$*" >&2; exit 1; }

fetch() {
	# $1 url, $2 output
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		die "neither curl nor wget is available"
	fi
}

os="$(uname -s)"
case "$os" in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) die "unsupported system: $os. Windows uses install.ps1." ;;
esac

arch="$(uname -m)"
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) die "unsupported architecture: $arch" ;;
esac

asset="cardo_${os}_${arch}.tar.gz"
say "platform: ${os}/${arch}"

fetch "$BASE/$asset" "$TMP/$asset"
fetch "$BASE/checksums.txt" "$TMP/checksums.txt"

expected="$(awk -v a="$asset" '$2 == a { print $1 }' "$TMP/checksums.txt")"
[ -n "$expected" ] || die "checksums.txt has no entry for $asset"

if command -v sha256sum >/dev/null 2>&1; then
	actual="$(sha256sum "$TMP/$asset" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
	actual="$(shasum -a 256 "$TMP/$asset" | awk '{ print $1 }')"
else
	die "no SHA-256 tool available (sha256sum or shasum)"
fi

[ "$expected" = "$actual" ] || die "checksum mismatch for $asset: refusing to install"

tar -xzf "$TMP/$asset" -C "$TMP"
[ -f "$TMP/cardo" ] || die "the archive did not contain a cardo binary"

mkdir -p "$BIN_DIR"
install -m 0755 "$TMP/cardo" "$BIN_DIR/cardo" 2>/dev/null || {
	cp "$TMP/cardo" "$BIN_DIR/cardo"
	chmod 0755 "$BIN_DIR/cardo"
}

say "installed: $BIN_DIR/cardo ($("$BIN_DIR/cardo" version))"

case ":${PATH}:" in
	*":$BIN_DIR:"*) ;;
	*)
		say ""
		say "$BIN_DIR is not on your PATH. Add this line to your shell profile:"
		say "  export PATH=\"$BIN_DIR:\$PATH\""
		;;
esac

say ""
say "next: cardo ui      (starts the installer; Docker Desktop must be running)"
