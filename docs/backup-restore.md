# Backup & restore

The vault is one Postgres database. Everything recoverable lives in it:
items (sealed), agents, grants, humans, sessions, audit, owner_keys, and
org_keys. There is no second store to back up.

## The one non-negotiable

Ciphertext is only as durable as the deployment `VEIL_KEK`. Every item
secret is sealed under a per-org master; every org master is wrapped in
`org_keys` under `VEIL_KEK`. A restored database under a missing or wrong
KEK is a pile of unreadable bytes — the store fails closed, it does not
degrade. `TestPostgresBackupRestoreDrill` proves both halves: same KEK
restores a live vault; different KEK yields nothing.

So the backup plan has exactly two assets:

| Asset | Where it lives | Loss consequence |
|---|---|---|
| Postgres database | Railway Postgres | All vault data |
| `VEIL_KEK` | Railway env | Everything is ciphertext |

Back up both. Keep them apart: a dump next to its KEK is a plaintext
export with extra steps.

## Backing up

Three layers, in increasing tightness: platform snapshots (crash recovery),
nightly logical dumps + daily base backups (durable, portable), and the
continuous WAL archive (point-in-time recovery, RPO seconds — see below).
For logical dumps by hand:

```bash
pg_dump --no-owner --no-privileges --format=custom \
  "postgres://<vault-dsn>" > veil-$(date +%Y%m%d).dump
```

Store the dump somewhere that is not the same failure domain as the
database (object storage, different account). The `VEIL_KEK` value goes
to the secret store of record — see the KEK runbook (VEIL-8) for escrow.

Actual cadence: platform snapshots for crash recovery, plus the
`veil-backup` Railway cron service (`.railway/railway.ts`) — daily at
05:17 UTC, custom-format `pg_dump` of all four real databases — `veil`
(the vault), `kratos` (humans), `keto` (org tuples), and `railway`
(hydra's DSN targets the default `railway` db) — onto the
`veil-backups` volume with 14-day retention. There is no `identity`
database; an earlier config dumped a name that never existed.

The container stays alive for ten minutes after each run so artifacts
can be pulled — `railway volume files` and `railway ssh` only work
while the service is running. Pull during the ~05:17–05:27 UTC window:

```bash
railway volume files -v veil-backups list /backups
railway volume files -v veil-backups download /backups/veil-YYYY-MM-DD-HHMM.dump ./veil.dump
# or any time: railway ssh -s Postgres -- "pg_dump -U postgres -Fc veil" > veil.dump
```

## Offsite copy (R2)

Every run also PUTs each fresh dump to Cloudflare R2 through the
`veil-backup-ingest` worker (`apps/backup-ingest`, route
`backup-ingest.veil.nyc`) into bucket `veil-backups`. This is the copy
that survives a Railway-account loss — the volume and the database share
a region's blast radius, R2 does not.

Pull an offsite artifact any time (no container window needed):

```bash
curl -fsS -H "Authorization: Bearer $OFFSITE_TOKEN" \
  https://backup-ingest.veil.nyc/v1/arc-<sysid>/veil-YYYY-MM-DD-HHMM.dump -o veil.dump
```

`OFFSITE_TOKEN` is `Veil backup-ingest token` in the 1Password Agents
vault and a worker secret of the same name. A dump plus its KEK is a
plaintext export — the token only gates the ciphertext; `VEIL_KEK` stays
in its own escrow.

The worker split tokens by capability: `OFFSITE_TOKEN` (writers + readers)
cannot delete. `DELETE /v1/<name>` requires the separate `DELETE_TOKEN`
secret — held only in escrow, never set on any writer service, so a
stolen upload token cannot erase the archive. `RESTORE_TOKEN` (optional)
can read without write.

All new PUTs must carry the `arc-<sysid>/` prefix — flat names return 400
(GET/HEAD/DELETE still serve the pre-namespace objects). The `.partial`
fence is a compare-and-swap on the R2 etag: a smaller PUT loses the race
atomically (409), and a writer that keeps losing gets a retryable 503 —
a `.partial` can never regress under concurrent archivers.

## WAL archive (PITR — offsite, continuous)

Nightly dumps bound loss to ~24h; that is not acceptable for an auth store.
The `veil-wal` Railway service (always-on, `Dockerfile.wal`,
`scripts/wal-archive.sh`) streams WAL from the primary over the replication
protocol into physical slot `wal_archive` and pushes to the same R2 bucket.
A standby archiver, `veil-wal-dr` (iad, slot `wal_archive_dr`), streams the
same bytes concurrently.

- Every completed 16MB segment lands as
  `arc-<sysid>/wal-<24-hex>`, plus `.history` timelines and
  `.<offset>.backup` labels.
- The in-flight `.partial` is re-pushed whenever its content hash changes
  (~15s loop) — segment names are preallocated so mtime/size don't move;
  content hashing is the only reliable signal. This is what bounds RPO:
  **~15–30s**, not hours. Segments older than the slot start never exist
  in the archive — replay starts at a base backup, not at genesis.

**Namespace fencing.** Everything offsite — WAL, base backups, dumps, audit
exports — lives under `arc-<pg system_identifier>/`. A rebuilt cluster has
a fresh sysid (timeline 1, LSN 0), so a drill or replacement cluster can
never overwrite the archive of the cluster it replaced. Two archivers are
safe because the worker fences writes atomically: a `.partial` PUT smaller
than the stored object gets 409 (the shipper treats it as success — a
peer's longer tail is already archived). The check is an R2 conditional
write on the object's etag, so it holds under concurrent writers, not
just sequential ones. Completed artifacts are immutable: the worker only
puts them if-absent — a same-size re-push is an idempotent retry (200),
a different-size collision is 409 and never overwrites.

Base backups give the PITR start point: `veil-backup` also runs
`pg_basebackup -Ft -z -X stream` daily → `arc-<sysid>/base-YYYY-MM-DD-HHMM.tar.gz`.
The streamed `pg_wal.tar.gz` is discarded — the archive is authoritative.

Runtime config on the Postgres container (set 2026-09-30, persisted on the
volume — pg_hba.conf and postgresql.auto.conf live in PGDATA):

- `host replication all 0.0.0.0/0 scram-sha-256` appended to pg_hba +
  `pg_reload_conf()` — stock images only permit replication on loopback.
- `ALTER SYSTEM SET max_slot_wal_keep_size='256MB'` — caps WAL a dead
  archiver pins. Beyond the cap the slot goes `wal_status='lost'` instead
  of filling the 500MB volume; wal-archive drops and recreates a lost
  slot on boot. The monitor pages on a stale `wal-archive` beat
  (`--wal-stale`, default 10m — beats are written every ~15s push loop).

If the Postgres volume is ever rebuilt from scratch, both must be re-applied;
the beat staleness finding is the tripwire that makes that visible.

## Audit archive (offsite, append-only)

Nightly dumps are the coarse net; the audit trail also exports continuously
through the same worker. The `veil-audit-export` Railway cron runs
`veil audit-export` hourly at :42 — every `audit` row past the
`audit_export_cursor` watermark is batched into `audit-<ts>-<first>-<last>.jsonl`
objects in the same `veil-backups` bucket. The cursor advances only after a
PUT lands; a failed run re-sends the identical object under the identical
name, so the archive is exactly-once by idempotency, not by luck. Audit rows
carry no secret material by design, so this export is safe to hold in the
same bucket as the ciphertext dumps. The sweep detaches `audit` partitions
after `--audit-keep` (90d) — the hourly export always precedes detach, so R2
holds the complete trail even after local retention rolls. The monitor pages
on a stale `audit-export` beat (`--audit-export-stale`, default 3h) or when
the exporter was never seen at all.

Re-read an exported range:

```bash
curl -fsS -H "Authorization: Bearer $OFFSITE_TOKEN" \
  "https://backup-ingest.veil.nyc/v1/arc-<sysid>/audit-YYYYMMDD-HHMMSS-<first>-<last>.jsonl"
```

## Restoring

Custom-format restore onto a fresh instance:

```bash
pg_restore --clean --if-exists --no-owner --no-privileges \
  -d "postgres://<fresh-dsn>" veil-YYYYMMDD.dump
```

Or a plain-text dump straight through psql. Then boot the origin with the
same `VEIL_KEK`; `EnsurePostgresSchema` is idempotent over restored DDL.

### Point-in-time restore (base backup + archived WAL)

To recover to an arbitrary timestamp — not just the last dump:

```bash
# 1. Pick the source cluster. Each archive object lives under
#    arc-<system_identifier>/ — list the prefixes and choose the sysid of
#    the cluster being restored (prod's, not a drill's). LIST pages —
#    loop on `cursor` until it comes back empty:
list_keys() { # list_keys <prefix> — emits every key under a prefix
  cursor=""
  while :; do
    out=$(curl -fsS -H "Authorization: Bearer $OFFSITE_TOKEN" \
      "https://backup-ingest.veil.nyc/v1/?prefix=$1&cursor=$cursor")
    echo "$out" | jq -r '.keys[].key'
    cursor=$(echo "$out" | jq -r '.cursor // empty')
    [ -n "$cursor" ] || break
  done
}
list_keys "arc-" | cut -d/ -f1 | sort -u
#    Then list everything under the chosen one:
list_keys "arc-<SYSID>/"

# 2. Fetch the base backup and every wal-* object in that namespace.
curl -fsS -H "Authorization: Bearer $OFFSITE_TOKEN" \
  "https://backup-ingest.veil.nyc/v1/arc-<SYSID>/base-YYYY-MM-DD-HHMM.tar.gz" -o base.tar.gz

# 3. Extract the cluster.
mkdir data && tar -xzf base.tar.gz -C data && chmod 700 data

# 4. Stage the archive: strip the wal- prefix off each object into a dir.
#    A complete segment beats a .partial of the same name; a .partial with
#    no complete counterpart is renamed to its segment name — that is the
#    live tail, valid WAL up to the last flush before loss.

# 5. Recover. recovery.signal flips the server into archive-recovery mode.
cat >> data/postgresql.auto.conf <<'EOF'
restore_command = 'cp /walarchive/%f %p'
recovery_target_time = 'YYYY-MM-DD HH:MM:SS+00'   # omit for end-of-log
EOF
touch data/recovery.signal
postgres -D data     # or container equivalent; replays, then pauses/promotes
```

`recovery_target_time` stops before the first commit newer than the target
(exclusive bound); omitting it replays to the tail of the newest `.partial` —
RPO is whatever hadn't flushed through the archiver, typically <30s. After
recovery, follow the verify list below (unwrap + item decrypt + wrong-KEK
fail-closed) before declaring it done.

Verify before declaring the restore done — the drill asserts each of
these, do the same by hand:

1. `org_keys` rows unwrap (origin boots without `ErrOrgKeyMismatch`).
2. An item decrypts: a broker `Use` returns the upstream fetch, not a
   secret error.
3. Grants, agents, humans, sessions, and audit rows are present.
4. Org isolation holds — cross-org `Use` still denies.

`TestPostgresBackupRestoreDrill` (`internal/store/backup_test.go`) runs
this whole loop against a live Postgres whenever `PG_TEST_DSN` is set;
`PG_DUMP_CMD` can point at a containerized pg_dump when the host lacks
the binary.

## What this does not cover

- Identity-plane state is three databases on the same shared Postgres —
  `kratos`, `keto`, and `railway` (hydra) — all dumped nightly alongside
  `veil`. Restoring the vault without them orphans humans: identities,
  org membership, and OAuth clients would be gone.
- `VEIL_KEK` escrow (done 2026-09-23): mode-600 copy at
  `~/.config/vortex/veil-kek` on the ops host plus a `Veil KEK (escrow)`
  item in the agents' 1Password vault. Verified by hash + a live unwrap
  drill — a restored prod dump decrypts under the escrowed key and fails
  closed (`crypto: authentication failed`) under a wrong one.
- Ransom/drills cadence: run the restore, not just the backup. A backup
  that has never been restored is a hypothesis. Real-dump drills:
  - 2026-09-23 — all four databases restored, `Secret()` unwrap verified.
  - 2026-09-24 — all four offsite R2 artifacts pulled through the ingest
    worker, restored into scratch databases on the production instance
    (262 items, 32 grants, 1547 audit rows, 2 kratos identities, 5 keto
    tuples, 6 hydra clients), `Secret()` decrypt verified under the
    escrowed KEK, wrong-KEK read fails closed
    (`crypto: authentication failed`). Driven by `cmd/drillrestore`.
  - 2026-09-28 — all four offsite R2 artifacts pulled through the ingest
    worker (`*-2026-09-28-0521.dump`), restored into a scratch Postgres 18
    on the ops host (262 items, 32 grants, 1564 audit rows, 5 agents, 1
    human, 2 kratos identities, 5 keto tuples, 6 hydra clients). `github`
    item decrypts under the escrowed KEK; wrong-KEK read fails closed
    (`crypto: authentication failed`). Audit archive independently
    re-read: `audit-20260928-195601-1571-1575.jsonl` fetched from R2,
    5 rows matching the export cursor (last_id 1575).
  - 2026-09-30 — all four offsite R2 artifacts pulled (`*-2026-09-30-0518.dump`),
    restored into a scratch Postgres 18 container on the ops host (291 items,
    33 grants, 1614 audit rows, 6 agents, 5 humans). `github` item decrypts
    under the escrowed KEK; wrong-KEK read fails closed
    (`crypto: authentication failed`). Audit archive re-read and verified
    contiguous: objects covering ids 1576→1614 all present in R2, and
    `audit-20260929-192042-1608-1614.jsonl` row-for-row identical to the
    restored table.
  - 2026-09-30 — PITR chain proven end-to-end locally before prod rollout:
    `pg_receivewal` + slot `wal_archive` streamed WAL from a scratch pg18,
    `pg_basebackup` base taken, primary hard-killed; recovery with
    `recovery_target_time` between two commits replayed exactly the earlier
    commit and excluded the later one, and end-of-log recovery picked up the
    final in-flight `.partial` (row committed ~4s before the kill survived).
    Live on prod same day: slot active, `wal-*.partial` + first
    `base-*.tar.gz` verified in R2, `wal-archive` beat reporting.

## The drill tool

`cmd/drillrestore` runs the assertion half against any restored database —
it exercises the production unwrap path (`OpenPostgres` → `Secret()`: org
master under the KEK, owner DEK under the master, item blob under the DEK)
and prints only pass/fail, never secret bytes:

```bash
drillrestore -dsn "$PG/drill_veil" -kek /path/to/veil-kek \
  -item <item-id> -org <org-id>            # expect DRILL PASS
drillrestore -dsn "$PG/drill_veil" -kek /path/to/veil-kek \
  -item <item-id> -org <org-id> -wrongkek  # expect DRILL PASS (fail-closed)
```

The KEK file may be 64 hex chars or raw 32 bytes. Run it inside the
database's network (a private-network `railway sandbox` works); the
escrowed KEK transits ssh stdin, never argv or logs.
