#!/usr/bin/env bash
# Ramp-to-break runner: run agent-mix.js at each VU level, record the k6
# summary, mark pass/fail against the SLO (p95<500, p99<1000, <1% errors),
# and stop at the first failing level. Then confirm the last passing level
# with a repeat — the methodology the launch-gate run documents.
#
# Required env:
#   VEIL_ORIGIN (or VEIL_ORIGINS) — target origin(s)
#   VEIL_TOKENS_FILE / VEIL_ITEMS_FILE — seeded fixtures (or VEIL_TOKENS / VEIL_ITEM_IDS)
# Optional:
#   VEIL_LEVELS="10 50 100 200 400 800 1600 3200"  — the doubling ladder
#   VEIL_HOLD_S=120  VEIL_RAMP_S=30                 — steady-state + ramp per level
#   LOADTEST_OUT=tests/load/k6/out                  — per-level summaries land in out/<run>/
set -uo pipefail

cd "$(dirname "$0")/../.."

OUT="${LOADTEST_OUT:-tests/load/k6/out}/ramp-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$OUT"
LEVELS="${VEIL_LEVELS:-10 50 100 200 400 800 1600 3200}"
HOLD="${VEIL_HOLD_S:-120}"
RAMP="${VEIL_RAMP_S:-30}"

pass=0
last_pass=0
echo "level rps avg med p95 p99 err% verdict" | tee "$OUT/results.tsv"
for vus in $LEVELS; do
  if [ "$vus" -gt 0 ] && [ -f "$OUT/vu-$vus.json" ]; then continue; fi
  VEIL_VUS="$vus" VEIL_HOLD_S="$HOLD" VEIL_RAMP_S="$RAMP" \
    k6 run --summary-export "$OUT/vu-$vus.json" tests/load/k6/agent-mix.js
  rc=$?
  line=$(python3 - "$OUT/vu-$vus.json" "$vus" "$rc" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
m = d.get("metrics", {})
def get(name, key):
    v = m.get(name, {})
    return (v.get("values") if isinstance(v.get("values"), dict) else v).get(key, 0) or 0
dur = m.get("http_req_duration", {})
if isinstance(dur.get("values"), dict):
    dur = dur["values"]
fail_rate = get("http_req_failed", "value") or get("http_req_failed", "rate")
vus, rc = sys.argv[2], int(sys.argv[3])
verdict = "PASS" if rc == 0 else "FAIL"
print(f"{vus}\t{get('http_reqs','rate'):.1f}\t{dur.get('avg',0):.1f}\t{dur.get('med',0):.1f}\t{dur.get('p(95)',0):.0f}\t{dur.get('p(99)',0):.0f}\t{fail_rate*100:.2f}\t{verdict}")
PY
)
  echo "$line" | tee -a "$OUT/results.tsv"
  if [ "$rc" -eq 0 ]; then
    pass=$((pass+1)); last_pass=$vus
  else
    echo "first failure at ${vus} VUs — last passing level: ${last_pass}" | tee -a "$OUT/results.tsv"
    if [ "$last_pass" -gt 0 ]; then
      echo "confirming ${last_pass} once more…" | tee -a "$OUT/results.tsv"
      VEIL_VUS="$last_pass" VEIL_HOLD_S="$HOLD" VEIL_RAMP_S="$RAMP" \
        k6 run --summary-export "$OUT/vu-${last_pass}-confirm.json" tests/load/k6/agent-mix.js \
        && echo "CONFIRMED ${last_pass}" | tee -a "$OUT/results.tsv" \
        || echo "CONFIRM FAILED — knee is below ${last_pass}" | tee -a "$OUT/results.tsv"
    fi
    break
  fi
done
echo "run dir: $OUT"
