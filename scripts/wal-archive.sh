#!/bin/sh
# veil-wal: stream WAL from the primary over a physical replication slot and
# push every complete segment — plus the in-flight .partial — to R2 through
# backup-ingest.veil.nyc. The slot retains WAL server-side while this service
# is down; max_slot_wal_keep_size on the server caps what a dead archiver can
# pin. A restore needs a base-*.tar.gz plus the wal-* objects newer than its
# start LSN — see docs/backup-restore.md.
set -u

: "${PGDUMP_BASE:?postgresql://user:pass@host:port}"
: "${OFFSITE_TOKEN:?backup-ingest bearer}"

DB="$PGDUMP_BASE/veil"
REPL="$PGDUMP_BASE/postgres?replication=database"
WALDIR="${WAL_DIR:-/wal}"
# Each archiver service owns a pinned slot via WAL_SLOT — a slot is
# single-writer. Primary is `wal_archive` (veil-wal, sfo); a standby runs
# `wal_archive_dr` from another region over private networking. Both stream
# the same WAL bytes; the worker's monotonic .partial rule fences the
# archive so a slower archiver can never overwrite a longer tail.
SLOT="${WAL_SLOT:-wal_archive}"
INGEST="${WAL_INGEST_URL:-https://backup-ingest.veil.nyc}/v1"
INTERVAL="${WAL_PUSH_INTERVAL:-15}"

mkdir -p "$WALDIR"

# Archive namespace: the source cluster's system identifier. A rebuilt
# cluster gets a fresh sysid (timeline 1, LSN 0) so its wal-*/base-* names
# can never collide with — or silently overwrite — the archive of the
# cluster it replaces. Restore enumerates arc-*/ prefixes and picks the
# sysid embedded in the chosen base backup's name. Postgres must be up
# before anything below works, so wait for it here.
SYSID=
while [ -z "$SYSID" ]; do
	SYSID=$(psql "$DB" -Atc "SELECT system_identifier FROM pg_control_system()" 2>/dev/null | tr -cd '0-9')
	[ -z "$SYSID" ] && sleep 3
done
ARC="arc-$SYSID"
echo "wal-archive: slot=$SLOT sysid=$SYSID prefix=$ARC"

push() { # push <path> <object> — 409 is benign ONLY for .partial (a peer's copy is newer)
	rc=$(curl -sS -o /dev/null -w '%{http_code}' -X PUT \
		-H "Authorization: Bearer $OFFSITE_TOKEN" \
		--data-binary "@$1" "$INGEST/$ARC/$2" || echo 0)
	case "$2" in *.partial) [ "$rc" = "409" ] && return 0 ;; esac
	[ "$rc" -ge 200 ] && [ "$rc" -lt 300 ]
}

beat() {
	psql "$DB" -qc "CREATE TABLE IF NOT EXISTS ops_heartbeat(name text primary key, at timestamptz not null);
		INSERT INTO ops_heartbeat(name,at) VALUES('wal-archive',now())
		ON CONFLICT(name) DO UPDATE SET at=now()" >/dev/null 2>&1
}

is_seg() { # exactly 24 uppercase hex chars
	[ ${#1} -eq 24 ] || return 1
	case "$1" in *[!0-9A-F]*) return 1 ;; *) return 0 ;; esac
}

# A slot that overflowed max_slot_wal_keep_size comes back 'lost' — drop it
# so creation below recreates cleanly and streaming resumes from the current
# LSN. WAL generated while the slot was lost is unarchived; the beat gap
# pages it.
slot_status=$(psql "$DB" -Atc \
	"SELECT coalesce(wal_status,'') FROM pg_replication_slots WHERE slot_name='$SLOT'" \
	2>/dev/null || true)
if [ "$slot_status" = "lost" ]; then
	psql "$DB" -qc "SELECT pg_drop_replication_slot('$SLOT')" >/dev/null
fi

# --create-slot is a one-shot: pg_receivewal creates the slot and exits
# rather than streaming, so it cannot live inside the receiver loop.
pg_receivewal -D "$WALDIR" -S "$SLOT" --create-slot --if-not-exists \
	--no-password -d "$REPL" || true

# Receiver: reconnect loop. On restart the slot's confirmed LSN decides where
# streaming resumes, so downtime re-streams whatever was missed.
(
	while :; do
		pg_receivewal -D "$WALDIR" -S "$SLOT" \
			-v --no-password -d "$REPL" 2>&1 | sed 's/^/receivewal: /'
		sleep 5
	done
) &

# Shipper: complete segments, .history timelines, and .backup labels push
# once then delete — the slot retains server-side WAL, so the local copy
# only exists until R2 has it. The open .partial pushes whenever its content
# changed — pg_receivewal preallocates the full 16MB, so size and mtime are
# unreliable change signals; a sha256 per loop catches every flush. On
# restore the newest .partial renames to its segment name.
while :; do
	ok=1
	for f in "$WALDIR"/*; do
		[ -f "$f" ] || continue
		b=${f##*/}
		case "$b" in
			*.partial)
				sum=$(sha256sum "$f" | cut -d' ' -f1)
				mark="$WALDIR/.$b.sum"
				if [ "$(cat "$mark" 2>/dev/null)" != "$sum" ]; then
					push "$f" "wal-$b" && echo "$sum" >"$mark" || ok=0
				fi
				;;
			*.history | *.backup)
				push "$f" "wal-$b" && rm -f "$f" || ok=0
				;;
			*)
				if is_seg "$b"; then
					push "$f" "wal-$b" && rm -f "$f" "$WALDIR/.$b.partial.sum" || ok=0
				fi
				;;
		esac
	done
	[ "$ok" = 1 ] && beat
	sleep "$INTERVAL"
done
