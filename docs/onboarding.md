# Onboarding — humans, coding agents, Grok, and cloud bots

Veil separates **who may administer the vault** (humans, owner JWT, SPA, CLI) from **who may Use granted items** (agents, Hydra, MCP). Agents never import, invite, or provision.

**Docs (published):**

- [Onboarding guide](https://veil.nyc/docs/guides/onboarding) (this content)
- [Grok + Veil](https://veil.nyc/docs/guides/grok)
- [Migrating from 1Password](https://veil.nyc/docs/guides/migration-1password)
- [Agents & MCP](https://veil.nyc/docs/guides/agents)

## Three personas

| Persona | Signs up | Imports passwords | Creates agents | Uses MCP |
|--------|----------|-------------------|----------------|----------|
| **Human alone** | app.veil.nyc or login.veil.nyc | Items → Import or `veil item import` | Vault → Agents | N/A |
| **Human + coding agent** | Human in browser | Human approves `veil item import` in terminal | Human in SPA/CLI | Agent after grants |
| **Human + Grok** | Human | Human in SPA | Human creates `grok` + grants | Custom MCP → veil.nyc/mcp |

## Checklist API (single source of truth)

- **SPA:** banner on every vault page until complete (`GET /v1/onboarding`).
- **CLI:** `veil onboard status` (JSON) · `veil onboard guide` (markdown for chat).
- **Coding agent:** human runs `veil onboard status`, pastes JSON — read `agent_hint` per step. No JWT or exports in chat.

`complete: true` (owner): at least one item, at least one agent, every agent has a grant.

JSON also includes `mcp_tools`, `import_caveats`, `docs`, and `agent_playbooks`.

## Human signup

1. Open **https://app.veil.nyc** (or invite recovery at **login.veil.nyc**).
2. Kratos: email, password, **TOTP** (required for fill and owner actions).
3. First login calls **`POST /v1/provision`** — fresh org + wrapped master (idempotent).

## Migration (one-shot import)

Not sync. Not Connect. Not MCP. See **`docs/migration-1password.md`** for 1Password / Grok Bot displacement.

| Source | How |
|--------|-----|
| 1Password | Export **.1pux**, or `veil item import --from-op` |
| Bitwarden | Unencrypted **CSV or JSON** export |
| Apple Passwords | CSV export |
| LastPass / Dashlane / Google | CSV export |
| Proton Pass | Zip export |
| KeePass | XML export |
| TOTP seeds | File of `otpauth://` lines |

**SPA:** Items → **Import**.  
**CLI:** `VEIL_ORIGIN=https://veil.nyc VEIL_HUMAN_TOKEN_FILE=… veil item import export.1pux`

## MCP tools (all agents)

| Tool | Purpose |
|------|---------|
| `list_items` | Granted items (metadata only) |
| `match_items` | Which item matches a URL |
| `check` | Dry-run; on `need_approval` **files** the ask |
| `fetch` | HTTP with inject |
| `approval_status` | Poll level-1 request |

Not on MCP: import, provision, invites, `GetSecret`, fill secrets.

## Wire a coding agent (Cursor / Codex / Devin)

1. Finish checklist (items + agent + grants).
2. Owner: **Agents** → create `cursor` (or dedicated name).
3. Owner: **Grants** → **level2** for unattended API items.
4. Owner: `veil agent hydra cursor --secret-file …/cursor.hydra`
5. MCP: `veil mcp stdio` or `veil mcp laptop` with `VEIL_ORIGIN=https://veil.nyc`.

## Wire Grok

Full guide: **`docs/grok.md`**.

1. Same checklist; create agent **`grok`**, grant items (level2 for APIs, level1 when you want Requests).
2. `VEIL_AGENT=grok veil mcp http` — paste into **grok.com/connectors → Custom** (`https://veil.nyc/mcp`, Bearer `agent-grok:<secret>`).
3. **Not** 1Password’s Grok Bot “Password Managers” link — that stays 1Password-only.
4. Website login in Grok Bot: Veil **fill** on the device, not MCP `fetch`.

## What agents must never do

- Receive vault passwords, exports, or JWTs in model context.
- Call `POST /v1/import`, `POST /v1/provision`, `POST /v1/invites`.
- Replace human **TOTP** or recovery — guide them to the browser.

## Operator gate

Before a cohort: `make prove-live` on `main`. See `docs/runbook.md` § Private alpha cohort.
