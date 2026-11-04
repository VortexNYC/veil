#!/usr/bin/env bash
# One site at a time — headed browser stays open so you can finish 2FA and see the session.
# Usage: bash scripts/agentic-dogfood-prove.sh [ITEM_NAME LOGIN_URL]
# With no args, prints inventory then runs each target sequentially (3 min hold each).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RUN="$ROOT/scripts/run-agentic-signin.sh"
ORIGIN="${VEIL_ORIGIN:-https://veil.nyc}"

export VEIL_ORIGIN="$ORIGIN"
export VEIL_HEADLESS=0
export VEIL_HOLD_MS="${VEIL_HOLD_MS:-180000}"

ensure_bridge() {
  if [[ ! -S "$HOME/.veil/fill.sock" ]]; then
    echo "fill bridge not running — start Veil.app or: veil fill --bridge" >&2
    exit 1
  fi
}

run_one() {
  local name="$1" url="$2"
  echo ""
  echo "══════════════════════════════════════════════════════════════"
  echo "  $name"
  echo "  $url"
  echo "  Browser opens → Veil fills → you finish 2FA/passkey if asked"
  echo "  Window stays open $((VEIL_HOLD_MS / 1000))s"
  echo "══════════════════════════════════════════════════════════════"
  if bash "$RUN" "$name" "$url"; then
    echo "PROVE OK (fill + fields): $name"
  else
    echo "PROVE FAIL: $name (exit $?)" >&2
    return 1
  fi
}

ensure_bridge

if [[ $# -ge 2 ]]; then
  run_one "$1" "$2"
  exit 0
fi

bash "$ROOT/scripts/agent-login-dogfood.sh" || true

TARGETS=(
  "Stripe|https://dashboard.stripe.com/login"
  "github|https://github.com/login"
  "Google (Personal)|https://accounts.google.com"
  "cloudflare-login|https://dash.cloudflare.com/login"
  "LinkedIn|https://www.linkedin.com/login"
  "X (twitter)|https://x.com/i/flow/login"
  "Apple ID|https://appleid.apple.com"
)

fail=0
for row in "${TARGETS[@]}"; do
  IFS='|' read -r name url <<< "$row"
  run_one "$name" "$url" || fail=1
done

exit "$fail"
