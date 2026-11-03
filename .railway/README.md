# Railway configuration (legacy — frozen)

**Production apply: `infra/alchemy/` only.** `.railway/railway.ts` throws if applied; it is kept so old docs/links resolve, not as IaC.

```txt
infra/alchemy/alchemy.run.ts  ← plan + deploy (`pnpm run deploy:secrets`)
.railway/railway.ts           ← frozen; do not apply
```

Do **not** run `railway config apply` on prod.

---

The notes below describe the legacy SDK workflow if you still need `railway config pull` for diffing.

```bash
pnpm add railway
```

## Common commands (legacy)

Preview what Railway would change:

```bash
railway config plan
```

Apply (avoid on prod during Alchemy migration):

```bash
railway config apply
```

## Notes

- Omitting a service in `.railway/railway.ts` and applying **deletes** it — another reason not to apply during cutover.
- Secrets are `preserve()` here; Alchemy uses `Railway.Variable` + `Redacted.make` / adopt (digest in state only).
- Never `railway up` without `railway service link` on the intended service.
  Postgres must never use the Veil repo Dockerfile — only the postgres-ssl template.
