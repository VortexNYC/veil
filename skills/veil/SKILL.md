---
name: veil
description: Use Veil MCP (list_items, match_items, check, fetch, approval_status) to make authenticated API calls to GitHub, Linear, Cloudflare, Firecrawl, Gmail, Resend, and other services without ever touching plaintext secrets.
---

# Veil — Credential Broker MCP Skill

Veil (`https://veil.nyc/mcp`) is mounted as the `veil` MCP server. **Agents never receive secrets.** Instead of reading API keys from `.env` or 1Password, use Veil's 5 MCP tools so the Veil broker injects the credential at the edge and scrubs any echoed secret from the response.

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
