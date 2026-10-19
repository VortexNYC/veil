# Prior art

The company we are taking customers from is **1Password**. Solo, families, small teams, startups under 100, developers, agents. We take their inject / SSH agent / fill / TOTP shape. We do not take Connect, `op` as GetSecret, vaults-as-ACL, AWS Secrets Manager sync, Privileged Access, or Device Trust.

Reviewing open source and using it under its license is legal. Being inspired by a product's patterns, solutions, and methods is legal. Copying their source into this tree is not.

Ideas we take. Code we do not copy. Licenses we respect.

| Source | License | What we take | What we leave |
|---|---|---|---|
| [Infisical](https://github.com/Infisical/infisical) | MIT (`ee/` is paid) | Machine identity as a principal with many auth methods (Universal Auth = client_credentials; OIDC/JWT/K8s/AWS/GCP/Azure/SPIFFE/TLS). Temporary access with TTL. Approval policy on *changes*. Audit log stream. Honey tokens (decoy credential that alerts on Use). Dynamic secret + rotation as a *pattern* (mint ephemeral, revoke). Gateway: reach private resources without inbound. | Dashboard, folders/environments as vaults, secret sync into GitHub/Vercel (that is GetSecret), PKI/CA, KMS encrypt API, PAM/session recording, secret scanning CLI, Gorm/TS monolith, becoming Teleport. |
| [Infisical Agent Vault](https://github.com/Infisical/agent-vault) | MIT (`ee/` is paid) | Agent as a named principal. Discover by name, never by value. Inject at HTTP edge. Scrub responses. `vault run` env into a child. Dummy placeholders (`__github_pat__`) so an SDK will start; the proxy substitutes. Orchestrator mints a short-lived session token for a sandbox (`sessions.create`, `vaultRole=proxy`) and passes only `HTTPS_PROXY` + CA. Broker on a different host from the agent. Postgres when more than one instance. | Their MITM/CA (we use goproxy), Gorm, management UI, default-allow unmatched hosts (we fail closed), Infisical product coupling. |
| [Infisical Agent Proxy](https://infisical.com/docs/documentation/platform/agent-proxy/quickstart/credentials) | proprietary product | Placeholder substitution. Local proxy vs hosted proxy as the same protocol. | Their dashboard. |
| [OneCLI](https://github.com/onecli/onecli) | see repo | Per-agent rules at the network layer. Audit the request, not the secret. Bitwarden as a *source*, not the data model. | Rust/TS stack, Cognito. |
| [Bitwarden Server](https://github.com/bitwarden/server) | AGPL + Bitwarden license | Split Identity / Api / Events (we already split Ory vs broker). Machine Secrets Manager: service account + scoped secret, not a human vault. Event ingest as its own plane. | User/org/collection vaults. Client-side E2E for the agent inject path (origin must decrypt to inject). Admin/Billing/SCIM/SSO consoles. Vaultwarden. Send as a product. PAM/session recording. |
| [Bitwarden Agent Access](https://github.com/bitwarden/agent-access) | Apache-2.0 | JIT request for one item. Fine-grained, user-mediated, just-in-time. `run` injects env into a child. TOTP lives on the item; we mint at inject and never return the code. Protocol v0: Noise E2E over a relay that sees only ciphertext. | Returning `{password, totp}` to the caller. Pairing that requires a laptop for cloud agents. A public relay. Becoming a Noise/WebSocket product. |
| [HashiCorp Vault](https://github.com/hashicorp/vault) | BSL | Lease on issued material: TTL, renew, revoke. Revoke a *tree* (everything this agent read) for incident response. Dynamic secrets: broker mints, broker revokes. Auth methods as plugins mapped onto an existing principal (AppRole ≈ Hydra client_credentials; AWS/K8s/JWT ≈ workload OIDC). Response wrapping: one-time token that unwraps into a Use session. Audit every Use. | Becoming a PKI CA. Transit-as-a-product. Consul/Raft. Importing `github.com/hashicorp/vault` as a library (they forbid it). Path-ACL instead of per-item grants. UI. Enterprise namespaces. |
| [KeePassXC](https://github.com/keepassxreboot/keepassxc) | GPL-2 or GPL-3 | Field references between items (we already do `${NAME}` / `veil://` only inside inject). Entry history. TOTP on the item, mint at use. SSH agent socket; Add/Remove refused. Browser fill is a native host, not the vault. | KDBX as the store. Groups. Auto-Type. YubiKey as vault unlock. Secret Service. HIBP/health reports. Import from 1Password. The desktop app. |
| [Buttercup](https://github.com/buttercup/buttercup-desktop) | GPL-3.0 | Encrypted vault *file* plus a dumb blob backend (fs / WebDAV). Desktop holds keys; extension never does. | Dead project (archived 2025). `.bcup`. Dropbox/Drive sync. Their Electron app. |
| [AuthPass](https://github.com/authpass/authpass) | GPL-3.0 | KeePass file as a portable ciphertext blob; cloud is just where the file sits (Drive, Dropbox, WebDAV). Biometric unlock of the local key. | Flutter. KDBX. Becoming a KeePass client. iOS/Android. Autofill as the product. |
| [1Password Agentic Autofill](https://www.1password.dev/agentic-autofill) | proprietary | HITL approval names the item. Fill happens outside the model. Veil execute mode (`docs/fill.md`): agent moves the cursor, host writes the field, MCP never sees the secret. | Always-level-1. Browser-only. Closed. Returning `{password, totp}` to the agent. |
| [Aside](https://aside.com/features/password-manager) ([docs](https://docs.aside.com/help/password-manager.md)) | proprietary. not OSS | Agent login *action*; password is autofilled into the page, never shown to the model. URL checked before a payload is built. MFA/passkey/CAPTCHA stay human. Fill ≠ payments/posts. Affiliated Google properties for suggestions. | Their browser. `Always allow` / `While unlocked` / `Never` (default Always). Vault-unlocked = agents roam. Becoming Chromium. Native-messaging copy hacks for 1Password/Apple. |
| [Apple Passwords](https://support.apple.com/en-us/120758) | proprietary | One store: passwords + passkeys + TOTP. AutoFill choose (QuickType / `prepareCredentialList`) and execute (`provideCredentialWithoutUserInteraction`). OTP is a credential type. Web↔app via associated domains. Windows and non-Safari browsers get an **extension** (iCloud Passwords) — they already admitted AutoFill is Safari-shaped. Shared Groups ≈ human grants. New-device approve ≈ pairing. | iCloud Keychain as our sync. Wi-Fi / AirDrop / Sign in with Apple / breach UI. Being the OS. Save-prompt and password generation in 37 v1. |
| [apple/password-manager-resources](https://github.com/apple/password-manager-resources) | MIT | `shared-credentials.json` for **choose** (name the source site; never silent release). `apple-appIDs-to-domains-shared-credentials.json` for native-app match (32/33). `password-rules.json` + HTML `passwordRules` for fill `generate`. 2FA-appended-to-password list: another reason we don’t auto-submit. Payment-iframe list: don’t save. Depend on the JSON; contribute quirks back. | Using it as field detection (`sites.js`). Vendoring a drifting copy. Change-password URLs until we drive that UI. |
| [Microsoft Entra Agent ID](https://learn.microsoft.com/en-us/entra/agent-id/what-are-agent-identities) | proprietary | Agent is a directory object with an owner. Workload federation: trust an external issuer, map subject to a principal. | Entra tokens only. Not a vault. Becoming an IdP. |
| [MeowPass](https://github.com/meowrithm/meowpass-cli) | see repo | Go CLI + MCP. Tool not value. Argon2id + AES-GCM (we use x/crypto XChaCha20-Poly1305 instead). | `.env` product. |
| [Authsome](https://github.com/agentrhq/authsome) | see repo | OAuth login once; refresh stays in the broker. Access token injected, never returned. | Local-only. Their token client — we use `golang.org/x/oauth2`. |
| [Ory Kratos](https://github.com/ory/kratos) | Apache-2.0 | Directory: passwords, sessions, recovery, invites. Pin the official image. | Their source in this tree. A user table. |
| [Ory Hydra](https://github.com/ory/hydra) | Apache-2.0 | Issuer. Login and consent are a hole we fill. | fosite. Compiling Hydra into the broker. |
| [Ory Keto](https://github.com/ory/keto) | Apache-2.0 | Identity-plane owner/member. Pin the official image. | Grant tuples. Compiling Keto into the broker. |
| [Ory Elements](https://github.com/ory/elements) | Apache-2.0 | Screens in `identity/login` only. Follows the identity schema. | Using it inside the broker. Auth.js. |
| [KeePassXC-Browser](https://github.com/keepassxreboot/keepassxc-browser) | GPL-3.0 | The fill wire we speak in slice 26. Native messaging, local process answers. Passkeys included. | Their source in this tree. KeePassXC as the vault. Shipping their listing as the Veil product. `gkpxc` — that is a client of their database. |

## 1Password OSS list (2026-09-10)

Source: [opensourcealternative.to/alternativesto/1password](https://opensourcealternative.to/alternativesto/1password) — five names: Vault, KeePassXC, Bitwarden, Buttercup, AuthPass. Plus [Infisical/infisical](https://github.com/Infisical/infisical) as requested.

That list is a 1Password clone roundup. Most of it is a human vault, a file format, or a desktop. Veil is not that architecture. The company we attack is 1Password. Backend gold we still take from Infisical (platform + Agent Vault) and Vault is sandbox session, revoke-tree, placeholders, honey — not their PKI/PAM.

### Already true here

- Inject at the edge; agent never receives the secret. Unknown hosts fail closed (`internal/proxy`).
- Agent principal + Hydra `client_credentials`. Workload OIDC maps onto an existing agent.
- Grant per item, level 1 vs 2. Grant expiry. No collections, no folders-as-vaults.
- `${NAME}` / `veil://` expand only inside `run --inject`. Unknown refs fail closed.
- TOTP minted at Use. SSH agent socket. Item history. Audit events without secrets.
- Origin is Railway. Laptop is a cloud agent of that origin.

### Backend patterns — status

1. **Sandbox session mint** — written. Owner `POST /v1/sessions` (Infisical Agent Vault `sessions.create`). Orchestrator mints a short-lived token onto an existing agent and hands the sandbox only that file plus `HTTPS_PROXY` + CA. The 1h agent JWT stays off the sandbox. Token once; never list; never MCP. Default 15m, max 1h. `internal/app/session.go`, `veil session`.
2. **Revoke tree** — written. `veil agent revoke --id` kills the principal, every grant it holds, and live sandbox sessions. In-flight Use fails closed with `agent_revoked` (checked again just before the secret is touched). `internal/broker/broker.go`.
3. **Dummy placeholders** — written. `veil-inject` is the child-env dummy; the proxy substitutes on the wire. `internal/proxy/dummy.go`.
4. **Honey item** — written. `item add --honey` sets `kind=honey`; any Use denies with reason `honey` before grant evaluation and writes an audit row. Decoy material optional.

### Do not add

- Dynamic AWS/Postgres secret engines until we have that problem. Static API keys are the first customer.
- Secret sync to GitHub/Vercel/AWS SM. That is GetSecret into someone else’s store.
- PKI, KMS, PAM, session recording, Teleport.
- KDBX, `.bcup`, collections, Send, Vaultwarden, a public Bitwarden-style relay.
- Importing HashiCorp Vault as a Go module.

If a library already does the job, depend on it:

- AEAD: `golang.org/x/crypto/chacha20poly1305`
- TOTP: `github.com/pquerna/otp`
- HTTPS MITM: `github.com/elazarl/goproxy`
- SQLite: `modernc.org/sqlite`
- OIDC verify: `github.com/coreos/go-oidc/v3` — the broker does not issue tokens
- OAuth refresh: `golang.org/x/oauth2`
- SSH agent: `golang.org/x/crypto/ssh/agent`
- Logs: `log/slog` JSON to stderr. Railway HTTP logs (`railway logs --http`) are the request log. Traces: `otelhttp` + OTLP HTTP. Sink is PostHog (`/i/v1/traces`, org already has it). Skip `/health` and `/ready`. No query string, no Authorization, no body on spans. Not zap. Not a collector we run. Not Langfuse.
- Humans: pinned Ory Kratos + Hydra + Keto. Glue is their Go clients. Screens are Ory Elements in `identity/login` only. Not Zitadel. Not Better Auth. Not a fork of Ory. Not Oathkeeper. Not Talos. Not their agent-security product. Grants are not Keto tuples.
