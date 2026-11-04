#!/usr/bin/env bash
# On-the-fly agentic login: vb (agent-browser) + veil CLI + worker CDP fill.
# No Playwright persistent runner. You watch the browser and finish 2FA.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ITEM="${1:?usage: vb-agentic-login.sh ITEM_NAME LOGIN_URL [vb-session]}"
LOGIN_URL="${2:?}"
SESSION="${3:-veil-dogfood}"
AGENT="${VEIL_AGENT:-cursor}"
ORIGIN="${VEIL_ORIGIN:-https://veil.nyc}"
EXT="${VEIL_EXTENSION_PATH:-$HOME/.veil/extension}"
PROFILE="${VEIL_AGENTIC_PROFILE:-$HOME/.veil/agent-browser-profile}"
HUMAN_JWT="${VEIL_OIDC_TOKEN_FILE:-$HOME/.config/veil/human.jwt}"
AGENT_JWT="${VEIL_AGENT_JWT_FILE:-$HOME/.config/vortex/veil/${AGENT}.jwt}"

export AGENT_BROWSER_SESSION="$SESSION"
export VEIL_ORIGIN="$ORIGIN"

GRANT_IDS="$(veil grant list | jq -r --arg a "$AGENT" '.[] | select(.agent_id==$a) | .item_id')"
ITEM_ID="$(veil item list | jq -r --arg n "$ITEM" --arg ids "$GRANT_IDS" '
  [.[] | select(.name==$n) | .id] as $candidates
  | ($ids | split("\n") | map(select(length>0))) as $granted
  | ([$candidates[] | select(. as $i | $granted | index($i))] | .[0]) // $candidates[0] // empty
')"
if [[ -z "$ITEM_ID" ]]; then
  echo "unknown item: $ITEM" >&2
  exit 1
fi

if [[ ! -S "$HOME/.veil/fill.sock" ]]; then
  echo "start fill bridge: Veil.app or veil fill --bridge" >&2
  exit 1
fi

CFTE="${VEIL_CHROMIUM_EXECUTABLE:-$HOME/.agent-browser/browsers/chrome-154.0.8037.57/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing}"
if [[ -x "$CFTE" ]]; then
  export AGENT_BROWSER_EXECUTABLE_PATH="$CFTE"
fi
NM="$PROFILE/NativeMessagingHosts"
SRC_NM="$HOME/Library/Application Support/Google/Chrome for Testing/NativeMessagingHosts/nyc.veil.fill.json"
if [[ ! -f "$SRC_NM" ]]; then
  SRC_NM="$HOME/Library/Application Support/Google/Chrome/NativeMessagingHosts/nyc.veil.fill.json"
fi
mkdir -p "$NM"
[[ -f "$SRC_NM" ]] && cp "$SRC_NM" "$NM/nyc.veil.fill.json"

echo "session=$SESSION item=$ITEM id=$ITEM_ID" >&2
echo "opening headed browser (vb + Veil extension)…" >&2
vb "$SESSION" --headed \
  --extension "$EXT" \
  --profile "$PROFILE" \
  open "$LOGIN_URL" >&2

sleep 2
export VEIL_AGENT_TOKEN="$(tr -d ' \n' <"$AGENT_JWT")"
FILED="$(curl -sS -X POST "$ORIGIN/v1/agent/fill/request" \
  -H "Authorization: Bearer $VEIL_AGENT_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"item_id\":\"$ITEM_ID\",\"url\":\"$LOGIN_URL\"}")"
REQUEST_ID="$(echo "$FILED" | jq -r .request_id)"
if [[ -z "$REQUEST_ID" || "$REQUEST_ID" == "null" ]]; then
  echo "fill request failed: $FILED" >&2
  exit 1
fi
echo "approve: $REQUEST_ID (app.veil.nyc/requests or CLI)" >&2
VEIL_OIDC_TOKEN_FILE="$HUMAN_JWT" veil request approve "$REQUEST_ID"

PAGE_MATCH="$(python3 -c "from urllib.parse import urlparse; u=urlparse('$LOGIN_URL'); print(u.netloc+u.path.rstrip('/'))")"
export REQUEST_ID VEIL_ITEM_ID="$ITEM_ID" PAGE_URL_SUBSTR="${PAGE_MATCH}"
export AGENT_BROWSER_SESSION="$SESSION"

cd "$ROOT/examples/agentic-autofill"
pnpm exec node worker-fill.mjs

echo "snapshot (verify fields / next step):" >&2
agent-browser snapshot -i -c | head -30

HOLD="${VEIL_HOLD_MS:-300000}"
echo "browser stays up ~$((HOLD / 1000))s — finish 2FA/passkey in the Chrome window" >&2
sleep "$HOLD"
