---
name: veil
description: Use Veil MCP (list_items, match_items, check, fetch, request_fill, approval_status) to make authenticated API calls and website login approvals without ever touching plaintext secrets.
---

# Veil — Credential Broker MCP Skill

Veil (`https://veil.nyc/mcp`) is mounted as the `veil` MCP server. **Agents never receive secrets.** Use these **six** MCP tools so the broker injects at the edge or files a human ask.

## Connecting an agent

Hosted HTTP (preferred — no local process):

```bash
veil mcp http   # prints the paste-ready mcpServers block, including your agent's bearer
```

The bearer is `agent-<name>:<client_secret>` from `veil agent hydra`. The broker exchanges it server-side at Hydra (cached), so the header never expires. For env-interpolated configs, set `VEIL_MCP_TOKEN=agent-<name>:<secret>` and use `Authorization: Bearer ${env:VEIL_MCP_TOKEN}`.

Laptop stdio shim (when the client can't set headers): `veil mcp laptop` prints the stdio block — the same deployed origin underneath.

## Available MCP Tools

1. **`list_items`** (`mcp__veil__list_items` / `veil__list_items`):
   - Lists every item granted to your agent (`name`, `kind`, `uris`, `tags`). Never returns secret material.
2. **`match_items`** (`mcp__veil__match_items` / `veil__match_items`):
   - Input: `{ "url": "https://api.github.com/user" }`
   - Finds which granted item(s) match a target URL or domain.
3. **`check`** (`mcp__veil__check` / `veil__check`):
   - Input: `{ "item": "github", "url": "https://api.github.com/user", "method": "GET" }`
   - Evaluates the grant decision (`allow`, `need_approval`, or `deny`) without calling the upstream URL or touching the secret. On `need_approval`, files the approval request and returns `request_id`.
4. **`fetch`** (`mcp__veil__fetch` / `veil__fetch`):
   - Input: `{ "item": "github", "url": "https://api.github.com/user", "method": "GET", "headers": {}, "body": "" }`
   - Executes the upstream HTTP request with the item's credential injected at the Veil broker.
   - If the grant is `level1` and requires human approval, returns `decision: "need_approval"` and `request_id`.
5. **`approval_status`** (`mcp__veil__approval_status` / `veil__approval_status`):
   - Input: `{ "request_id": "req_..." }`
   - Polls the status (`open`, `approved`, `denied`, `expired`). Response includes `action` (`use` vs `fill`). When `action` is `fill` and `status` is `approved`, the human must run browser fill (extension + host) — not another `fetch`.
6. **`request_fill`** (`mcp__veil__request_fill` / `veil__request_fill`):
   - Input: `{ "item": "airbnb", "url": "https://www.airbnb.com/login" }`
   - Files a **website login** approval. Returns `decision: need_approval`, `request_id`, `approval_url`, `action: fill`. Poll `approval_status`; execute fill on the browser device (CDP `veilAgentic` — not an MCP tool).

## State machine (Grok / Muse / cloud MCP)

| Intent | Tools | Human | Agent after approve |
|--------|-------|-------|---------------------|
| Call an API | `match_items` → `fetch` (or `check` first) | level1: approve at `approval_url` | Retry same `fetch` |
| Sign into a website | `match_items` → **`request_fill`** | Approve at `approval_url` | Poll `approval_status`; partner runs **fillCredential** in Chromium with Veil extension |
| Dry-run API policy | `check` | Same as fetch if `need_approval` | Same |

**Not on MCP (by design):** approve/deny (human only — use **`veil request confirm`** / app / iOS on the human device), passwords/TOTP/passkeys, `execute_fill` (browser CDP only).

## Browser automation (agent-browser, Browserbase, Playwright)

Website login is **not** `fetch`. After **`request_fill`** → human approve → **`approval_status` === `approved`**:

1. Drive a Chromium session with the **Veil MV3 extension** loaded (same profile as `veil fill install` or `--load-extension=apps/fill`).
2. Native **fill host** must be running with a live human JWT.
3. Partner/orchestrator calls **`globalThis.veilAgentic.v1`** on the extension service worker over CDP: `agenticMode.enable({ tabId })`, then `autofill.fillCredential({ tabId, requestId, itemId })`.

HTTP equivalent of `request_fill` for your backend: `POST /v1/agent/fill/request` with agent bearer; poll `GET /v1/agent/fill/request/{id}`. Full Browserbase-shaped doc: repo `docs/guides/agentic-autofill.md`.

## Grok (grok.com + Grok Bot)

**Not** the [1Password × Grok Bot](https://support.1password.com/1password-grok-bot/) Password Managers integration. That path autofills **website logins** after approval in the 1Password app. Veil uses **Custom MCP** to `https://veil.nyc/mcp` for **HTTP API inject**.

Before Grok can call tools: human must import items, create agent `grok`, and grant items (`veil onboard status`).

```bash
export VEIL_AGENT=grok
veil mcp http   # grok.com/connectors → Custom — paste JSON; never ${file:} in cloud config
```

- **API tasks** (GitHub, Stripe, …): `match_items` → `fetch`.
- **Level1**: human approves at app.veil.nyc/requests; you poll `approval_status`.
- **Website login in Grok Bot** (Airbnb, …): MCP cannot fill forms — human needs Veil **fill** extension + host on that device (`docs/fill.md` execute mode).

Human docs: https://veil.nyc/docs/guides/grok · https://veil.nyc/docs/guides/migration-1password

## Human-only (never MCP)

Import, provision, invites, agent creation, grants — owner or SPA. Tell the human to run `veil onboard guide` or open app.veil.nyc.
