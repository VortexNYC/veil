#!/usr/bin/env bash
# Origin proof: Railway https://veil.nyc + public Hydra. Runs on the operator
# laptop only — no SSH, no local identity docker. Never prints tokens or secrets.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

ORIGIN="${VEIL_ORIGIN:-https://veil.nyc}"
ISSUER="${VEIL_HYDRA_ISSUER:-https://id.veil.nyc}"
VEIL_BIN="${VEIL_LIVE_BIN:-${VEIL_BIN:-$(command -v veil 2>/dev/null || echo "$HOME/.local/bin/veil")}}"
SECRET="${VEIL_LIVE_SECRET:-${VEIL_HYDRA_SECRET_FILE:-$HOME/.config/vortex/veil/cursor.hydra}}"
AGENT="${VEIL_LIVE_AGENT:-cursor}"
JWT_FILE="$(mktemp)"
CLI_OUT="$(mktemp)"
chmod 600 "$JWT_FILE" "$CLI_OUT"
cleanup() { rm -f "$JWT_FILE" "$CLI_OUT"; }
trap cleanup EXIT

if [ -n "${VEIL_LIVE_SSH:-}" ]; then
	echo "== note: VEIL_LIVE_SSH is ignored (prove-live is local-only since 2026-11)" >&2
fi

echo "== deploy-check"
sh scripts/deploy-check.sh

echo "== issuer $ISSUER"
curl -sf --max-time 15 "$ISSUER/.well-known/openid-configuration" >/dev/null

echo "== mint $AGENT (jwt not printed)"
VEIL_ORIGIN="$ORIGIN" VEIL_HYDRA_ISSUER="$ISSUER" \
	"$VEIL_BIN" agent token "$AGENT" --secret-file "$SECRET" --out-file "$JWT_FILE" >/dev/null
test -s "$JWT_FILE"

echo "== go test liveorigin"
VEIL_ORIGIN="$ORIGIN" VEIL_LIVE_JWT_FILE="$JWT_FILE" \
	go test -tags liveorigin ./internal/livetest -count=1 -timeout 5m

echo "== CLI use github (VEIL_ORIGIN HTTP)"
export VEIL_ORIGIN
"$VEIL_BIN" use --agent "$AGENT" --oidc-token-file "$JWT_FILE" \
	--item github --url https://api.github.com/user >"$CLI_OUT"
python3 - <<PY
import json, re
from pathlib import Path
raw = Path("$CLI_OUT").read_text()
if re.search(r"ghp_|gho_|github_pat_|lin_api_|cfut|cfat_|eyJ", raw):
    raise SystemExit("cli leaked secret pattern")
j = json.loads(raw)
if j.get("decision") != "allow" or j.get("status") != 200:
    raise SystemExit("cli github not allow/200")
body = json.loads(j.get("body") or "{}")
if not body.get("login"):
    raise SystemExit("cli github no login")
print("cli github ok")
PY

echo "== CLI deny wrong host"
"$VEIL_BIN" use --agent "$AGENT" --oidc-token-file "$JWT_FILE" \
	--item github --url https://example.com/ >"$CLI_OUT"
python3 - <<PY
import json
from pathlib import Path
j = json.loads(Path("$CLI_OUT").read_text())
if j.get("decision") != "deny" or j.get("reason") != "host_not_allowed":
    raise SystemExit("cli deny failed")
print("cli deny ok")
PY

echo "prove-live ok"
