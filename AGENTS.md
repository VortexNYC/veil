# Veil — agent contract

The product is **Veil**. Forever. Repo and CLI stay `veil` in VeilNYC until a rename. Agents say Veil, not PWM, not veil.

This is an agent-first credential broker that takes 1Password's customers: developers, agents, solo users, families, small teams, startups under 100. Read `docs/SPEC.md` and `docs/prior-art.md` before writing code. Fill (browser, OS, choose vs execute) is `docs/fill.md`.

## Ory \*

**Ory is the company.** We pin three of their products. They are not synonyms. We do not fork them. We do not rewrite them. Glue talks to them. The broker does not.

| Name | Job | Directory |
|---|---|---|
| **Kratos** | Humans. Email, password, recovery, session. The directory. | `identity/glue/kratos` (client) · `identity/kratos` (image) |
| **Hydra** | Tokens. “Is this proof real?” | `identity/glue/hydra` (client) · `identity/hydra` (image) |
| **Keto** | Org owner / member. Not grants. | `identity/glue/keto` (client) · `identity/keto` (image) |

**Glue** (`identity/glue`) wires those three. Official Ory Go clients are in `glue/kratos`, `glue/hydra`, `glue/keto` — that is the SDK. Glue itself does not import them so a switch is one directory per product. **Broker** is our product: items, agents, grants, inject.

Same UUID (`protocol.LocalOrgID`) is a join key in the vault, Kratos `organization_id`, and the Keto object. It is not three directories.

We do not run Oathkeeper, Keto as a grant engine, or a second user table.

## Load-bearing rules

1. **Agents never receive secrets.** Not in JSON, not in MCP, not in logs, not in audit. If a test can marshal a result and find the secret, the slice is broken.
2. **The grant is the object.** Org RBAC is who may *administer* grants (create and list). The grant is whether this principal may *Use* this item at this level. Members see granted items via list/fill, not `GET /v1/grants`. Humans, invites, sessions live in Ory. Identity-plane owner/member is Keto. sqlite `humans` is planted `self` only. The broker may call glue for member/owner checks. Do not put grants in Keto.
3. **Laptop is a cloud agent.** Same `Use` / `Approve` protocol. Local sockets are transport, not identity.
4. **Do not hand-roll what exists.** Crypto is `golang.org/x/crypto`. TOTP is `pquerna/otp`. MITM is `goproxy`. OIDC verify is `coreos/go-oidc`. OAuth refresh is `golang.org/x/oauth2`. Humans are pinned Ory Kratos + Hydra. Identity-plane owner/member is Keto. Device pairing is `nacl/box`. Slice 26 fill speaks `nacl/box` so store keepassxc-browser works. Slice 37 fill is native messaging JSON — we ship both sides; origin is already TLS. SSH is `x/crypto/ssh/agent`. Do not fork Ory. Do not compile it into the broker. Do not put grants in Keto.
5. **Prior art, licensed use, not copies.** Reviewing open source and depending on a library under its license is legal. Being inspired by a product's pattern is legal. Copying their source into this tree is not. Infisical, Bitwarden, 1Password, Aside, Apple Passwords, `apple/veil-resources`, Entra, OneCLI, Ory, KeePassXC-Browser are prior art.
6. **Go owns the product.** Policy, crypto, grants, inject, CLI, MCP, API, daemon. The SPA (pnpm + Vite+ on Cloudflare) is every human option. Identity screens are Ory Elements at `login.veil.nyc` — do not restyle. Vault widgets are Kumo (`@cloudflare/kumo`). Agents use CLI / MCP / SDK. Phone is a proper iOS/Android app: add a password or fill one — not grants, agents, audit, or settings. Desktop is not a vault app. Mac may ship a menu-bar accessory `.app` (Safari + native-app AutoFill later). Windows/Linux a tray helper. Chrome / Edge / Firefox fill needs an extension — Chromium does not use OS AutoFill for HTML forms. Customers get a Veil-branded extension we ship (SPEC slice 37). Slice 26 proves the Go host with store keepassxc-browser. Do not copy that extension into this tree. Do not scaffold the helper until Chrome URI fill Uses origin.
7. **Tests first** for grant, inject, and leak. GitHub Actions runs `make ci`. `go test -race -shuffle=on ./...` is the Go suite (`testing`, `testing.F` seed corpus, `testing/synctest`). Do not add Ginkgo/testify/gotestsum. Vitest is `veil-vault` only. Fill JS is `node --test` from `TestFieldsJS`. `prove-live` and Chrome for Testing are not CI. `go test -fuzz` is local, not the merge bar.
8. No AI attribution in commits, PRs, or generated files.

## Current slice

1Password. Individual / Families / Teams / startup <100. Agents are the wedge; humans in that same org still fill, TOTP, SSH, share. Not Infisical PKI. Not 1Password Enterprise.

Laptop MCP adapter is written. `veil mcp stdio` against `https://veil.nyc`. Cursor live: `list_items` + `fetch` GitHub `/user` allow/200. Flue Uses Veil on Engineering. One store is written: origin HTTP owns item/grant/list/`run`/`proxy`; `VEIL_ORIGIN` never opens sqlite. Origin `run` is Infisical `vault run` against `https://veil.nyc`: dummy `veil-inject` in child env, HTTPS_PROXY MITM calls `POST /v1/use`, non-dummy Authorization passes through. `--inject` is local vault. `human login --out-file` is the human mint (HTTP remint with password+TOTP files, or browser). Identity on origin is sibling Railway processes (Kratos, Keto, glue, Hydra) plus Cloudflare Workers: login UI at `https://login.veil.nyc`, SPA vault at `https://app.veil.nyc`. Grants stay in the vault. Chrome URI fill (slice 26) is written. Passkeys fill (slice 31) is `passkeys-get`/`passkeys-register` on that host; private key sealed; not MCP. `totp enroll` writes seed `--out-file` and an otpauth QR `--qr-file`. Kratos MFA is official totp + webauthn (not DIY). Human grants are the same grant object (`grant add --human`). Human-surface roadmap is SPEC slices 26–37. Slice 30 SPA vault is written. Fill remints the human JWT. Mac fill is Touch ID before a secret leaves. Passkey assertions are checked against go-webauthn as RP. The native host is the Go binary plus `fill.json` next to it — not a shell wrapper. Do not paper over missing primitives with scripts. After KeePassXC-Browser fill is proven on this Mac, next is slice 37: design our own Chrome/Edge/Firefox/Safari Web Extension. Thin client. Origin does fill, passkeys, remint. Do not copy keepassxc-browser. Slice 32 is later (ASCredentialProvider, not the Safari extension). Written-not-proven is not done. Not another sqlite.

OpenAPI factory. `docs/openapi/veil.openapi.json` is the contract. HTTP `/v1/items` and `/v1/use` (body + headers). CLI `use --body-file`. MCP `fetch` the same operation. Generated Go/TS/Python SDKs. Blume `/reference` via `apps/docs`. Never handwrite clients. No GetSecret.

Item lifecycle and secret refs. `${NAME}` / `veil://name` resolve only inside `run --inject` and child env. Archive, delete, tags, history, file BLOB (owner write to disk), grant `--expires`, owner `audit`, `gen`. MCP agent tools are still only `list_items` + `fetch`. No 1Password vaults/Connect/`op`. No iOS until fill origin is proven.

Cloud coding agents hit `https://veil.nyc/mcp`. Same Streamable HTTP. Bearer after `agent bind` / `agent hydra`. Mint at `https://id.veil.nyc` (Hydra public: token+JWKS). Not admin. Origin is Railway. Cloudflare is DNS plus Workers (`login.veil.nyc`, `app.veil.nyc`, `mail.veil.nyc` email relay, `veil.nyc/` landing). Not Tunnel.

Keto is membership truth. Invite is owner-gated after bootstrap. Master is wrapped (`device.key` + `wraps/`), not a plaintext `master.key`. Origin crypto is per-org: `org_keys` rows hold each org's master sealed under `PWM_KEK` (env → `VEIL_KEK`); `VEIL_MASTER_KEY` is the legacy seed, not the model. If Hydra is configured, Approve is ApproveOIDC. Login is Kratos `oauth2_provider`, not a glue HTTP hop. Hydra consent skip still needs glue AcceptConsent.
