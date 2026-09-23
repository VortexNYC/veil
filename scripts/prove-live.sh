#!/usr/bin/env bash
# Origin proof. Railway origin + https://veil.nyc. Never prints tokens or vault secrets.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

ORIGIN="${VEIL_ORIGIN:-https://veil.nyc}"
ISSUER="${VEIL_HYDRA_ISSUER:-https://id.veil.nyc}"
SSH_HOST="${VEIL_LIVE_SSH:-mini}"
MINI_HOME="${VEIL_LIVE_HOME:-/Users/shlomokabareti/.veil}"
MINI_BIN="${VEIL_LIVE_BIN:-/Users/shlomokabareti/.local/bin/veil}"
# mini's checkout stays at Projects/veil (PlasmaPOS/veil owns ~/Projects/veil there);
# agent hydra secrets live under ~/.config/vortex on both machines.
MINI_SECRET="${VEIL_LIVE_SECRET:-/Users/shlomokabareti/.config/vortex/veil/cursor.hydra}"
MINI_IDENTITY="${VEIL_LIVE_IDENTITY:-/Users/shlomokabareti/Projects/veil/identity}"
AGENT="${VEIL_LIVE_AGENT:-cursor}"
JWT_FILE="$(mktemp)"
CLI_OUT="$(mktemp)"
chmod 600 "$JWT_FILE"
chmod 600 "$CLI_OUT"

cleanup() { rm -f "$JWT_FILE" "$CLI_OUT"; }
trap cleanup EXIT

remote() {
  ssh -o BatchMode=yes "$SSH_HOST" "$@"
}

ensure_identity() {
  echo "== identity via $SSH_HOST"
  remote "set -euo pipefail
    cd '$MINI_IDENTITY'
    docker compose --env-file .env -f compose.yml up -d >/dev/null 2>&1
    for i in \$(seq 1 20); do
      if curl -sf -o /dev/null --max-time 2 http://127.0.0.1:4444/health/ready; then
        exit 0
      fi
      sleep 1
    done
    echo 'hydra not ready on mini' >&2
    exit 1
  "
}

mint() {
  echo "== mint $AGENT via $SSH_HOST (jwt not printed)"
  remote "set -euo pipefail
    export VEIL_HOME='$MINI_HOME' VEIL_HYDRA_ISSUER='$ISSUER'
    rm -f /tmp/veil-prove-live.jwt
    '$MINI_BIN' agent token '$AGENT' --secret-file '$MINI_SECRET' --out-file /tmp/veil-prove-live.jwt >/dev/null
    chmod 600 /tmp/veil-prove-live.jwt
  "
  scp -q "$SSH_HOST:/tmp/veil-prove-live.jwt" "$JWT_FILE"
  remote "rm -f /tmp/veil-prove-live.jwt"
}

echo "== origin $ORIGIN"
ensure_identity

code="$(curl -sS -o /tmp/veil-prove-health.txt -w '%{http_code}' --max-time 10 "$ORIGIN/health")"
echo "== GET /health http=$code"
test "$code" = "200"

ready_code="$(curl -sS -o /tmp/veil-prove-ready.txt -w '%{http_code}' --max-time 10 "$ORIGIN/ready" || true)"
echo "== GET /ready http=$ready_code"
if test "$ready_code" != "200"; then
  echo "== issuer discovery (Mini process may not have /ready until rebuild)"
  curl -sf --max-time 10 "$ISSUER/.well-known/openid-configuration" >/dev/null
fi

mint
test -s "$JWT_FILE"

echo "== go test liveorigin"
VEIL_ORIGIN="$ORIGIN" VEIL_LIVE_JWT_FILE="$JWT_FILE" \
  go test -tags liveorigin ./internal/livetest -count=1 -timeout 3m

echo "== CLI use github on $SSH_HOST"
remote "set -euo pipefail
  export VEIL_HOME='$MINI_HOME' VEIL_HYDRA_ISSUER='$ISSUER'
  '$MINI_BIN' use --agent '$AGENT' --item github --url https://api.github.com/user
" >"$CLI_OUT"
python3 - <<PY
import json, re
from pathlib import Path
raw = Path("$CLI_OUT").read_text()
if re.search(r"ghp_|lin_api_|cfut|cfat_|eyJ", raw):
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
remote "set -euo pipefail
  export VEIL_HOME='$MINI_HOME' VEIL_HYDRA_ISSUER='$ISSUER'
  '$MINI_BIN' use --agent '$AGENT' --item github --url https://example.com/
" >"$CLI_OUT"
python3 - <<PY
import json
from pathlib import Path
j = json.loads(Path("$CLI_OUT").read_text())
if j.get("decision") != "deny" or j.get("reason") != "host_not_allowed":
    raise SystemExit("cli deny failed")
print("cli deny ok")
PY

echo "prove-live ok"
