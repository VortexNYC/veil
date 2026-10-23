# Veil

**Veil** is an agent-first credential broker and human vault (`https://veil.nyc`).

Agents get the **ability to use** a credential—they never receive the plaintext secret. Grants decide which items an agent may use (`level2` autonomous or `level1` human-approved); the broker injects the credential at the edge and scrubs secrets from upstream responses; humans in the same organization use Veil to autofill passwords, TOTP codes, cards, and WebAuthn passkeys across macOS, iOS, Android, and browsers.

- **Hosted Origin & MCP**: `https://veil.nyc` · `https://veil.nyc/mcp`
- **Web Vault**: `https://app.veil.nyc`
- **Documentation & OpenAPI Reference**: `https://veil.nyc/docs` · `https://veil.nyc/openapi.json`
- **Security Architecture**: `https://veil.nyc/security`

---

## Architecture Overview

```text
┌────────────────────────────────────────────────────────────────────────────┐
│                              Callers & Surfaces                            │
│  • Cloud Workloads (Vercel, GitHub Actions, Fly, Better Auth/Clerk/WorkOS) │
│  • Coding & AI Agents (Cursor, Claude Code, Devin, Grok, Muse via MCP/SDK) │
│  • Humans (macOS Veil.app, Chrome/Firefox/Safari Extension, iOS/Android)   │
└──────────────────────────────────────┬─────────────────────────────────────┘
                                       │
                    Short-lived OIDC / Hydra JWT / Session
                     (+ optional RFC 9449 DPoP ES256 proof)
                                       ▼
┌────────────────────────────────────────────────────────────────────────────┐
│                          Veil Broker & Vault Origin                        │
│                                                                            │
│  1. Principal Resolution (PrincipalAgent vs PrincipalHuman — never shared) │
│  2. Honey Tripwire Check (--honey / veil:honey-revoke instant revocation)  │
│  3. Grant Engine (level1 human-approved, level2 autonomous, deny)          │
│     └─ Blocks provider key-management paths (/api-keys, /v3/api_keys, ...) │
│  4. Sealed Material Unpack (XChaCha20-Poly1305 under per-org master key)   │
│     ├─ Static API Keys / Logins + TOTP (pquerna/otp)                       │
│     ├─ OAuth Refresh Tokens + Outbound RFC 7523 WIF (TokenCache)           │
│     ├─ Dual-Phase Provider Auto-Rotation (Resend, SendGrid, PostHog, Spec) │
│     └─ Sealed WebAuthn P-256 Passkeys (Touch ID / remote approval gated)   │
│  5. Edge Injection + Response Scrubbing + Transactional Audit Row          │
└────────────────────────────────────────────────────────────────────────────┘
```

### Load-Bearing Invariants

1. **Agents never receive secrets**: No API route, MCP tool, SDK method, log line, or audit record ever returns a vault secret.
2. **Principal separation (`PrincipalAgent` vs `PrincipalHuman`)**: Agents do not log in as you or share your personal session. Every agent has its own principal ID, its own grants, and forensic attribution (`agent_id`, `approval_id`) in `GET /v1/audit/events`.
3. **Identity is Ory**: Humans authenticate through **Ory Kratos** (password + TOTP/WebAuthn), tokens through **Ory Hydra** (`https://id.veil.nyc`), and org owner/member checks through **Ory Keto** (`identity/glue`). Or run `veil mcp --self-host` as a single binary on SQLite/Postgres with zero Ory containers.

---

## Feature Matrix

| Category | Capabilities |
| --- | --- |
| **Agent MCP & HTTP Broker** | Streamable HTTP MCP (`https://veil.nyc/mcp`) and stdio bridge (`veil mcp stdio`). Tools: `list_items`, `match_items`, `check`, `fetch`, `approval_status`. HTTP: `POST /v1/use`, `veil run` (MITM injection with dummy placeholders). |
| **OIDC Workload Federation** | `veil agent bind` with presets for **Vercel** (`--preset vercel`, immutable `owner_id`/`project_id` claims), **GitHub Actions** (`--preset github-actions`), **Fly.io** (`--preset fly`), **Better Auth**, **Clerk**, **WorkOS**, and custom HTTPS OIDC issuers. Includes `veil agent verify-issuer`, `veil agent workloads`, `veil agent unbind`, and dial-time DNS-rebind/CGNAT SSRF protection. |
| **Zero-Env TypeScript SDK** | `@vortex-api/veil` (`createAmbientClient`, `ambientOidcToken`) automatically resolves `x-vercel-oidc-token`, `VERCEL_OIDC_TOKEN_FILE`, `VERCEL_OIDC_TOKEN`, GitHub Actions `ACTIONS_ID_TOKEN_REQUEST_URL`, or `VEIL_OIDC_TOKEN`. |
| **Outbound RFC 7523 WIF** | `veil item add --federation-rule-id fdrl_...` mints short-lived OIDC assertions from `https://id.veil.nyc` and exchanges them for ephemeral upstream tokens (such as **Anthropic** `sk-ant-oat01-...`) with `singleflight` caching (`material.TokenCache`), so Veil never stores a static upstream API key. |
| **RFC 9449 DPoP PoP** | Optional sender-constrained `DPoP` (`dpop+jwt` ES256) verification on `/v1/use`, `/mcp`, and principal-authenticated `/v1/*` routes (`htm`, `htu`, `iat`, `ath`, `jti` replay cache, `cnf.jkt` + bind-on-first-use key pinning). |
| **Dual-Phase Key Rotation** | `veil rotate ITEM` runs a 4-step state machine (`Create → Verify → Commit → Retire`) inside the sealed envelope. Built-in zero-touch rotators for **Resend**, **SendGrid** (scope-preserving), **PostHog** (`us`, `eu`, `app`), and declarative `rotate.Spec`, plus assisted rotation (`--secret-file`), separate `--management-item`, scheduled `--interval`, and `veil item verify`. |
| **Breach Containment** | `grant.Evaluate` blocks agent calls to provider key-management endpoints (`reason="management_endpoint"`). Decoy `--honey` items tagged `veil:honey-revoke` revoke the offending agent principal on first touch (`reason="honey_tripwire agent=<id>"`). |
| **Human Fill, Passkeys & Migration** | Browser extension (Chrome/Firefox/Safari), macOS `/Applications/Veil.app` (`UniversalFill` `⌘\` + AutoFill appex + single native Touch ID prompt), iOS/Android AutoFill, sealed WebAuthn P-256 passkeys with local Touch ID or remote device approval (`POST /v1/fill/request`), and one-command migration (`veil item import --from-op` or `.1pux` / Bitwarden / Apple Passwords / KeePass / `otpauth://`). |

---

## Quickstarts

### 1. Install CLI & Migrate from 1Password

```bash
# Install the veil binary
curl -fsSL https://veil.nyc/install.sh | sh

# Authenticate your human owner session
veil human login --out-file ~/.config/veil/human.jwt
export VEIL_ORIGIN=https://veil.nyc VEIL_HUMAN_TOKEN_FILE=~/.config/veil/human.jwt

# Import logins, API keys, TOTP seeds, and cards directly from your signed-in 1Password CLI
veil item import --from-op
```

### 2. Bind a Cloud Workload (Vercel / GitHub Actions / Fly.io) — Zero Static Env Keys

```bash
# Register an agent and bind Vercel OIDC with rename-resilient team/project ID claims
veil agent add web-agent
veil agent bind web-agent \
  --preset vercel \
  --org acme \
  --app acme-web \
  --env production \
  --claim owner_id=team_abc123 \
  --claim project_id=prj_xyz789

# Grant autonomous (level2) use on an item
veil grant add --agent web-agent --item resend --level level2
```

Call from your Vercel Function or GitHub Action using `@vortex-api/veil`:

```ts
import { createAmbientClient, useItem } from "@vortex-api/veil";

export async function POST(req: Request) {
  const client = createAmbientClient({ request: req });
  const { data } = await useItem({
    client,
    body: {
      item: "resend",
      url: "https://api.resend.com/emails",
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        from: "agent@veil.nyc",
        to: ["ops@veil.nyc"],
        subject: "Hello from Veil",
        text: "Injected at the broker edge with zero static env keys.",
      }),
    },
  });
  return Response.json({ status: data?.status });
}
```

### 3. Connect Coding Agents (Cursor, Claude Code, Devin, Grok) via MCP

```bash
veil agent add cursor
veil agent hydra cursor --secret-file ~/.config/veil/cursor.hydra
veil grant add --agent cursor --item github --level level2

# Print the stdio MCP config for Cursor (uses token file path, never embeds the JWT)
veil mcp laptop
```

### 4. Single-Binary Self-Host Mode (No Ory Containers Required)

```bash
# Run against a local SQLite vault (~/.veil)
veil mcp --self-host --listen 127.0.0.1:4461

# Or against Postgres
VEIL_SELF_HOST=1 VEIL_POSTGRES_DSN="postgres://..." veil mcp --listen :4461
```

---

## Repository Layout

| Path | Description |
| --- | --- |
| `cmd/veil` | Main `veil` CLI, HTTP origin server, MCP server, and native fill host binary. |
| `internal/app` | Core application orchestration: items, grants, agents, workloads, rotation, verification, and sessions. |
| `internal/broker` | Credential injection engine, honey tripwire enforcement, response scrubbing, and child-process `run` env. |
| `internal/grant` | Grant evaluation (`level1`, `level2`, `deny`), URL host matching, and provider management-endpoint blocking. |
| `internal/material` | Sealed credential envelopes (`api_key`, `oauth`, Outbound RFC 7523 `wif`, `passkey`, `card`, `identity`) + `TokenCache`. |
| `internal/workload` | External OIDC token verification (`coreos/go-oidc`), provider presets, custom claim matching, and issuer dry-runs. |
| `internal/dpop` | RFC 9449 OAuth 2.0 Demonstrating Proof-of-Possession (`DPoP`) ES256 verifier and proof minter. |
| `internal/rotate` | Dual-phase provider key rotators (`Resend`, `SendGrid`, `PostHog`, and declarative `rotate.Spec`). |
| `internal/fill` | Browser/OS native fill bridge, passkey WebAuthn ceremony signing, and remote approval polling. |
| `internal/store` | Storage implementations (`SQLite`, `Postgres` via `pgx`/`sqlc`, `Memory`) and SQLite-to-Postgres migrator. |
| `identity/glue` | Ory Kratos, Hydra, and Keto integration service. |
| `apps/vault` | Web Vault SPA (`https://app.veil.nyc`, React + TanStack Router + `@cloudflare/kumo`). |
| `apps/docs` | Documentation & OpenAPI reference site (`https://veil.nyc/docs`, Blume on Cloudflare Workers). |
| `apps/site` | Public landing & security pages (`https://veil.nyc`). |
| `apps/fill` | Cross-browser WebExtension (Chrome, Firefox, Edge, Safari) for autofill and passkeys. |
| `apps/fill-safari` | macOS `Veil.app` (menu-bar helper, `UniversalFill`, Safari WebExtension, and macOS AutoFill provider). |
| `sdks/` | Generated Go (`sdks/go`), Python (`sdks/python`), and TypeScript (`sdks/typescript`, `@vortex-api/veil`) SDKs. |

## Development & Verification

```bash
# Run the fast CI contract (SDK freshness, go vet, lint, typecheck) and full test gate
make ci

# Build and install the macOS /Applications/Veil.app bundle locally
make macos-app

# Regenerate Go, Python, and TypeScript SDKs from docs/openapi/veil.openapi.json
pnpm run sdk:generate
```

See [`docs/SPEC.md`](docs/SPEC.md) for the slice specification, [`docs/fill.md`](docs/fill.md) for the fill architecture, and [`docs/prior-art.md`](docs/prior-art.md) for prior art and licensed dependencies.

## License

MIT. Copyright 2026 Veil NYC, Inc.
