#!/usr/bin/env bash
# Compare 1Password item metadata (local op CLI) to Veil origin items.
# No secrets printed. Requires: op signed in, VEIL human session (fill.json or env).
set -euo pipefail

ORIGIN="${VEIL_ORIGIN:-https://veil.nyc}"
VAULT="${1:-Agents}"
OP="${OP_BIN:-$(command -v op 2>/dev/null || echo "$HOME/.local/bin/op")}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "== Veil items from $ORIGIN"
VEIL_ORIGIN="$ORIGIN" veil item list >"$tmp/veil.json"

echo "== 1Password vault: $VAULT"
"$OP" item list --vault "$VAULT" --format json >"$tmp/op.json"

python3 <<PY
import json, sys
from urllib.parse import urlparse

def norm_name(s):
    return (s or "").strip().lower()

veil = json.load(open("$tmp/veil.json"))
op = json.load(open("$tmp/op.json"))
vby = {norm_name(i["name"]): i for i in veil}
# last wins on dup names in veil listing

matched, only_op, only_veil = [], [], []
for it in op:
    t = it.get("title", "")
    if norm_name(t) in vby:
        matched.append(t)
    else:
        only_op.append({"title": t, "category": it.get("category", ""), "id": it.get("id", "")})

op_names = {norm_name(it.get("title", "")) for it in op}
for i in veil:
    if norm_name(i["name"]) not in op_names:
        only_veil.append(i["name"])

print(f"veil_items={len(veil)} op_items={len(op)} matched_by_name={len(matched)}")
print(f"only_in_1password={len(only_op)} only_in_veil={len(only_veil)}")
if only_op:
    print("\n-- Import or add in Veil before deleting from 1Password --")
    for row in sorted(only_op, key=lambda x: x["title"].lower())[:50]:
        print(f"  [{row['category']}] {row['title']}")
    if len(only_op) > 50:
        print(f"  ... and {len(only_op)-50} more")
if only_veil and len(only_veil) <= 20:
    print("\n-- In Veil but not in this OP vault (expected if vault is Agents-only) --")
    for n in sorted(only_veil)[:20]:
        print(f"  {n}")
PY

echo ""
echo "Next: per login, validate fill + 2FA on site, then: op item delete \"TITLE\" --vault $VAULT"
