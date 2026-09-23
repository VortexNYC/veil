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
| `VEIL_KEK` | Railway env (`PWM_KEK` via bridge) | Everything is ciphertext |

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
05:17 UTC, custom-format `pg_dump` of both the `veil` and `identity`
databases onto the `veil-backups` volume with 14-day retention. Pull a
dump offsite when it matters:

```bash
railway files --service veil-backup list /backups
railway files --service veil-backup download /backups/veil-YYYY-MM-DD-HHMM.dump
```

Offsite replication (R2/object storage, different account) is the
post-alpha step — until then the volume and the database share a region's
blast radius, which is honest but not maximal.

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

- Kratos / Keto / Hydra state lives in the `identity` database on the
  same shared Postgres — `veil-backup` dumps it alongside `veil` nightly.
- Ransom/drills cadence: run the restore, not just the backup. A backup
  that has never been restored is a hypothesis.
