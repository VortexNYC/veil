# Postgres credential drift — incident runbook

Oct 21–22 2026: Railway security incident → creds rotated → `veil`, `keto`,
`kratos` died on `FATAL: password authentication failed (28P01)` while every
env var looked correct. This is the fix path for next time.

## The trap

Railway env vars are injected at **deploy time**. Rotating a variable does
nothing to running containers — they keep the old value until redeploy. During
the incident we rotated `POSTGRES_PASSWORD` in the DB (so the *server* expects
the new password) while deploys were platform-paused, so running services kept
auth'ing with the **old** password → SASL fail on every fresh connection.

Symptom shape: `psql` with the *env-var* password works fine from inside the
network (server has new pw), but services fail (they hold old pw). Same host,
same user, same port — different result by client.

## Verify

```bash
# which services are broken on fresh auth
railway logs --service veil | grep "SASL auth"

# does the env-var password actually work on the server?
railway ssh --service veil-wal "PGPASSWORD=<var-pw> psql postgresql://postgres@postgres.railway.internal:5432/postgres -tAc 'select 1'"
```

`veil-wal` / `veil-wal-dr` have shells + psql (`veil`, `kratos`, `glue` are
distroless — `railway ssh` there returns "container does not have a shell").
Kratos admin (port 4434) was NOT reachable from veil-wal even on the private
network — DB psql is the reliable path.

## Recover the OLD password the stale containers hold

Their deploy-time env is only visible inside a running container of the same
generation. In order:

1. `railway ssh --service <any pg-tool service> 'env | grep -i dsn'` —
   `veil-wal-dr` held the old `PGDUMP_BASE` and that gave the old password.
2. 1Password `Agents` vault — "Veil prod env (escrow)" items.
3. `~/.config/vortex/veil-env-escrow.json` + `~/.config/vortex/comp/db-*.url`.
4. Railway dashboard variable history / `auditLogs` (2-day retention on Hobby).

## Fix without a redeploy

```bash
railway ssh --service veil-wal "PGPASSWORD=<current-server-pw> psql \
  postgresql://postgres@postgres.railway.internal:5432/postgres \
  -c \"ALTER USER postgres WITH PASSWORD '<old-pw>'\""
```

Server then matches the stale envs → services heal on next reconnect (~15s).

## After healing — reconcile forward

The server and the env vars now disagree **inverted**. Before the next
deploy lands, either:

- update every `*_DSN`/`PGDUMP_BASE`/`POSTGRES_PASSWORD` var to the old
  password (staged, applies on next deploy), **or**
- `ALTER USER` forward to the var value *as part of* the redeploy.

Do NOT leave it mismatched — the next deploy re-breaks auth silently.

## Root fixes filed

- VTX-632: hardcoded DSN values → `${{Postgres.DATABASE_URL}}` reference vars
  so rotations propagate with the service env, not by hand.
- `nyc.veil.watchdog` (LaunchAgent, 5min) pages via osascript on
  `veil.nyc/ready` — it's the "random veil notification" that fired all day.
  `launchctl bootout gui/$(id -u)/nyc.veil.watchdog` to mute,
  `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/nyc.veil.watchdog.plist`
  to re-enable.

## Gotchas learned

- `railway ssh` needs a service with bash/sh; pick pg-tool images.
- `railway restart`/`serviceInstanceRedeploy` are BOTH gated when "Deploys
  have been paused" — there is no restart-only escape hatch.
- Hobby keeps 2 days of `auditLogs`; deploy timestamps per service live in
  `railway deployment list --service <name> --json` — compare them to see who
  carries stale env.
