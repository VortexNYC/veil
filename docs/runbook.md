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
| `veil` | Origin — `/veil mcp` on `veil.nyc`. GitHub `main`, redeploy with `--from-source`. |
| `Postgres` | Store of record — `veil`, `kratos`, `keto`, `railway` (hydra). **Image:** `ghcr.io/railwayapp-templates/postgres-ssl` — never deploy the repo Dockerfile here. |
| `glue`, `kratos`, `keto`, `hydra` | Ory identity plane + broker glue. |
| `veil-migrate` | One-shot `/veil migrate` — idempotent schema sync. |
| `veil-sweep` | Hourly cron: expiry cleanup, audit partitions, retention. |
| `veil-backup` | Daily cron (05:17 UTC): `pg_dump` ×4 + `pg_basebackup` → volume + R2 via `backup-ingest.veil.nyc`. |
| `veil-wal` | Always-on WAL archiver (slot `wal_archive`) → R2. |
| `veil-wal-dr` | Standby archiver in `iad` (slot `wal_archive_dr`). |
| `veil-audit-export` | Hourly cron: audit JSONL → R2 (same ingest bearer as backup). |
| `veil-monitor` | Every 15 min: stale heartbeats, `/ready`, ntfy/email. |

Canvas groups in `.railway/railway.ts`: **Identity**, **Store**, **Broker**, **Backup plane**.

## Deploy

Origin and every GitHub-sourced service deploy from **`main`**, not a local
`railway up` checkout (mis-linked CLI has deployed the broker onto **Postgres**
before — treat that as sev-1).

```bash
git checkout main && git pull
sh scripts/railway-redeploy-origin.sh veil   # link + redeploy --from-source
./scripts/deploy-check.sh
railway deployment list --service veil       # SUCCESS before walking away
```

Identity images and backup-plane Dockerfiles: same pattern — always
`railway service link <name>` then `railway redeploy --service <name> --from-source -y`.
**Never** `railway redeploy --service Postgres` from the Veil repo; Postgres
source is the template image (`railway service source connect … postgres-ssl`).

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
- `railway volume files -v veil-backups list /backups` — fresh
  `veil-*.dump`, `kratos-*.dump`, `keto-*.dump`, and `railway-*.dump`
  every day (the container sleeps 10 min post-run; pull inside that
  window). A missing day is an incident, not a nit.

## Backup / restore

`docs/backup-restore.md`. Short version: daily `pg_dump -Fc` of all four
databases (`veil`, `kratos`, `keto`, `railway` for hydra) on a dedicated
volume; restore with `pg_restore` onto a fresh instance and boot origin
with the same `VEIL_KEK` (escrowed — `~/.config/vortex/veil-kek` +
1Password `Veil KEK (escrow)`). A dump next to its KEK is a plaintext
export — keep them apart. Run the restore drill
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

## Customer lifecycle

All verbs are owner-gated human-token calls on the origin (Bearer = a
provisioned human's Hydra id_token, never a session token):

- **Offboard a member**: `DELETE /v1/members/{identity-id}` — drops the
  Keto member tuple and the `humans` row; the token resolves nothing
  from that call on. Refuses owners (demote first) and the caller
  themselves.
- **Promote a member**: `POST /v1/members/{identity-id}/owner`.
- **Demote an owner**: `DELETE /v1/members/{identity-id}/owner` — refuses
  the last owner (an org with no owner is unmanageable).
- **Customer self-delete**: `DELETE /v1/me` — drops member (+owner)
  tuples and the humans row. A sole owner is refused: promote someone or
  delete the org.
- **Org teardown**: `DELETE /v1/org` — deletes every Keto tuple, then
  purges all vault rows (items, grants, sessions, agents, humans,
  org_keys) in one transaction. `audit` rows survive deliberately —
  teardown must not erase the forensic record.

Support console equivalent: the same calls with an owner token via curl
— there is no separate admin API.

## Compromised human (operator kill-switch)

A compromised owner cannot be removed by another human — owners remove
*members*, not owners. The operator path is manual and fail-closed:

1. Freeze the identity: Kratos admin
   `PATCH /admin/identities/{id}/state` → `inactive` (token mint stops).
2. Revoke their agents: `POST /v1/agents/{name}/revoke` under the org
   owner's token — session tokens die at `revoked_at`.
3. Drop their tuples: Keto write `DELETE /relationships` for
   `Organization#members@{id}` and `#owners@{id}` on the org object —
   `RequireOwner` stops honoring them.
4. Purge the humans row: `DELETE FROM humans WHERE id = '{id}'` — token
   resolution fails closed even if Keto is unreachable.
5. Forensics: `audit` rows are append-only — pull the org's rows before
   and after the compromise window. Nothing is deleted.

If the org itself is burned, `DELETE /v1/org` by a surviving owner (or
the Keto tuple wipe + `PurgeOrg` by hand) is the teardown.

## RPO / RTO

- **RPO: 24h.** Daily `pg_dump -Fc` of all four databases at 05:17 UTC
  to the `veil-backups` volume **and** Cloudflare R2 (via the
  `backup-ingest` worker — the copy that survives a Railway-account
  loss); 2-day local retention (the volume is a staging area; artifacts
  over the edge's ~500MB request cap ship as R2 multipart). Worst case
  is one day of audit/grant
  churn — secrets themselves re-wrap on restore.
- **RTO: hours, not days.** The drill (pg_restore into a fresh schema +
  `VEIL_KEK` unwrap + wrong-key fails closed) is proven
  (`TestPostgresBackupRestoreDrill` + the live drill in git history).
  The run is scripted; the slow part is provisioning a replacement
  Postgres.
- **Drill cadence: monthly.** Pull a dump inside the 10-minute
  post-run window (`railway volume files -v veil-backups`), restore to
  the throwaway DB, unwrap one item. A backup that has never been
  restored is a hypothesis.
- **Alerts**: `veil-monitor` emails `VEIL_ALERT_TO` when the backup or
  sweep heartbeat is stale, or when `/ready` fails. A missing beat is
  an incident, not a nit.

## Railway billing

Workspace usage: `railway usage` and `railway usage projects`. Veil prod is
the `veil` project (~$57/mo of the workspace bill). Soft/hard caps:
`railway usage limit status` · `railway usage limit set --target workspace
--soft <usd> --hard <usd>`. Set soft above the monthly estimate so alert
email is signal, not noise — hard caps spend, soft does not.

## Deploy guardrails

- **Origin / GitHub services:** `sh scripts/railway-redeploy-origin.sh <service>`
  (link + `redeploy --from-source`). Not `railway up` from a local checkout.
- **Postgres:** template image only — `ghcr.io/railwayapp-templates/postgres-ssl`.
  Never deploy the Veil repo Dockerfile to the Postgres service.
- **IaC:** `railway config plan` then `railway config apply --yes` when the plan
  matches intent (no surprise destroys).

## Operational secrets rotation

Distinct from `veil key` (data-plane keys — `docs/key-rotation.md`).
The operational secrets to rotate on suspicion or quarterly:

- `VEIL_MAIL_TOKEN` / `VEIL_MAIL_URL` — Resend worker creds. Rotate on
  the worker (`wrangler secret put`), then on `veil`, `veil-monitor`,
  `glue` (`preserve()` vars — set by hand). `veil-monitor` inherits via
  the `${{veil.VEIL_MAIL_TOKEN}}` reference, so only the worker secret
  and the `veil` var need setting. Sends fail during the skew window —
  keep it short; a skew loses a send, never auth.
- `OFFSITE_TOKEN` / `INGEST_TOKEN` — backup-ingest Bearer. Rotate on the
  worker and `veil-backup` together; a skew fails uploads, not dumps.
  Worker secret: `cd apps/backup-ingest && printf '%s' "$TOKEN" | pnpm exec wrangler secret put OFFSITE_TOKEN`.
  Railway holds the same value on `veil-backup`, `veil-wal`, `veil-wal-dr`, and
  `VEIL_AUDIT_INGEST_TOKEN` on `veil-audit-export`. After rotation, smoke:
  `curl -fsS -H "Authorization: Bearer $TOKEN" https://backup-ingest.veil.nyc/healthz`.
- `VEIL_KEYSVC_URL` / `VEIL_KEYSVC_TOKEN` — origin unwrap for org_keys rows
  sealed at `keys.veil.nyc` (0x02 blobs). Pinned in `.railway/railway.ts`;
  production has every org_keys row on the service today. Drop from IaC only
  after `veil key migrate-keysvc` and a verified row census (`get_byte(wrapped,0)=2`
  count zero). See `docs/keysvc-rotation.md`.
- `HYDRA_SYSTEM_SECRET` / `SECRETS_SYSTEM` — `SECRETS_SYSTEM` is a
  comma-separated list: the first entry encrypts new grant material,
  every entry decrypts. Rotate by prepending: `SECRETS_SYSTEM=NEW,OLD`,
  leave `HYDRA_SYSTEM_SECRET=OLD` untouched — under either env-var
  precedence decryption keeps working, so the swap is zero-downtime.
  Remove OLD at the next rotation after old-encrypted grant TTLs expire.
  Escrow the new value: `~/.config/vortex/veil-hydra-system-secret` plus
  the `Veil Hydra system secret (escrow)` Agents-vault item (created
  2026-09-24 — it had no escrow before the drill).
- `VEIL_KEK` — `veil key rotate-kek` (docs/key-rotation.md). Coordinated
  redeploy; mixed-KEK replicas fail closed.

Agent Hydra client secrets (`agent-NAME` clients, `*.hydra` files):
`veil agent hydra` is local-vault only. Against production, run inside
the private network (`railway sandbox --private-network` or any host
that can reach `hydra.railway.internal:4445`) and drive the admin API:
`PUT /admin/clients/agent-NAME` with the canonical client body **and an
explicit `client_secret`** — PUT without one preserves the old secret,
PATCH 500s on hydra v26. Then rewrite the local `*.hydra` file
(`{secret, audience, issuer}`) and remint. Verify both directions:
new secret mints a JWT at `https://id.veil.nyc/oauth2/token`, old
secret returns 401.

Each rotation ends with the same proof: `veil monitor` clean,
`/ready` ok, one `Use` smoke through the origin.

Drill log: 2026-09-24 — all three rotations ran live. Mail token:
worker+service swapped, old 401, real send `{"ok":true}`. `agent-devin`:
explicit-secret PUT, new JWT minted, old 401, `Use` allow/200. System
secret: prepended list, zero downtime, JWKS + mint + Use all green.

## Audit

`audit` is `PARTITION BY RANGE (at)` monthly. `veil-sweep` creates
partitions ~3 months ahead and detaches partitions older than
`--audit-keep` (90d) into standalone `audit_YYYY_MM` tables — archive or
drop them by hand; sweep never drops. `audit_outbox` is the durability
fallback: failed `audit` writes land there and a 2s relay on the audit
pool lands them in `audit` atomically. `ListAudit` unions both, so a
queued event is already visible.

## Quarterly access review

Who can reach what — diff reality against expectation each quarter:

- `SELECT id, org_id FROM humans` — every row should be a known customer.
- Keto: `GET /relationships` on each org — tuples match the humans table.
- Railway: project members = founder only.
- Cloudflare: account members + API tokens = founder + the four workers.
- GitHub: repo collaborators on `VortexNYC/veil`.
- 1Password: `Agents` vault access = the SA + founder.
- Kratos: `kratos list identities` — count matches humans.

Anything unexplained is an S2 until proven otherwise.

## Incidents

`docs/incident-response.md` has the severity ladder, comms template, and
postmortem format. The verbs:

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
