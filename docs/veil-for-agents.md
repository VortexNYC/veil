# Veil for Agents

Veil’s answer to [1Password for Agents](https://1password.com/personal-ai-agent-integrations): agents **Use** credentials without ever holding them. Two lanes — same invariant as 1Password, different transport than their OAuth autofill deals.

| Lane | 1Password | Veil |
|------|-----------|------|
| **API / tokens** | Environments MCP (env in IDE) or manual | **`fetch`** at `https://veil.nyc/mcp` — inject at broker edge |
| **Website login** | Native app autofill after in-app approval | **`request_fill`** → human approves in **Requests** → **fill execute** on device (extension + host) |

Partner map vs 1Password’s shipped integrations: [`1password-for-agents-landscape.md`](./1password-for-agents-landscape.md).

## MCP tools (six)

| Tool | Use |
|------|-----|
| `list_items` | Granted items (metadata only) |
| `match_items` | Pick item for a URL |
| `check` | Dry-run API Use; files level-1 ask |
| `fetch` | HTTP with inject |
| **`request_fill`** | **Website login approval (no password to model)** |
| `approval_status` | Poll any open ask |

Wire any host: [`grok.md`](./grok.md) · [`onboarding.md`](./onboarding.md) · `veil mcp http`.

## Match 1Password’s live partners (today)

| Agent | Veil today | Notes |
|-------|------------|--------|
| **Grok Bot** | Custom MCP + `request_fill` + fill on device | Not their Password Managers OAuth — [`grok.md`](./grok.md) |
| **Meta Muse** | Same MCP pattern; dedicated agent `muse` | No Muse marketplace slot — custom MCP when Muse exposes tools |
| **Instinct** | Same | Mobile-first errands → `request_fill` + phone Requests alerts |
| **Town (Townie)** | Same | Work assistant → grant work logins + MCP |
| **Cursor / Codex / Devin / Claude Code** | `veil mcp stdio` or `laptop` | Beats 1Password Environments MCP — no env in context |
| **OpenAI / Anthropic** (1P waitlist) | Custom MCP at `veil.nyc/mcp` | Ready before their 1Password connectors ship |

### Website task playbook (Grok, Muse, Instinct, …)

1. Human: import, agent, **grant** on the site login item.
2. Agent: `match_items` with page URL → `request_fill` with item + URL.
3. Human: **app.veil.nyc/requests** (or push if enabled) → Approve.
4. Agent: poll `approval_status` → continue browser automation.
5. On the **same machine** as the browser: Veil **extension + host** execute-fill (agent moves cursor; secret never on MCP).

Cloud-only agents without a local browser still get **API** lane only.

## Match 1Password’s dev integrations

| 1Password marketplace | Veil |
|----------------------|------|
| Environments MCP (Cursor, Codex, Kiro) | **`https://veil.nyc/mcp`** + grants + audit |
| Copilot agent hook | `veil agent` + MCP / `agent bind` |
| GitHub Actions `op` | `veil agent bind --preset github-actions` |
| NVIDIA OpenShell | `veil session` + MCP in sandbox |
| Anchor Browser | MCP for API steps + fill for login forms |

## What still needs a partnership

Native **Connect apps → Password Managers → Veil** in Grok/Muse apps (1Password’s UX). Engineering prerequisite is already here: **`request_fill` + fill execute + Requests**. Business step is xAI/Meta/etc. — track in Pile when you want formal outreach.

## Human setup

`veil onboard status` · SPA checklist · Settings → **Agent approval alerts**.
