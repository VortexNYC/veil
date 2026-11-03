# Alchemy migration from `.railway/railway.ts`

**Status: complete (2026-11-03).** Production apply path is `infra/alchemy/` only.

## Phase A — shape

- [x] Full service / domain / volume / group plane in `src/plane.ts`
- [x] `Railway.PrivateNetwork` + `PostgresPrivateDns` (`src/network.ts`) — deployed; `state/ci/` updated
- [x] Do not `railway config apply` on prod (`.railway/railway.ts` throws if applied)
- [x] `branch: main` on GitHub deploy triggers — `pnpm run finish:main-branch` (Railway CLI `service source connect`); Alchemy GraphQL still errors on trigger create; `state/ci/` carries `branch: main`; CI sets `VEIL_RAILWAY_REPO_BRANCH=main` for plan

## Phase B — secrets (`preserve()` → `Railway.Variable`)

Manifest: `src/preserve-manifest.ts` (empty) · list: `pnpm run manifest:list`

**Ops:** `node scripts/restore-identity-env-from-escrow.mjs` if a deploy empties service vars (never prints values).

**Rule:** `Railway.Variable` with wrong/empty `Redacted.make` **wipes** live vars. Service-scoped secrets are **Variable only** — no `${{LogicalId.KEY}}` on `Service.env`.

- [x] Shared offsite + audit-ingest + monitor + identity + broker (`src/variables/*`, loaders, `state/ci/` digests)

## Phase C — retire handrolled IaC

- [x] Broker Variable adopt + `state/ci/` (133 resources; legacy `RESEND_*` removed)
- [x] `.railway/railway.ts` frozen (throws on apply)
- [x] Runbook + `.railway/README.md` → Alchemy apply path
- [x] CI `railway-alchemy` plan bar with `VEIL_RAILWAY_PLAN_SOURCE=escrow-first` (Railway CLI on Actions; local deploy stays `escrow` strict)

### Apply / CI

```bash
cd infra/alchemy
export VEIL_RAILWAY_PLAN_SOURCE=escrow   # local plan/deploy: skip railway variable list
pnpm run deploy:secrets                  # Veil offsite token + adopt
pnpm run export:ci-state                 # from live stage → state/ci/ (scrubs rpcToken only)
```

`export-ci-state.mjs` keeps each `Railway.Variable` **props** digest as plan truth (does not overwrite from stale `attr.digest`).

### Optional: GitHub `main` branch on services

Live triggers (Railway CLI — works with your login session):

```bash
pnpm run finish:main-branch   # connect all repo services + sync state/ci
```

Or only IaC snapshot after you wired branch elsewhere:

```bash
pnpm run sync:branch-state
```

`VEIL_RAILWAY_REPO_BRANCH=main pnpm run deploy:secrets` still fails on Railway GraphQL
(`Cannot create deployment trigger … no one in the project has access`) — do not use for branch.
CI plan sets `VEIL_RAILWAY_REPO_BRANCH=main` in `.github/workflows/railway-alchemy.yml`.

### Identity mail (Cloudflare, not Resend)

Kratos courier is HTTP → `mail.veil.nyc` (`COURIER_HTTP_*` + shared `VEIL_MAIL_TOKEN`). Legacy `RESEND_*` Railway vars are **not** in IaC.

```bash
railway variable delete RESEND_API_KEY --service kratos      # if still present
railway variable delete RESEND_AUTHORIZATION --service kratos
pnpm run strip:kratos-resend-escrow   # local ~/.config/vortex/veil-env-escrow.json
```
