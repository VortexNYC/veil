# Finish Alchemy on Railway (read this only)

You are **not** blocked on “understanding Alchemy.” You are blocked on **Railway API quota** + **one missing adopt pass**.

## What prod is

| Layer | Truth |
|--------|--------|
| **Live services** | Still on Railway project `veil` — working. |
| **IaC** | `infra/alchemy/` should own shape + secrets. |
| **Gap** | None for adopt — `state/ci/` owns broker + identity Variables. CI loads digests via `escrow-first` (Railway CLI on Actions). |

## Why 429

Every `plan` / `deploy` was doing **6× `railway variable list` + 100+ GraphQL calls**. Migration + retries burned the workspace quota. **Fix:** use escrow for loaders (`VEIL_RAILWAY_PLAN_SOURCE=escrow`), **one** deploy, stop replanning.

## Do this once (when CLI is not rate-limited)

```bash
cd infra/alchemy
unset RAILWAY_API_TOKEN          # use OAuth locally
pnpm run merge:keysvc-escrow     # escrow gets KEYSVC (no Railway call)
pnpm run merge:broker-escrow-gaps # non-secret broker defaults in escrow
export VEIL_RAILWAY_PLAN_SOURCE=escrow
pnpm run sync:broker-keysvc      # optional: copy KEYSVC to Railway veil service
pnpm run deploy:secrets          # ONE adopt — broker Variables + shared tokens
pnpm run export:ci-state
git add state/ci && git commit -m "…"
pnpm run plan                    # must say Plan: no changes
```

If `deploy:secrets` 429s, **wait 15–20 min**, do **not** loop `plan`. Escrow mode still avoids the 6 CLI lists.

## After that

GitHub **`main`** deploy triggers: `pnpm run finish:main-branch` (CLI + `state/ci/`). **Then** think about OVH/PlanetScale — not before.
