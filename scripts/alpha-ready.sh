#!/bin/sh
# Pre-cohort gate: public surfaces + origin contract (Use/list/MCP leak checks).
# Does not print tokens or vault secrets. Run from repo root on main.
#
#   sh scripts/alpha-ready.sh              # deploy-check + liveorigin
#   VEIL_LIVE_AGENT=devin sh scripts/alpha-ready.sh
#
# Full laptop CLI proof (needs ssh mini): make prove-live
set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

AGENT="${VEIL_LIVE_AGENT:-cursor}"
ORIGIN="${VEIL_ORIGIN:-https://veil.nyc}"
SECRET="${VEIL_HYDRA_SECRET_FILE:-$HOME/.config/vortex/veil/${AGENT}.hydra}"
JWT="$(mktemp)"
chmod 600 "$JWT"
cleanup() { rm -f "$JWT"; }
trap cleanup EXIT

echo "== deploy-check"
sh scripts/deploy-check.sh

echo "== mint $AGENT (jwt not printed)"
VEIL_ORIGIN="$ORIGIN" veil agent token "$AGENT" \
	--secret-file "$SECRET" --out-file "$JWT" >/dev/null

echo "== liveorigin (https://veil.nyc contract)"
VEIL_ORIGIN="$ORIGIN" VEIL_LIVE_JWT_FILE="$JWT" \
	go test -tags liveorigin ./internal/livetest -count=1 -timeout 5m

echo "alpha-ready ok (run make prove-live before a large cohort if the laptop path changed)"
