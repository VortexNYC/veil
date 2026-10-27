#!/bin/sh
# Veil — one-shot install. https://veil.nyc
set -eu

say() { printf '%s\n' "$*"; }
die() { say "veil: $*" >&2; exit 1; }

INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
VERSION="0.1.0"
BIN=""

# --- platform detection -----------------------------------------------------
OS="$(uname -s)"
case "$OS" in
	Darwin) OS=darwin ;;
	Linux)  OS=linux ;;
	*)      OS="" ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
	arm64|aarch64) ARCH=arm64 ;;
	x86_64|amd64)  ARCH=amd64 ;;
	*)             ARCH="" ;;
esac

# --- try the release tarball first ------------------------------------------
if [ -n "$OS" ] && [ -n "$ARCH" ]; then
	URL="https://github.com/VortexNYC/veil/releases/latest/download/veil_${VERSION}_${OS}_${ARCH}.tar.gz"
	TMP="$(mktemp -d 2>/dev/null || mktemp -d -t veil)"
	trap 'rm -rf "$TMP"' EXIT
	say "veil: downloading $URL"
	if command -v curl >/dev/null 2>&1; then
		DL="curl -fsSL -o $TMP/veil.tar.gz $URL"
	elif command -v wget >/dev/null 2>&1; then
		DL="wget -q -O $TMP/veil.tar.gz $URL"
	else
		DL=""
	fi
	if [ -n "$DL" ] && sh -c "$DL" 2>/dev/null \
		&& tar -xzf "$TMP/veil.tar.gz" -C "$TMP" veil 2>/dev/null; then
		mkdir -p "$INSTALL_DIR"
		mv "$TMP/veil" "$INSTALL_DIR/veil"
		chmod 755 "$INSTALL_DIR/veil"
		BIN="$INSTALL_DIR/veil"
		say "veil: installed binary — $BIN"
	else
		say "veil: binary download unavailable, falling back to go install"
	fi
else
	say "veil: unsupported platform ($OS/$ARCH), falling back to go install"
fi

# --- fallback: build from source --------------------------------------------
if [ -z "$BIN" ]; then
	command -v go >/dev/null 2>&1 || die "Go is required — install it from https://go.dev/dl and re-run."

	say "veil: installing CLI…"
	go install github.com/VortexNYC/veil/cmd/veil@latest

	GOBIN="$(go env GOBIN)"
	[ -n "$GOBIN" ] || GOBIN="$(go env GOPATH)/bin"
	BIN="$GOBIN/veil"
	[ -x "$BIN" ] || die "install reported success but $BIN is missing"

	case ":$PATH:" in
		*":$GOBIN:"*) ;;
		*) say "veil: add $GOBIN to your PATH" ;;
	esac
fi

case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*) say "veil: add $INSTALL_DIR to your PATH" ;;
esac

VER="$("$BIN" --help 2>/dev/null | head -1 || true)"

cat <<EOF

veil: installed — $BIN

Next steps:
  1. Create your account at https://login.veil.nyc/registration (or accept an org invite).
  2. Sign in:            $BIN human login --out-file ~/.config/veil/human.jwt
  3. Provision your org: VEIL_ORIGIN=https://veil.nyc $BIN init --oidc-token-file ~/.config/veil/human.jwt
  4. Bind an agent:      $BIN agent hydra <name>
  5. MCP for your agent: $BIN mcp laptop
  6. Browser fill:       VEIL_ORIGIN=https://veil.nyc $BIN fill install
     then load unpacked at chrome://extensions (Developer mode) → ~/.veil/extension

Docs: https://veil.nyc/docs · Changelog: https://veil.nyc/docs/changelog
EOF
