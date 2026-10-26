#!/bin/sh
# veil-backup: daily pg_dump of every real database plus a pg_basebackup,
# staged on the /backups volume and PUT to R2 through backup-ingest.veil.nyc.
#
# The identity plane is three databases, not one: kratos (humans), keto
# (org tuples), and `railway` (hydra — its DSN targets the default db).
# Losing them orphans humans even with a perfect vault restore.
#
# The 2 GB /backups volume is a staging area — R2 already holds every prior
# run offsite, so the volume only needs headroom for THIS run (~1.2 GB
# worst case). Reclaim runs BEFORE dumping, not after: stale base/ dir,
# artifacts older than 2 days, then escalating if the volume stays >50%
# full. A same-day pileup only happens when uploads fail (crashed run
# leaves its bytes local) — tier 3 drops every prior artifact; a fresh
# run's output replaces it.
#
# Big artifacts (kratos/base tars crossed the edge's ~500MB single-request
# body cap in Oct 2026) ship as R2 multipart parts via the worker's
# ?mpu= routes — same names, same bearer, parts reassembled server-side.
#
# WARNING: every name this PUTs must match the worker's NAME allowlist
# (apps/backup-ingest/src/index.ts) — an unlisted suffix 404s, rc=1 kills
# the run before the heartbeat, and the monitor pages a silent cron. The
# test at apps/backup-ingest/test/worker.test.ts enumerates the accepted
# shapes; add yours there too.
set -u

: "${PGDUMP_BASE:?postgresql://user:pass@host:port}"
: "${OFFSITE_TOKEN:?backup-ingest bearer}"

INGEST="${BACKUP_INGEST_URL:-https://backup-ingest.veil.nyc}/v1"
DBS="veil kratos keto railway"
# Objects bigger than this go multipart — the Cloudflare edge refuses a
# single request body past ~500MB (kratos-*.dump hit 522MB on 2026-10-26).
MPU_MIN=$((400 * 1024 * 1024))
MPU_PART=96m # busybox split suffix; under the cap, over R2's 5MB floor

usepct() { # percent-used of the /backups mount
	set -- $(df -P /backups | tail -n 1)
	printf '%s' "${5%%%}"
}

reclaim() { # reclaim <mtime-expr> — delete matching artifacts
	find /backups \( -name "*.dump" -o -name "base-*.tar.gz" -o -name "base-*.manifest" \) "$@" -delete
}

rc=0
STAMP="$(date +%F-%H%M)"

# --- pre-run reclaim --------------------------------------------------
rm -rf /backups/base
reclaim -mtime +2
if [ "$(usepct)" -gt 50 ] 2>/dev/null; then
	echo "backup: /backups >50% after 2d pass — keeping only the last day"
	reclaim -mtime +0
fi
if [ "$(usepct)" -gt 50 ] 2>/dev/null; then
	echo "backup: /backups still >50% — dropping all prior artifacts; this run replaces them"
	reclaim
fi

# --- sysid namespace ---------------------------------------------------
SYSID=$(psql "$PGDUMP_BASE/veil" -Atc "SELECT system_identifier FROM pg_control_system()" | tr -cd '0-9')
if [ -z "$SYSID" ]; then
	echo "no system_identifier — cannot namespace archive"
	rc=1
fi
ARC="arc-$SYSID"

# --- dumps -------------------------------------------------------------
for d in $DBS; do
	pg_dump "$PGDUMP_BASE/$d" -Fc -f "/backups/$d-$STAMP.dump" || rc=1
done
if pg_basebackup -D /backups/base -Ft -z -X stream -d "$PGDUMP_BASE/postgres?replication=database"; then
	mv /backups/base/base.tar.gz "/backups/base-$STAMP.tar.gz"
	mv /backups/base/backup_manifest "/backups/base-$STAMP.manifest"
	rm -f /backups/base/pg_wal.tar.gz
else
	rc=1
fi

# --- offsite push ------------------------------------------------------
put() { # put <path> <object> — single PUT for small artifacts
	curl -fsS -X PUT -H "Authorization: Bearer $OFFSITE_TOKEN" \
		--data-binary "@$1" "$INGEST/$ARC/$2"
}

mpu_abort() { # best-effort — the orphan expires server-side regardless
	curl -fsS -X POST -H "Authorization: Bearer $OFFSITE_TOKEN" \
		-H "X-Mpu-Id: $1" "$INGEST/$ARC/$2?mpu=abort" >/dev/null 2>&1 || true
}

mpu_put() { # mpu_put <path> <object> — multipart for >MPU_MIN artifacts
	f=$1
	obj=$2
	upid=$(curl -fsS -X POST -H "Authorization: Bearer $OFFSITE_TOKEN" \
		"$INGEST/$ARC/$obj?mpu=create" | sed -n 's/.*"uploadId":"\([^"]*\)".*/\1/p') || return 1
	if [ -z "$upid" ]; then
		echo "backup: mpu create returned no uploadId for $obj"
		return 1
	fi
	rm -f /tmp/mpart-*
	split -b "$MPU_PART" "$f" /tmp/mpart- || { mpu_abort "$upid" "$obj"; return 1; }
	parts=""
	i=0
	for p in /tmp/mpart-*; do
		i=$((i + 1))
		etag=$(curl -fsS -X PUT -H "Authorization: Bearer $OFFSITE_TOKEN" \
			-H "X-Mpu-Id: $upid" --data-binary "@$p" \
			"$INGEST/$ARC/$obj?partNumber=$i" | sed -n 's/.*"etag":"\([^"]*\)".*/\1/p') || {
			mpu_abort "$upid" "$obj"
			return 1
		}
		if [ -z "$etag" ]; then
			echo "backup: mpu part $i returned no etag for $obj"
			mpu_abort "$upid" "$obj"
			return 1
		fi
		rm -f "$p"
		parts="$parts{\"partNumber\":$i,\"etag\":\"$etag\"},"
	done
	curl -fsS -X POST -H "Authorization: Bearer $OFFSITE_TOKEN" \
		-H "X-Mpu-Id: $upid" -H "Content-Type: application/json" \
		-d "{\"parts\":[${parts%,}]}" "$INGEST/$ARC/$obj?mpu=complete"
}

if [ "$rc" -eq 0 ]; then
	for f in /backups/*-"$STAMP".*; do
		[ -f "$f" ] || continue
		sz=$(wc -c <"$f")
		if [ "$sz" -gt "$MPU_MIN" ]; then
			echo "backup: $(basename "$f") is ${sz}B — multipart push"
			mpu_put "$f" "$(basename "$f")" || rc=1
		else
			put "$f" "$(basename "$f")" || rc=1
		fi
	done
fi

# --- post-run retention + heartbeat ------------------------------------
find /backups \( -name "*.dump" -o -name "base-*.tar.gz" -o -name "base-*.manifest" \) -mtime +2 -delete
if [ "$rc" -eq 0 ]; then
	psql "$PGDUMP_BASE/veil" -qc "CREATE TABLE IF NOT EXISTS ops_heartbeat(name text primary key, at timestamptz not null); INSERT INTO ops_heartbeat(name,at) VALUES('backup',now()) ON CONFLICT(name) DO UPDATE SET at=now();" || rc=1
fi
# The trailing sleep leaves a daily window where the container is alive
# for `railway ssh`/`railway volume files` pulls — the SFTP bridge only
# works while the service is running.
sleep 600
exit "$rc"
