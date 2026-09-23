# Operator runbook

Veil production lives in the Railway project `veil`
(`project/92578292-9436-407b-af3e-0f7b19a74289`), environment
`production`. Source of truth for the plane is `.railway/railway.ts` —
change it there, review with `railway config plan`, apply with
`railway config apply`. Do not hand-edit service config in the
dashboard; `preserve()` secrets are the exception — they live only in
the service and must never be re-entered by hand.

| Service | Role |
|---|---|
| `veil` | Origin — `/veil mcp` on `veil.nyc`. Stateless on `VEIL_POSTGRES_DSN`. |
| `Postgres` | Store of record — `veil` and `identity` databases. |
| `glue`, `kratos`, `keto`, `hydra` | Ory identity plane + broker glue. |
| `veil-migrate` | One-shot `/veil migrate` — idempotent schema sync. |
| `veil-sweep` | Hourly cron: expiry cleanup, audit partitions, retention. |
| `veil-backup` | Daily cron: `pg_dump` of `veil` + `identity` to `veil-backups` volume. |

## Deploy

Origin has no repo source — it deploys from a checkout:

```bash
git checkout main && git pull
railway up --service veil --detach -m "<what's shipping>"
railway deployment list --service veil   # verify SUCCESS before walking away
```

`OpenPostgres` runs `EnsurePostgresSchema` on boot — a deploy migrates.
`veil-migrate` exists for running a sync without touching the origin
(redeploy the service to re-run; it is idempotent and mounts the legacy
sqlite volume as the rollback path).

## Health checks that matter

- `GET https://veil.nyc/health` — 200.
- `railway logs --service veil-sweep` — hourly line:
  `sessions=N grants=N approvals=N` then `audit_outbox=empty`.
  `audit_outbox=NNN oldest=MMs WARN` means the audit relay is behind:
  rows are durable in `audit_outbox` and visible via `Audit()`, but check
  `veil` logs for `audit relay` errors and confirm Postgres health.
- `railway files --service veil-backup list /backups` — a fresh
  `veil-*.dump` and `identity-*.dump` every day. A missing day is an
  incident, not a nit.

## Backup / restore

`docs/backup-restore.md`. Short version: daily `pg_dump -Fc` of both
databases on a dedicated volume; restore with `pg_restore` onto a fresh
instance and boot origin with the same `VEIL_KEK`. A dump next to its KEK
is a plaintext export — keep them apart. Run the restore drill
(`PG_TEST_DSN=... go test ./internal/store -run BackupRestoreDrill`)
before trusting the backups.

## Key lifecycle

`docs/key-rotation.md`. The verbs are on the binary:

```bash
veil key rotate-org --org <org> --dsn <dsn>     # rewrap owner DEKs
veil key rotate-kek --new-kek-file <f> --dsn <dsn>  # rewrap org masters
veil key store-recovery --org <o> --owner <id> --material-file <f>
veil key recover-org --org <o> --owner <id> --material-file <f> --dsn <dsn>
```

Secrets via files/env, never argv. KEK rotation requires coordinated
redeploy — mixed-KEK replicas fail closed, they do not degrade.

## Audit

`audit` is `PARTITION BY RANGE (at)` monthly. `veil-sweep` creates
partitions ~3 months ahead and detaches partitions older than
`--audit-keep` (90d) into standalone `audit_YYYY_MM` tables — archive or
drop them by hand; sweep never drops. `audit_outbox` is the durability
fallback: failed `audit` writes land there and a 2s relay on the audit
pool lands them in `audit` atomically. `ListAudit` unions both, so a
queued event is already visible.

## Incidents

- **Origin down**: `railway logs --service veil`, `railway service
  redeploy --service veil` for the last good image.
- **Postgres down**: everything degrades together — the store fails
  closed (no secret leaves unaudited). Fix Postgres, services recover.
- **Restore**: pg_restore + same KEK — the drill is the doc.
- **Lost KEK**: owner recovery wraps via `recover-org` reseed — without
  them, ciphertext is unrecoverable by design.
- **Suspicion of misuse**: `veil audit` per org owner; audit rows are
  append-only and partitioned — detached months are still readable
  tables, not gone.
