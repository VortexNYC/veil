# Veil

Agent-first credential broker. Agents reference an item. Something outside the model injects the secret. The model never holds it.

The product is Veil. This repo and the CLI stay `veil` in the VeilNYC org for now.

Open source under Veil NYC. Not a 1Password clone and not an auth company.

**Ory \*** — the identity vendor, not us. **Kratos** = humans (email, login, invite). **Hydra** = tokens. **Keto** = org owner/member. **Glue** talks to those three. **This repo** is the broker (secrets, grants, inject). Cheatsheet: `AGENTS.md` and `docs/SPEC.md`.

## What this is

Two principals (human, agent). Two grant levels:

- **Level 1** — agent does the work; a human does the last unlock.
- **Level 2** — this identity was donated to agents (service accounts, CI, a mailbox the bots own).

Same protocol on a laptop and in Codex Cloud / Flue / Cloudflare Agents. An agent is a principal. Cloud and laptop prove it with the same OIDC token we verify. The laptop socket is transport, not a second identity.

## Status

Engine + local SQLite vault + CLI + MCP. Covered by tests.

## Hosted quickstart

Origin is `https://veil.nyc`. Accounts are invite-only during alpha — an org owner runs `veil human invite EMAIL`, which issues a recovery link that is the invitee's account setup. The hosted vault is Postgres — your laptop keeps no second store when `VEIL_ORIGIN` is set.

```
# 1. CLI
go install github.com/VortexNYC/veil/cmd/veil@latest

# 2. Human token — browser flow, or VEIL_LOGIN_EMAIL + VEIL_KRATOS_PASSWORD_FILE
#    (+ VEIL_KRATOS_TOTP_FILE) for headless. Writes a file, never stdout.
veil human login --out-file ~/.config/veil/human.jwt

# 3. Provision your org on the origin (idempotent)
VEIL_ORIGIN=https://veil.nyc veil init --oidc-token-file ~/.config/veil/human.jwt

# 4. First item + agent + grant (owner verbs take the human JWT as bearer)
export VEIL_ORIGIN=https://veil.nyc VEIL_OIDC_TOKEN_FILE=~/.config/veil/human.jwt
veil item add stripe --uri https://api.stripe.com --secret-file ./sk_live
veil agent add cursor
veil agent hydra cursor --secret-file ~/.config/veil/cursor.hydra
veil grant add --agent cursor --item stripe --level level2

# 5. Wire the agent — stdio for Cursor, HTTP Bearer for cloud agents
veil mcp laptop   # prints the Cursor block; token file path, never the JWT
```

Cloud agents (Codex Cloud, Flue, Cloudflare) skip step 5's stdio block: mint with `client_credentials` at `https://id.veil.nyc`, send the JWT as Bearer to `https://veil.nyc/mcp`. OpenAPI: `https://veil.nyc/openapi.json`. TS SDK: `npm i @vortex-api/veil`.

## Local vault

```
make test
go run ./cmd/veil init --home /tmp/veil
go run ./cmd/veil item add stripe --home /tmp/veil --uri https://api.stripe.com --secret-file ./key --totp-file ./seed
go run ./cmd/veil agent add claude --home /tmp/veil
go run ./cmd/veil agent bind claude --home /tmp/veil --issuer https://token.actions.githubusercontent.com --subject 'repo:VortexNYC/veil:ref:refs/heads/main' --audience veil
go run ./cmd/veil grant add --home /tmp/veil --agent claude --item stripe --level level2
go run ./cmd/veil mcp --home /tmp/veil
go run ./cmd/veil mcp config
go run ./cmd/veil serve --home /tmp/veil
go run ./cmd/veil fill install --home /tmp/veil
go run ./cmd/veil item add github --home /tmp/veil --ssh-file ./id_ed25519
go run ./cmd/veil ssh --home /tmp/veil
go run ./cmd/veil run --home /tmp/veil --agent claude -- curl -s https://api.stripe.com/v1/customers
```

MCP tools: `list_items`, `fetch`. HTTP: `GET /v1/items`, `POST /v1/use`, `GET /v1/events`, owner `POST /v1/items` and `/v1/grants`. Same operations. Streamable HTTP MCP at `https://veil.nyc/mcp`. OpenAPI at `https://veil.nyc/openapi.json`. Bearer is the agent or a Keto member human (`VEIL_OIDC_TOKEN` / Hydra JWT). `VEIL_ORIGIN=https://veil.nyc` makes CLI `use`, `audit`, `item`, `grant`, `fill`, `run`, and `proxy` hit that API and never open a second sqlite. Origin `run` puts dummy `veil-inject` in the child env and MITMs via `POST /v1/use`. Cursor (Dock-launched, no Bearer interpolation) uses `veil mcp stdio` against that origin; token stays in `VEIL_OIDC_TOKEN_FILE`. If that JWT is stale, stdio remints with `client_credentials` from `VEIL_HYDRA_SECRET_FILE` (Hydra public issuer). A human JWT remints with Kratos frontend HTTP when `VEIL_LOGIN_EMAIL`, `VEIL_KRATOS_PASSWORD_FILE`, and `VEIL_KRATOS_TOTP_FILE` are set. The Hydra client secret never enters MCP JSON. The model cannot switch principals and never sees the token. Secrets never appear in tool or SDK output. SDKs are generated: `pnpm run sdk:generate`. Docs: `pnpm run docs:dev`.

Cloud agents (Flue, Cloudflare, Codex) hit the URL with Bearer. The host is not identity. Origin is Railway. Cloudflare is DNS plus Workers (`login.veil.nyc`, `app.veil.nyc`). Cloud agents mint at `https://id.veil.nyc` (`client_credentials`), then send the JWT. Hydra admin stays private.

```
veil agent add cursor
veil agent hydra cursor --secret-file ./cursor.hydra
veil agent token cursor --secret-file ./cursor.hydra --out-file ./cursor.jwt
veil grant add --agent cursor --item stripe --level level2
veil mcp
veil mcp config
veil mcp laptop
```

Paste `mcp config` as the HTTP server block when the process has `VEIL_OIDC_TOKEN`. Cursor does not: `veil mcp laptop` prints the stdio block with the token-file *path* that is already in the env, never the JWT, never `${file:}`. `mcp config` includes `issuer` when `VEIL_HYDRA_ISSUER` is set (`https://id.veil.nyc`). `.well-known/oauth-protected-resource` points at that issuer. Mint: `agent token` (`POST {issuer}/oauth2/token`). The JWT is `--out-file` only.

`run` is Infisical `vault run`. Against a local vault, granted item material is in the child's environment (item name → env var, uppercased). Against `VEIL_ORIGIN`, the child gets dummy `veil-inject` and `HTTPS_PROXY`; origin `POST /v1/use` injects. The broker does not print the secret. Level 1 is skipped until a human Approves. SSH keys stay on `ssh.sock`. `--inject` file templates are local vault only.

`run` / `proxy` MITM: unmodified HTTP clients go through `HTTPS_PROXY`. Unknown hosts fail closed. A per-machine CA (`ca.pem`) is used for MITM — not goproxy's public default. Origin MITM with a real (non-dummy) Authorization header passes through, so Wrangler asset-upload JWTs keep working.

`fill` is the native host (native messaging + nacl box). `veil fill install` registers this process. Slice 26 dogfoods store KeePassXC-Browser. Slice 31 speaks `passkeys-get` / `passkeys-register` on that host. Customers get a Veil-branded extension (SPEC slice 37). Fill writes into the page. Agents never receive the secret.

`ssh` is the OpenSSH agent. Export `SSH_AUTH_SOCK` from its output. The broker signs. The private key never leaves. `item add --ssh-file` stores the PEM; never argv.

`human invite EMAIL` is the invite: `POST /v1/invites` mints the Kratos identity plus recovery link and emails it through the mail worker — the invitee's link is their account setup. Owner-gated (`--oidc-token-file` / `VEIL_OIDC_TOKEN_FILE`). With no `VEIL_ORIGIN` the verb talks to Kratos admin directly and `--code-file` writes the code instead. Re-inviting the same email resends. Email stays in Kratos; membership is Keto; there is no invite table.

`device offer` wraps master to another machine's public key (`nacl/box`, same as fill). `Init` writes `device.key` and `wraps/`. `device accept` writes those, not plaintext `master.key`. Copy `vault.db` yourself. That is not sync and it does not pair a model.

TOTP is a field on the item, not a tool. `totp enroll --out-file --qr-file` writes the seed and an otpauth PNG. Then `item add --totp-file`. At Use, `pquerna/otp` mints a 6-digit code and the broker sets `X-TOTP`. The model never receives the seed or the code. There is no `get_totp`.

There is no `item get`. Secrets are injected at Use, not printed.

See [docs/SPEC.md](docs/SPEC.md) for the capability map and [docs/prior-art.md](docs/prior-art.md) for licensed libraries and the patterns we follow from Infisical, Bitwarden, OneCLI, 1Password, Aside, Entra, and MeowPass.

## License

MIT. Copyright 2026 Veil NYC, Inc.
