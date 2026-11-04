#!/usr/bin/env bash
# Run examples/agentic-autofill/signin.mjs with Playwright Chromium + Veil extension.
# Auto-approves the fill request via human JWT (CLI). No secrets printed.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
EXAMPLE="$ROOT/examples/agentic-autofill"
ITEM="${1:?usage: run-agentic-signin.sh ITEM_NAME LOGIN_URL [SUCCESS_URL]}"
LOGIN_URL="${2:?}"
SUCCESS_URL="${3:-$LOGIN_URL}"
AGENT="${VEIL_AGENT:-cursor}"
ORIGIN="${VEIL_ORIGIN:-https://veil.nyc}"
HUMAN_JWT="${VEIL_OIDC_TOKEN_FILE:-$HOME/.config/veil/human.jwt}"
AGENT_JWT="${VEIL_AGENT_JWT_FILE:-$HOME/.config/vortex/veil/${AGENT}.jwt}"

cd "$EXAMPLE"
if [[ ! -d node_modules/playwright-core ]]; then
  pnpm install --dir "$EXAMPLE" --silent
fi
CFTP="$HOME/Library/Caches/ms-playwright/chromium-1248/chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"
if [[ -x "$CFTP" ]]; then
  export VEIL_CHROMIUM_EXECUTABLE="$CFTP"
else
  pnpm exec --dir "$EXAMPLE" playwright-core install chromium
fi

GRANT_IDS="$(VEIL_ORIGIN="$ORIGIN" veil grant list | jq -r --arg a "$AGENT" '.[] | select(.agent_id==$a) | .item_id')"
ITEM_ID="$(VEIL_ORIGIN="$ORIGIN" veil item list | jq -r --arg n "$ITEM" --arg ids "$GRANT_IDS" '
  [.[] | select(.name==$n) | .id] as $candidates
  | ($ids | split("\n") | map(select(length>0))) as $granted
  | ([$candidates[] | select(. as $i | $granted | index($i))] | .[0]) // $candidates[0] // empty
')"
if [[ -z "$ITEM_ID" ]]; then
  echo "unknown item: $ITEM" >&2
  exit 1
fi

export VEIL_ORIGIN="$ORIGIN"
export VEIL_AGENT_TOKEN="$(tr -d ' \n' <"$AGENT_JWT")"
export VEIL_EXTENSION_PATH="${VEIL_EXTENSION_PATH:-$HOME/.veil/extension}"
export VEIL_LOGIN_URL="$LOGIN_URL"
export VEIL_SUCCESS_URL="$SUCCESS_URL"
export VEIL_ITEM_ID="$ITEM_ID"
export VEIL_SKIP_SUCCESS_URL="${VEIL_SKIP_SUCCESS_URL:-1}"
export VEIL_HEADLESS="${VEIL_HEADLESS:-0}"
export VEIL_HOLD_MS="${VEIL_HOLD_MS:-$([[ "${VEIL_HEADLESS:-0}" == 1 ]] && echo 5000 || echo 180000)}"

PROFILE="${VEIL_AGENTIC_PROFILE:-$HOME/.veil/agentic-chromium-profile}"
NM="$PROFILE/NativeMessagingHosts"
SRC_NM="$HOME/Library/Application Support/Google/Chrome for Testing/NativeMessagingHosts/nyc.veil.fill.json"
mkdir -p "$NM"
if [[ -f "$SRC_NM" ]]; then
  cp "$SRC_NM" "$NM/nyc.veil.fill.json"
fi

node <<'NODE'
import { execFileSync } from "node:child_process";
import { signIn } from "./signin.mjs";

await signIn({
  showApproval: async (_url, requestId) => {
    if (!requestId) throw new Error("missing request_id from origin");
    const humanJwt = process.env.VEIL_OIDC_TOKEN_FILE || `${process.env.HOME}/.config/veil/human.jwt`;
    const env = {
      ...process.env,
      VEIL_ORIGIN: process.env.VEIL_ORIGIN,
      VEIL_OIDC_TOKEN_FILE: humanJwt,
    };
    if (process.env.VEIL_AUTO_APPROVE === "1") {
      execFileSync("veil", ["request", "approve", requestId], { stdio: "inherit", env });
      return;
    }
    execFileSync("veil", ["request", "confirm", requestId], { stdio: "inherit", env });
  },
});
console.error("agentic sign-in finished");
NODE
