# Migrating from 1Password (including Grok Bot users)

Veil **displaces** 1Password as the store — it does not sync with 1Password Connect, `op` MCP, or [1Password for Agents](https://1password.com/blog/1password-for-ai-agents) while you keep both as one vault.

## One-shot import

| Source | Command / UI |
|--------|----------------|
| 1Password export | Items → Import **.1pux**, or `veil item import export.1pux` |
| Local `op` CLI | `veil item import --from-op` (signed-in machine; optional `--vault`) |
| Other managers | CSV / JSON / zip per `docs/onboarding.md` |

Requires owner human bearer (`VEIL_HUMAN_TOKEN_FILE` or SPA). **Not MCP.**

Repeat import **skips** rows that already match — it does **not** overwrite passwords you rotated in Veil after import.

## 1Password for Grok Bot vs Veil for Grok

| | 1Password × Grok Bot | Veil × Grok (MCP) |
|--|----------------------|-------------------|
| **Integration** | Grok Bot → Password Managers → 1Password OAuth | grok.com **Custom MCP** → `https://veil.nyc/mcp` |
| **Primary job** | Website login autofill after approval in 1Password app | HTTP API / token inject at broker |
| **Secret to model** | Never | Never |
| **Approval** | Per login in 1Password app (~30 days) | level1 grant → Vault **Requests**; level2 for unattended API |
| **Vault scope** | Personal / Private vault logins only (1P limits) | Whatever you imported + granted |
| **Passkeys / “Sign in with Google”** | Not supported by 1P Grok (Oct 2026) | Passkeys via Veil fill (not via MCP); import passkeys from 1P export is limited |
| **Shared / family vaults** | Not supported by 1P Grok | Human grants (not 1P-style shared vault type) |
| **Accounts** | 1password.com Individual/Family only for Agents | Veil org on app.veil.nyc |

After migration, **disconnect 1Password from Grok Bot** if Veil is the only store you want agents to use — otherwise Grok may still ask 1Password for sites you have not moved.

Full Grok wiring: [grok.md](./grok.md).

**1Password for Agents** (Muse, Instinct, Town, Claude/OpenAI waitlist, …): [1password-for-agents-landscape.md](./1password-for-agents-landscape.md).

## Import caveats (1Password → Veil)

- **Login items** with username/password/URI/TOTP seeds generally import from `.1pux` or `--from-op`.
- **Passkeys** in 1Password are often **not** carried in standard exports — re-register at sites or keep passkeys in 1Password until fill migration is done.
- **Sign-in-with-provider** logins (Google/Facebook buttons) may not map to a password item Veil can inject; treat as human step or OAuth item types where supported.
- **Attachments / documents / SSH keys** — depend on export shape; verify in Items after import.
- **1Password business / EU / CA** — 1Password for Agents may be unavailable on those regions; Veil import is independent of 1P Agents eligibility.

## After import

1. Create agents (`grok`, `cursor`, …) in Vault → Agents.
2. Grant **level2** on API items bots need unattended.
3. Run `veil mcp http` (Grok cloud) or `veil mcp laptop` (local Grok Bot).
4. Optional: install fill extension on each human browser (`docs/fill.md`).

Checklist: `veil onboard guide`.
