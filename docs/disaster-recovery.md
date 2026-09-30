# Disaster recovery

Total loss of the Railway account or the sfo region: every durable asset
is in Cloudflare R2 (`veil-backups` bucket) plus two escrows — `VEIL_KEK`
(`~/.config/vortex/veil-kek`, 1Password `Veil KEK (escrow)`) and the
`backup-ingest` bearer (1Password `Veil backup-ingest token`). R2 objects
are append-only ciphertext; the KEK is the last secret standing — guard it
harder than the dumps.

This runbook rebuilds production from only those assets. It is the script
the DR fire drill (VEIL-73) times; keep it honest — a step that has never
been executed is a hypothesis, not a plan.

## Order of operations

Restore in dependency order: Postgres first (everything mounts it), then
identity plane, then the broker, then DNS.

1. **Create the new stack.** `railway link` a fresh project (new region is
   fine) and `railway config apply --yes` — `railway.ts` provisions
   Postgres, kratos, keto, glue, hydra, veil, and every cron/volume as
   declared. Preserve() secrets do NOT carry between projects: re-enter
   `VEIL_KEK`, `VEIL_MASTER_KEY`, `VEIL_POSTGRES_DSN` deps, Hydra
   `SECRETS_SYSTEM`/`OIDC_*` salts, Kratos `SECRETS_*`, `OFFSITE_TOKEN`,
   `VEIL_MAIL_*`, billing vars — the full preserve() list in railway.ts.
   Secrets live in 1Password `Agents`/`Dev Tools` + `~/.config/vortex/`.

2. **Re-arm WAL archiving on the new Postgres** (it does not carry over —
   pg_hba and postgresql.auto.conf live in the OLD cluster's PGDATA):
   ```
   railway ssh -s Postgres -- "echo 'host replication all 0.0.0.0/0 scram-sha-256' \
     >> /var/lib/postgresql/data/pgdata/pg_hba.conf && \
     psql -U postgres -qc 'SELECT pg_reload_conf()' \
     -qc \"ALTER SYSTEM SET max_slot_wal_keep_size='256MB'\" \
     -qc 'SELECT pg_reload_conf()'"
   ```

3. **Restore the databases.** Either path works; PITR is tighter.

   **A. Latest dumps (simplest, loses up to 24h):**
   ```
   for d in veil kratos keto railway; do
     curl -fsS -H "Authorization: Bearer $OFFSITE_TOKEN" \
       https://backup-ingest.veil.nyc/v1/$d-<latest>.dump -o $d.dump
     pg_restore --no-owner --no-privileges -d "$PG/$d" $d.dump
   done
   ```

   **B. PITR (loses ~15–30s):** fetch `base-*.tar.gz` + all `wal-*` objects
   (`GET /v1/?prefix=wal-` lists them — VEIL-75), extract base into PGDATA,
   stage WAL with the wal- prefix stripped (a `.partial` with no complete
   counterpart renames to its segment name), set `restore_command =
   'cp /walarchive/%f %p'` + optional `recovery_target_time`, touch
   `recovery.signal`, start postgres. Details in docs/backup-restore.md.

4. **Identity plane order.** Kratos and Keto have `preDeploy` migrations —
   apply runs them; restoring their dumps into the fresh cluster must happen
   BEFORE the migrated services roll too far, or their schemas fight.
   Sequence: create cluster → restore dumps → then let services boot.

5. **Verify before traffic:**
   - `drillrestore -dsn $PG/veil -kek ~/.config/vortex/veil-kek \
      -item github -org aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa` → DRILL PASS
   - same with `-wrongkek` → DRILL PASS (fail-closed)
   - item/grant/agent counts match the pre-loss inventory (291/33/6 as of
     2026-09-30)
   - `GET /ready` → `ok`, `audit_spool=0`

6. **DNS.** Re-point `veil.nyc`, `id.veil.nyc`, `accounts.veil.nyc`,
   `consent.veil.nyc`, `login.veil.nyc`, `app.veil.nyc` to the new Railway
   domains (Cloudflare DNS, proxied — `wrangler`/dashboard). Clients fail
   closed during the gap: local vaults keep serving `--inject` mode.

## What is NOT covered

- In-flight writes between the last `.partial` flush and the loss event
  (~15–30s worst case) — the RPO floor.
- `ops_heartbeat` rows older than the restore point — noise, not data.
- Railway volume contents (pwm-volume sqlite shadow — superseded by
  Postgres; keep it mounted as rollback history only).
- Anything only in the founder's head — that is why VEIL-72 exists.
