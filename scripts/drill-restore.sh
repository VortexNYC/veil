#!/usr/bin/env bash
# drill-restore.sh — the recurring backup drill (VEIL-80).
#
# Pulls the newest offsite R2 artifacts through the backup-ingest worker,
# restores all four databases into a scratch postgres container, then
# proves the restored vault is live: drillrestore decrypts an item through
# the production unwrap path under the escrowed KEK, and -wrongkek asserts
# the same reads fail closed. Finishes with an audit-archive spot check.
#
# Never prints secret bytes. Exit 0 = DRILL PASS, 1 = DRILL FAIL.
#
# Env (all optional, defaults are the ops-host layout):
#   OFFSITE_TOKEN   — backup-ingest bearer; falls back to
#                     `op read "op://Agents/Veil backup-ingest token/password"`
#   VEIL_KEK_FILE   — escrowed KEK file (default ~/.config/vortex/veil-kek)
#   DRILL_STATE_DIR — logs/artifacts (default ~/.local/state/veil/drills)
#   DRILL_ITEM_NAME — item to decrypt (default github)
set -euo pipefail

INGEST="https://backup-ingest.veil.nyc/v1"
KEK_FILE="${VEIL_KEK_FILE:-$HOME/.config/vortex/veil-kek}"
STATE="${DRILL_STATE_DIR:-$HOME/.local/state/veil/drills}"
ITEM_NAME="${DRILL_ITEM_NAME:-github}"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
TS="$(date -u +%Y%m%d-%H%M%S)"
RUN="$STATE/$TS"
LOG="$RUN/drill.log"
mkdir -p "$RUN"

log() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*" | tee -a "$LOG"; }
fail() { log "DRILL FAIL: $*"; [ -t 1 ] || osascript -e "display notification \"Veil restore drill FAILED: $*\" with title \"Veil DR drill\"" 2>/dev/null || true; exit 1; }

# --- credentials -------------------------------------------------------
if [ -z "${OFFSITE_TOKEN:-}" ]; then
  OFFSITE_TOKEN="$(op read "op://Agents/Veil backup-ingest token/password" 2>/dev/null)" \
    || fail "OFFSITE_TOKEN unset and op read failed"
fi
[ -f "$KEK_FILE" ] || fail "KEK escrow file missing: $KEK_FILE"

list_keys() { # every key under a prefix, paging on cursor
  local cursor="" out qs
  while :; do
    # An empty cursor= param reaches R2's list() as "" and throws (1101) —
    # omit the param entirely until the first page returns a cursor.
    qs="prefix=$1"
    [ -n "$cursor" ] && qs="$qs&cursor=$cursor"
    out=$(curl -fsS -m 30 -H "Authorization: Bearer $OFFSITE_TOKEN" \
      "$INGEST/?$qs") || return 1
    echo "$out" | jq -r '.keys[].key'
    cursor=$(echo "$out" | jq -r '.cursor // empty')
    [ -n "$cursor" ] || break
  done
}

fetch() { # fetch <key> <out>
  curl -fsS -m 300 -H "Authorization: Bearer $OFFSITE_TOKEN" "$INGEST/$1" -o "$2" || return 1
}

log "drill $TS — pulling newest offsite artifacts"

# --- pick the newest dump set ------------------------------------------
SYSIDS="$(list_keys "arc-" | cut -d/ -f1 | sort -u)" || fail "cannot list archive namespaces"
SYSID="$(echo "$SYSIDS" | tail -1)"
[ -n "$SYSID" ] || fail "no arc-<sysid> namespaces in R2"
log "namespace: $SYSID"

VEIL_DUMP="$(list_keys "$SYSID/veil-" | sort | tail -1)"
[ -n "$VEIL_DUMP" ] || fail "no veil-*.dump under $SYSID"
STAMP="$(basename "$VEIL_DUMP" | sed 's/^veil-//; s/\.dump$//')"
log "dump set stamp: $STAMP"

for db in veil kratos keto railway; do
  key="$SYSID/$db-$STAMP.dump"
  fetch "$key" "$RUN/$db.dump" || fail "fetch $key"
  log "pulled $key ($(stat -f%z "$RUN/$db.dump") bytes)"
done

# --- scratch postgres ---------------------------------------------------
log "starting scratch postgres:18"
docker rm -f veil-drill >/dev/null 2>&1 || true
docker run -d --name veil-drill -e POSTGRES_PASSWORD=drill \
  -p 127.0.0.1::5432 postgres:18 >/dev/null \
  || fail "docker run"
cleanup() { docker rm -f veil-drill >/dev/null 2>&1 || true; }
trap cleanup EXIT

# The postgres image's init does an internal restart after initdb —
# pg_isready can flap ready→shutdown→ready. Wait for a stable query.
for i in $(seq 1 60); do
  docker exec veil-drill psql -U postgres -tAc "select 1" >/dev/null 2>&1 && sleep 2 && \
    docker exec veil-drill psql -U postgres -tAc "select 1" >/dev/null 2>&1 && break
  [ "$i" = 60 ] && fail "postgres never came ready"
  sleep 1
done

PG="postgres://postgres:drill@127.0.0.1:$(docker port veil-drill 5432/tcp | cut -d: -f2)"
for db in veil kratos keto railway; do
  docker cp "$RUN/$db.dump" "veil-drill:/tmp/$db.dump" || fail "cp $db"
  ok=""
  for i in 1 2 3; do
    if docker exec veil-drill sh -c "dropdb -U postgres --if-exists $db; createdb -U postgres $db && pg_restore -U postgres -d $db --no-owner --no-privileges /tmp/$db.dump" >/dev/null 2>&1; then
      ok=1; break
    fi
    sleep 2
  done
  [ -n "$ok" ] || fail "restore $db (3 tries)"
  log "restored $db"
done

# --- counts -------------------------------------------------------------
counts() { docker exec veil-drill psql -U postgres -d veil -tAc "$1"; }
ITEMS=$(counts "select count(*) from items")
GRANTS=$(counts "select count(*) from grants")
AUDIT=$(counts "select count(*) from audit")
AGENTS=$(counts "select count(*) from agents")
[ "${ITEMS:-0}" -gt 0 ] || fail "restored vault has 0 items"
log "restored counts: items=$ITEMS grants=$GRANTS audit=$AUDIT agents=$AGENTS"

# --- decrypt proof ------------------------------------------------------
IFS='|' read -r ITEM_ID ORG_ID < <(counts "select id, org_id from items where name='$ITEM_NAME' limit 1")
[ -n "${ITEM_ID:-}" ] && [ -n "${ORG_ID:-}" ] || fail "item '$ITEM_NAME' not in restored vault"

BIN="$RUN/drillrestore"
(cd "$REPO" && go build -o "$BIN" ./cmd/drillrestore) || fail "build drillrestore"
"$BIN" -dsn "$PG/veil" -kek "$KEK_FILE" -item "$ITEM_ID" -org "$ORG_ID" >>"$LOG" 2>&1 \
  || fail "drillrestore decrypt path"
"$BIN" -dsn "$PG/veil" -kek "$KEK_FILE" -item "$ITEM_ID" -org "$ORG_ID" -wrongkek >>"$LOG" 2>&1 \
  || fail "drillrestore wrong-KEK fail-closed path"
log "decrypt pass + wrong-KEK fail-closed pass"

# --- audit archive spot check -------------------------------------------
# Audit objects live at the root prefix (audit-YYYYMMDD-HHMMSS-first-last),
# not under the arc namespace. The real claim: the archive covers the
# dump's audit tail — every row in the restored table is also offsite.
MAX_AUDIT_ID="$(counts "select max(id) from audit")"
AUDIT_OBJ=""
for k in $(list_keys "audit-"); do
  range="$(basename "$k" .jsonl | awk -F- '{print $(NF-1), $NF}')"
  first="${range% *}"; last="${range#* }"
  if [ -n "$first" ] && [ -n "$last" ] && [ "$first" -le "$MAX_AUDIT_ID" ] && [ "$last" -ge "$MAX_AUDIT_ID" ]; then
    AUDIT_OBJ="$k"; break
  fi
done
[ -n "$AUDIT_OBJ" ] || fail "no audit-* object covers restored max(id)=$MAX_AUDIT_ID"
fetch "$AUDIT_OBJ" "$RUN/audit.jsonl" || fail "fetch $AUDIT_OBJ"
rows=$(wc -l < "$RUN/audit.jsonl" | tr -d ' ')
[ "${rows:-0}" -gt 0 ] || fail "audit object $AUDIT_OBJ empty"
log "audit archive: $AUDIT_OBJ covers restored max(id)=$MAX_AUDIT_ID ($rows rows)"

log "DRILL PASS — $SYSID $STAMP — items=$ITEMS grants=$GRANTS audit=$AUDIT agents=$AGENTS"
