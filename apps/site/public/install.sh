#!/bin/sh
# Veil — one-shot install. https://veil.nyc
set -eu

say() { printf '%s\n' "$*"; }
die() { say "veil: $*" >&2; exit 1; }

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

VER="$("$BIN" --help 2>/dev/null | head -1 || true)"

cat <<EOF

veil: installed — $BIN

Next steps:
  1. Accept your invite — the setup link arrives by email (private alpha).
  2. Sign in:            $BIN human login --out-file ~/.config/veil/human.jwt
  3. Provision your org: VEIL_ORIGIN=https://veil.nyc $BIN init --oidc-token-file ~/.config/veil/human.jwt
  4. Bind an agent:      $BIN agent hydra <name>
  5. MCP for your agent: $BIN mcp laptop
  6. Browser fill:       VEIL_ORIGIN=https://veil.nyc $BIN fill install
     then load unpacked at chrome://extensions (Developer mode) → ~/.veil/extension

Docs: https://veil.nyc/docs · Changelog: https://veil.nyc/docs/changelog
EOF
