# Veil production — Alchemy + Railway

Alchemy owns the live `veil` Railway project in code (`alchemy.run.ts` + `src/plane.ts`). Local state: `infra/alchemy/.alchemy/` (gitignored). Auth: Railway OAuth in `~/.alchemy/profiles/default/railway.json` (`pnpm login:railway`). Do **not** set `RAILWAY_API_TOKEN` locally when using OAuth.

**Dual apply (until cutover):** `.railway/railway.ts` remains the env/domain-heavy apply path for secrets and `preserve()` bulk. Alchemy manages service shape, groups, custom domains, volumes, and the non-secret env literals that match the IaC file. Flip CI to `alchemy deploy` only when you retire `railway config apply`.

```bash
cd infra/alchemy
pnpm install
unset RAILWAY_API_TOKEN
pnpm plan                 # expect "Plan: no changes" when synced
pnpm run deploy           # apply when plan shows intentional diffs
pnpm run seed:volumes     # one-time adopt helper if volume state lacks instanceId
pnpm run login:railway    # refresh OAuth
```

First adopt on a fresh machine:

1. `pnpm exec alchemy deploy --adopt -y --no-input --include '*RpcToken'`
2. `pnpm run deploy --adopt` (full stack)
3. If volumes plan as `create`, run `pnpm run seed:volumes` then `pnpm run deploy` again.

CI (`.github/workflows/railway-alchemy.yml`) requires GitHub secret **`RAILWAY_API_TOKEN`** (Railway account API token). One-time setup from repo root:

```bash
pnpm --filter @veil/railway-alchemy run secret:github
```

Or create a token at [railway.com/account/tokens](https://railway.com/account/tokens) and `gh secret set RAILWAY_API_TOKEN -R VortexNYC/veil`.

Committed snapshot: `state/ci/` (no rpc material in `attr`). After local deploy drift, refresh with `pnpm run export:ci-state` and commit. CI uses `ALCHEMY_STAGE=ci`.
