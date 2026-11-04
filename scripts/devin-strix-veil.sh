#!/usr/bin/env bash
# Local Devin SWE-2 drives a self-hosted Strix auth scan on Veil (OSS CLI only).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export PATH="${HOME}/.strix/bin:${HOME}/.local/bin:${PATH}"

ENV_FILE="${STRIX_ENV_FILE:-${HOME}/.config/vortex/strix.env}"
if [[ ! -f "$ENV_FILE" ]]; then
  echo "devin-strix-veil: missing $ENV_FILE (STRIX_LLM, LLM_API_KEY, DEVIN_MODEL)" >&2
  exit 1
fi
# shellcheck disable=SC1090
set -a && source "$ENV_FILE" && set +a

DEVIN_MODEL="${DEVIN_MODEL:-swe-2-high}"
STRIX_BUDGET="${STRIX_MAX_BUDGET:-10}"
STRIX_TURNS="${STRIX_MAX_TURNS:-50}"
MODE="${1:-devin}" # devin | strix-only

run_strix() {
  docker info >/dev/null
  strix --version >/dev/null
  make security-fast
  strix -n -m quick --max-budget "$STRIX_BUDGET" --max-turns "$STRIX_TURNS" \
    -t ./internal/publicapi \
    -t ./internal/human \
    -t ./internal/dpop \
    -t ./identity/glue \
    -t ./internal/mcpserver \
    --instruction "White-box auth and access-control review for Veil broker and Ory glue. No outbound attacks against production. No exfiltration of real secrets in reports. Prefer missing MFA/owner gates, IDOR, session/DPoP bypass hypotheses validated against code."
}

summarize_prompt='Follow devin-strix-local. You are Devin SWE-2 summarizing a finished self-hosted Strix run (not Strix Cloud). Read the newest directory under strix_runs/ and make security-fast output already on disk. Produce: severity-ordered findings with file:line, false-positive calls, and a fix punch list. No prod URLs. Do not run strix cloud. Do not auto-commit.'

case "$MODE" in
  strix-only)
    run_strix
    ;;
  devin)
    run_strix
    devin -p --permission-mode auto --model "$DEVIN_MODEL" --respect-workspace-trust false -- "$summarize_prompt"
    ;;
  devin-drive)
    devin -p --permission-mode smart --model "$DEVIN_MODEL" --respect-workspace-trust false -- \
      "Follow devin-strix-local. From repo root run: scripts/devin-strix-veil.sh strix-only (after sourcing ~/.config/vortex/strix.env). Then summarize strix_runs/ as a punch list. No strix cloud, no prod URLs, no auto-commit."
    ;;
  *)
    echo "usage: $0 [devin|strix-only|devin-drive]" >&2
    exit 2
    ;;
esac
