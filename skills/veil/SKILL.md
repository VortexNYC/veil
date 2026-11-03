---
name: veil
description: Use Veil MCP (list_items, match_items, check, fetch, approval_status) to make authenticated API calls to GitHub, Linear, Cloudflare, Firecrawl, Gmail, Resend, and other services without ever touching plaintext secrets.
---

# Veil — Credential Broker MCP Skill

Veil (`https://veil.nyc/mcp`) is mounted as the `veil` MCP server. **Agents never receive secrets.** Instead of reading API keys from `.env` or 1Password, use Veil's 5 MCP tools so the Veil broker injects the credential at the edge and scrubs any echoed secret from the response.

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
   - Polls the status (`open`, `approved`, `denied`, `expired`) of a `level1` approval request without re-triggering the upstream call. Once `status === "approved"`, retry `fetch`.

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
