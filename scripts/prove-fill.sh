#!/usr/bin/env bash
# Origin fill proof. Human JWT + POST /v1/fill/logins. Never prints secrets.
set -euo pipefail

ORIGIN="${VEIL_ORIGIN:-https://veil.nyc}"
TOKEN_FILE="${VEIL_HUMAN_TOKEN_FILE:-$HOME/.config/veil/human.jwt}"
URL="${VEIL_FILL_URL:-https://example.com/}"

if [[ ! -s "$TOKEN_FILE" ]]; then
  echo "missing VEIL_HUMAN_TOKEN_FILE" >&2
  exit 1
fi

echo "== origin $ORIGIN fill $URL"
code="$(python3 - "$ORIGIN" "$TOKEN_FILE" "$URL" <<'PY'
import json, os, sys, urllib.error, urllib.request
origin, token_file, url = sys.argv[1], sys.argv[2], sys.argv[3]
tok = open(token_file).read().strip()
req = urllib.request.Request(
    origin.rstrip("/") + "/v1/fill/logins",
    data=json.dumps({"url": url}).encode(),
    headers={"Authorization": "Bearer " + tok, "Content-Type": "application/json"},
    method="POST",
)
try:
    with urllib.request.urlopen(req, timeout=20) as res:
        raw = res.read()
        status = res.status
except urllib.error.HTTPError as e:
    print(e.code)
    sys.exit(1)
data = json.loads(raw)
entries = data.get("entries") or []
print(status)
print("entries", len(entries))
for e in entries:
    print("name", e.get("name"), "login", e.get("login"), "password_len", len(e.get("password") or ""))
if status != 200 or not entries:
    sys.exit(1)
PY
)"

echo "$code"
echo "== fill origin ok"
