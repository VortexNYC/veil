# Veil production — Alchemy + Railway

**Stuck or rate-limited?** Read [`FINISH-RAILWAY.md`](./FINISH-RAILWAY.md) first — one page, one command (`pnpm run finish:broker`).

Alchemy owns the live `veil` Railway project in code (`alchemy.run.ts` + `src/plane.ts`). Local state: `infra/alchemy/.alchemy/` (gitignored). Auth: Railway OAuth in `~/.alchemy/profiles/default/railway.json` (`pnpm login:railway`). Do **not** set `RAILWAY_API_TOKEN` locally when using OAuth.

**Apply path:** `infra/alchemy/` is production infra. Use `pnpm plan` / `pnpm run deploy` with Railway OAuth locally. Do **not** `railway config apply` for shape or secrets once a slice is owned here.

**Legacy:** `.railway/railway.ts` is **frozen** (throws on `railway config apply`). All secrets are `Railway.Variable` in Alchemy state.

```bash
cd infra/alchemy
pnpm install
unset RAILWAY_API_TOKEN
pnpm plan                 # expect "Plan: no changes" when synced
pnpm run deploy           # apply when plan shows intentional diffs
pnpm run seed:volumes     # one-time adopt helper if volume state lacks instanceId
pnpm run login:railway    # refresh OAuth
pnpm run manifest:list    # preserve() → Variable migration checklist
```

Repo **`branch: main`**: live triggers via `pnpm run finish:main-branch` (Railway CLI `service source connect`). Committed `state/ci/` includes `branch: main`; CI sets `VEIL_RAILWAY_REPO_BRANCH=main` for plan. Do not rely on Alchemy deploy for branch (Railway GraphQL trigger error); account API tokens still cannot create triggers on apply.

**Identity mail:** Cloudflare Email at `mail.veil.nyc` — not Resend. Kratos uses `COURIER_HTTP_*` only; drop stale `RESEND_*` on Railway and run `pnpm run strip:kratos-resend-escrow` after refreshing escrow snapshots.

After adding resources (network, variables, …): `pnpm run deploy --adopt`, then `pnpm run export:ci-state` and commit `state/ci/`.

First adopt on a fresh machine:

1. `pnpm exec alchemy deploy --adopt -y --no-input --include '*RpcToken'`
2. `pnpm run deploy --adopt` (full stack)
3. If volumes plan as `create`, run `pnpm run seed:volumes` then `pnpm run deploy` again.

CI (`.github/workflows/railway-alchemy.yml`) requires GitHub secret **`RAILWAY_API_TOKEN`**. The canonical token lives in Veil as item **`railway-alchemy-ci`** (`https://backboard.railway.com`, tags `infra`, `alchemy`); the GitHub secret is a mirror for Actions only.

**Rotation:** update the Veil item, then sync GitHub (human JWT + remint files as for any origin admin):

```bash
pnpm --filter @veil/railway-alchemy run secret:github
```

The script reads the item via `veil item reveal railway-alchemy-ci` unless `RAILWAY_API_TOKEN` is already exported. Override item name with `VEIL_RAILWAY_CI_ITEM`. Fallbacks: Railway `apiTokenCreate`, then Alchemy OAuth profile.

**Shared `OFFSITE_TOKEN`:** deploy with `pnpm run deploy:secrets` (Veil item `Veil backup-ingest token`). CI plan needs `VEIL_OFFSITE_TOKEN` and `VEIL_AUDIT_INGEST_TOKEN` on GitHub — `pnpm run secret:github-offsite` and `secret:github-audit-ingest` (same item value).

**Broker `VEIL_KEYSVC_TOKEN`:** if Railway `veil` service omits it, `load-broker-env.mjs` reveals Veil item **`veil-key-service-token`** (override `VEIL_KEYSVC_ITEM`). Add the live value to escrow when rotating. One-shot Railway sync: `pnpm run sync:broker-keysvc`. CI mirror: `pnpm run secret:github-keysvc`.

Manual: [railway.com/account/tokens](https://railway.com/account/tokens) → `gh secret set RAILWAY_API_TOKEN -R VortexNYC/veil`.

Committed snapshot: `state/ci/` (no rpc material in `attr`). After local deploy drift, refresh with `pnpm run export:ci-state` and commit. CI uses `ALCHEMY_STAGE=ci`.
