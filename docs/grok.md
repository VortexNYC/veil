# Grok + Veil

Veil is **not** the [1Password × Grok Bot](https://support.1password.com/1password-grok-bot/) integration. That product is a native **Password Managers** OAuth link: Grok hits a **website login**, you approve in the **1Password app**, 1Password **autofills the form**. Grok never sees the password.

Veil gives Grok **HTTP credential inject** over MCP — API keys, bearer tokens, basic auth at the broker edge. Same security invariant (no secret in the model), different job. For **website sign-in** while using Grok Bot, see [Browser login (fill execute)](#browser-login-fill-execute) below.

## Two Grok surfaces

| Surface | Veil path | Best for |
|--------|-----------|----------|
| **grok.com** (chat) | [Custom MCP connector](https://docs.x.ai/grok/connectors) → `https://veil.nyc/mcp` | GitHub, Stripe, Firecrawl, Gmail API, … |
| **Grok Bot app** (desktop / mobile) | Same custom MCP **or** local `veil mcp stdio` if the app spawns MCP on your machine | Coding + API tasks on the device |

1Password’s Grok integration lives only under **Connect apps → Password Managers** in the Grok Bot app. Veil does not appear there today; use **Custom** MCP with a public URL (no tunnel required for `https://veil.nyc/mcp`).

## Prerequisites (human / owner)

Finish the vault checklist before Grok can Use anything:

1. Sign up at **https://app.veil.nyc** (TOTP required).
2. **Import** passwords (one-shot — not sync). See [migration-1password.md](./migration-1password.md).
3. **Agents** → create `grok` (dedicated Hydra client per bot).
4. **Grants** → grant items to `grok`:
   - **level2** — unattended API work (typical for GitHub, Cloudflare, …).
   - **level1** — each sensitive Use files an approval; you resolve in **Vault → Requests** (enable [request notifications](#level-1-approvals) on your phone).

Checklist API: `GET /v1/onboarding` · `veil onboard status` · SPA banner until complete.

## Wire Grok (hosted HTTP MCP)

Origin is already on the public internet. **Do not** tunnel `veil.nyc` unless you self-host elsewhere.

```bash
export VEIL_ORIGIN=https://veil.nyc
export VEIL_AGENT=grok
veil agent add grok
veil agent hydra grok --secret-file ~/.config/vortex/veil/grok.hydra
veil mcp http   # paste-ready JSON — contains client secret; mode 600 on disk only
```

`veil mcp http` prints:

- `"type": "http"`
- `"url": "https://veil.nyc/mcp"`
- `"Authorization": "Bearer agent-grok:<hydra_client_secret>"`

The broker exchanges `agent-<name>:<secret>` at Hydra server-side (cached). Prefer this bearer over a short-lived JWT in cloud configs.

### grok.com custom connector

1. Open [grok.com/connectors](https://grok.com/connectors) → **New Connector** → **Custom**.
2. Server URL: `https://veil.nyc/mcp`
3. Authentication: **Bearer** header from `veil mcp http` (never `${file:}` in cloud JSON — hosts send the literal string and origin 401s).
4. Grok discovers five tools (names may be prefixed, e.g. `veil__fetch`):

| Tool | Role |
|------|------|
| `list_items` | Granted items (names, URIs — never secrets) |
| `match_items` | Which item matches a URL |
| `check` | Dry-run decision; on `need_approval` **files** the ask and returns `request_id` |
| `fetch` | Upstream HTTP with inject |
| `approval_status` | Poll level-1 request; retry `fetch` when `approved` |

Skill for Grok: repo `skills/veil/SKILL.md` (install via `.grok-plugin`).

### Grok Bot desktop (stdio)

If Grok Bot runs MCP locally and `veil` is on `PATH`:

```bash
veil mcp laptop   # stdio block with VEIL_ORIGIN + hydra secret file
```

Parent process detection sets `VEIL_AGENT=grok` when the caller is Grok. Hydra secret file must exist at `~/.config/vortex/veil/grok.hydra`.

## Level-1 approvals

When `fetch` or `check` returns `decision: "need_approval"`:

1. Human opens **https://app.veil.nyc/requests** (or `veil request list`).
2. Approve or deny; agent polls `approval_status` then retries `fetch`.

**Settings → Agent approval alerts** in the vault enables browser notifications when a new open request arrives (works on phone when the vault PWA/tab is allowed to notify). This is Veil’s analogue to 1Password’s in-app Grok approval prompt — not identical UX, same control point.

## Browser login (fill execute)

Tasks like “book Airbnb” or “sign into this dashboard in the browser” are **not** solved by MCP `fetch`:

- Login forms, OAuth buttons, passkeys, and CAPTCHA need **fill** (`docs/fill.md` **execute** mode).
- Agents **never** receive fill secrets on MCP; they move the cursor, the **Veil extension + Go host** writes fields after Touch ID / confirm.

On the Mac where Grok Bot’s browser runs:

1. Install **Veil fill** (extension + native host / `Veil.app`).
2. Human grants and fill as today — no Grok ↔ Veil native “Password Managers” slot.

For parity with 1Password Grok, a future **xAI partnership** could add Veil to Grok Bot’s password-manager list. Until then: **MCP for APIs**, **fill for web login**.

## What Grok must never do

- Import vault exports, owner JWTs, or Hydra secrets in chat.
- Call `POST /v1/import`, `/v1/provision`, `/v1/invites` (not on MCP).
- Paste passwords into prompts — use Veil inject or human fill.

## Dual stack (common during migration)

You can keep **1Password Grok** for personal web errands and **Veil MCP** for engineering agents. They are different vaults. Displacement path: import into Veil once, re-grant agents, retire 1P items when confident.

## Operator gate

Before a cohort: `make prove-live` on `main`. See `docs/runbook.md`.
