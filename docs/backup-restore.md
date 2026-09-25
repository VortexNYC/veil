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

Railway managed Postgres takes platform snapshots; that is the baseline.
For logical backups (point-in-time-independent, portable across instances):

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
  https://backup-ingest.veil.nyc/v1/veil-YYYY-MM-DD-HHMM.dump -o veil.dump
```

`OFFSITE_TOKEN` is `Veil backup-ingest token` in the 1Password Agents
vault and the `INGEST_TOKEN` worker secret. A dump plus its KEK is a
plaintext export — the token only gates the ciphertext; `VEIL_KEK` stays
in its own escrow.

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
  https://backup-ingest.veil.nyc/v1/audit-YYYYMMDD-HHMMSS-<first>-<last>.jsonl
```

## Restoring

Custom-format restore onto a fresh instance:

```bash
pg_restore --clean --if-exists --no-owner --no-privileges \
  -d "postgres://<fresh-dsn>" veil-YYYYMMDD.dump
```

Or a plain-text dump straight through psql. Then boot the origin with the
same `VEIL_KEK`; `EnsurePostgresSchema` is idempotent over restored DDL.

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
