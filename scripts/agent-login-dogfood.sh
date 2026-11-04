#!/usr/bin/env bash
# Agent-driven website login dogfood — inventory grants, items, and gaps.
# No secrets. Requires VEIL_ORIGIN + human JWT (VEIL_HUMAN_TOKEN_FILE or default paths).
set -euo pipefail

ORIGIN="${VEIL_ORIGIN:-https://veil.nyc}"
AGENT="${VEIL_AGENT:-cursor}"
export VEIL_ORIGIN="$ORIGIN"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

veil item list >"$tmp/items.json"
veil grant list >"$tmp/grants.json"

python3 <<PY
import json, os, sys
from urllib.parse import urlparse

agent = os.environ.get("VEIL_AGENT", "cursor")
items = json.load(open("${tmp}/items.json"))
grants = json.load(open("${tmp}/grants.json"))
grant_by_item = {g["item_id"]: g for g in grants if g.get("agent_id") == agent}

# Targets: human name hint, login URL, complexity notes for the operator.
TARGETS = [
    ("Apple", "Apple ID", "https://appleid.apple.com", "Apple 2FA / passkeys / device codes; fix empty URI on item"),
    ("Google", "Google (Personal)", "https://accounts.google.com", "TOTP; SSO hub for many apps"),
    ("Cloudflare", "cloudflare-login", "https://dash.cloudflare.com", "TOTP; API item separate (cloudflare)"),
    ("Vercel", "Vercel Token (night-shift-agent)", "https://vercel.com/login", "Dashboard login may need its own item; token is API-only today"),
    ("GitHub", "github", "https://github.com/login", "API Use proven; site login + 2FA via fill"),
    ("Stripe", "Stripe", "https://dashboard.stripe.com/login", "TOTP; passkey register in Veil; level1 fill"),
    ("LinkedIn", "LinkedIn", "https://www.linkedin.com/login", "TOTP; bot / checkpoint risk"),
    ("X / Twitter", "X (twitter)", "https://x.com/i/flow/login", "TOTP; duplicate X items — pick one grant"),
]

by_name = {}
for it in items:
    by_name[it["name"]] = it

print(f"origin={os.environ.get('VEIL_ORIGIN','')} agent={agent}")
print(f"veil_items={len(items)} cursor_grants={len(grant_by_item)}")
print()
print("service\titem\tgrant\tlogin_uri\ttotp\tpasskeys\tnotes")
print("-" * 100)

gaps = []
for svc, name, login_url, notes in TARGETS:
    it = by_name.get(name)
    if not it:
        # fuzzy: first name match
        for k, v in by_name.items():
            if k.lower() == name.lower():
                it = v
                name = k
                break
    if not it:
        print(f"{svc}\tMISSING\t-\t-\t-\t-\tadd item or fix name ({name})")
        gaps.append(f"add item: {name}")
        continue
    iid = it["id"]
    g = grant_by_item.get(iid)
    grant = g["level"] if g else "NONE"
    uris = it.get("uris") or []
    host_ok = any(login_url.split("/")[2] in u for u in uris) if uris else False
    uri_cell = "ok" if host_ok else ("EMPTY" if not uris else "mismatch")
    totp = "yes" if it.get("has_totp") else "no"
    # passkeys counted separately on origin list — not in list API always
    pk = "?" 
    if grant == "NONE":
        gaps.append(f"veil grant add --agent {agent} --item {iid} --level level1  # {name}")
    if uri_cell != "ok" and svc != "Vercel":
        gaps.append(f"veil item update \"{name}\" --uri {login_url!r}  # {svc}")
    print(f"{svc}\t{name}\t{grant}\t{uri_cell}\t{totp}\t{pk}\t{notes}")

print()
print("== Agentic fill loop (per site, after grant + URI ok) ==")
print("  1. vb <session> --headed + ~/.veil/extension  OR  agent-browser + load-extension")
print("  2. veil agent fill request --item <name> --url '<login URL>'")
print("  3. veil request confirm req-…  (human CLI — same as app)  OR  watch in 2nd terminal")
print("  4. CDP veilAgentic.v1.autofill.fillCredential  (see examples/agentic-autofill/signin.mjs)")
print("  5. Complete TOTP/passkey/CAPTCHA on device; mark pass in Pile")
print()
if gaps:
    print("== Gaps (run as vault owner) ==")
    for line in gaps:
        print(f"  {line}")
    sys.exit(1)
print("Inventory green — start browser proofs.")
PY
