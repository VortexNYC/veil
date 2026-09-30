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

## Drill record — 2026-09-30 (VEIL-73)

**RTO: 30 minutes** wall-clock from empty Railway project to the full
stack serving on restored data (23:01 → 23:31 UTC). Vault decrypt proven
at the ~18-minute mark. RPO this run: dumps only (17h stale — drill used
the 05:18 dump at ~23:12; a real event would replay WAL to ~seconds).

Executed end-to-end on project `veil-drill` (deleted after): fresh
`railway init` → `config apply` → per-service secrets from escrow →
dumps pulled from R2 via the LIST endpoint → `pg_restore` over
`railway ssh` → all services green → binary `drillrestore` pushed over
ssh → PASS + fail-closed PASS → drill hydra serves prod-identical JWKS.

Verified counts: 291 items · 33 grants · 1614 audit · 6 agents ·
4 kratos identities · 6 hydra clients · 9 keto tuples.

### Findings the drill surfaced (all real, some fixed same-night)

1. **Escrowed `SECRETS_SYSTEM` was truncated** — escrow held 48 chars,
   prod stores a 97-char rotation list (new+old). Drill hydra could not
   decrypt restored JWKS → `.well-known` 500 → no token minting.
   FIXED: escrow file + 1Password item now hold the full list; drill
   recovered and serves kids `f3324aac`/`e06a78ef` identical to prod.
   *Lesson: escrow verification must be length/hash-compared against
   live prod vars, not just "a file exists."*
2. **Escrow coverage gap** — kratos `SECRETS_CIPHER`/`SECRETS_COOKIE`,
   `COURIER_*`, `RESEND_API_KEY`, hydra `OIDC_*` salt, glue `BOOTSTRAP_*`,
   and the `VEIL_VORTEX_*` billing vars were NOT escrowed anywhere.
   FIXED: full per-service env snapshot (235 vars, 11 services) now lives
   at `~/.config/vortex/veil-env-escrow.json` (mode 600) and 1Password
   `Veil prod env (escrow)` in Agents. Re-snapshot when vars change —
   stale escrow is the finding this section exists to prevent.
3. **`config apply` is all-or-nothing on volume size** — fresh Postgres
   provisions a 5GB volume; IaC's `sizeMB: 500` is a forbidden shrink
   that failed the ENTIRE change-set (24 opaque "change" errors — the
   real reason sits in `--json` diagnostics only). FIXED: IaC now
   declares `sizeMB: 5000` (matches fresh provision, grew prod too).
4. **IaC didn't carry repo sources** — the file relied on imported
   state; fresh services were created sourceless and failed silently.
   FIXED: `source: github("VortexNYC/veil", {branch:"main"})` now
   declared on all 10 Dockerfile-built services — the file is
   self-bootstrapping on an empty project.
5. **Custom domains can't be IaC-registered** — expected; the runbook's
   DNS step (Cloudflare re-point) is the real path.
6. **WAL namespace collision risk** — a second cluster streaming to the
   same bucket writes `wal-` segments on the same timeline-1 naming,
   colliding with prod's archive. Drill's veil-wal never connected
   (pg_hba correctly denied), so nothing pushed — but VEIL-74's fencing
   design must namespace per cluster or make objects immutable.
7. **pg_hba fails closed correctly** — replication denied by default on
   the fresh cluster; the documented `host replication` append +
   `pg_reload_conf()` worked without restart.
8. **Agent client secrets rotate** — local `*.hydra` files go stale vs
   restored client hashes; mint a fresh client via hydra admin during DR
   instead of trusting local files.
9. **Kratos rejects `SECRETS_CIPHER` > 32 chars** and courier auth is
   `api_key` type — the exact var shapes are now in the table below.
10. **`railway ssh` needs a local ssh-agent or `SSH_AUTH_SOCK=""`** —
    agent socket flakiness kills sessions mid-command; bypass works.

### Required secret inventory (rebuild-from-escrow checklist)

| Service | Vars that MUST come from escrow | Vars that can be regenerated |
|---|---|---|
| veil | `VEIL_KEK`, `VEIL_MASTER_KEY`, `VEIL_MAIL_TOKEN` | `VEIL_*` URLs, billing placeholders, pool sizes |
| hydra | `SECRETS_SYSTEM` (FULL LIST — comma-separated) | `OIDC_*` salt, `URLS_*`, `SERVE_*` |
| kratos | *(should escrow)* `SECRETS_CIPHER` ≤32ch, `SECRETS_COOKIE` ≤32ch | `COURIER_*`, `RESEND_*`, `PORT` |
| keto | — | `DSN` only |
| glue | — | `BOOTSTRAP_*`, service URLs |
| veil-wal / veil-backup / audit-export | `OFFSITE_TOKEN` | `PGDUMP_BASE` (templated) |
| veil-monitor | — | `VEIL_ALERT_TO`, `VEIL_READY_URL` |

Without the Must column the restore is ciphertext-only; without the
regen column things boot degraded — sessions die, mail is quiet.
