# 1Password for Agents — partner landscape (Veil competitive map)

Source of truth for **personal agent autofill** integrations (website login, HITL in the 1Password app). Not Connect, not `op` GetSecret, not the **1Password Environments MCP** (`.env` for Codex/Cursor — different product).

Last reviewed against [1password.com/personal-ai-agent-integrations](https://1password.com/personal-ai-agent-integrations) and [marketplace.1password.com](https://marketplace.1password.com/) (Nov 2026).

## Shipped today (“1Password for Agents”)

| Partner | Job | Platforms (1P marketing) | Veil displacement |
|--------|-----|--------------------------|-------------------|
| **Meta Muse** | Personal errands, plans | iOS, Android, Web | Import + human fill; no Muse OAuth slot — MCP if Muse ever exposes HTTP tools |
| **SpaceXSI Grok Bot** | Personal + coding teammate, browser tasks | iOS, Android, macOS, Windows, Linux | [veil-for-agents.md](./veil-for-agents.md) — MCP + **request_fill** + fill execute |
| **Instinct** | Personal agent (mobile-first) | iOS, Android | Same |
| **Town (Townie)** | Email/calendar/work assistant | iOS, Android, macOS, Windows, Linux | Same |

## Waitlist / coming soon (1P marketing page)

Announced on the landing page waitlist form — **not** necessarily shipping:

| Partner | Notes |
|--------|--------|
| **Anthropic** (Claude Duet) | Coding / in-tool agent — blog said “coming soon” |
| **OpenAI** (ChatGPT Work, dots) | General + work agents |
| **Browserbase** | Browser automation SaaS |
| **Browser Use** | Browser automation |
| **Dana** | — |
| **Kernel** | — |
| **Perplexity** | Research / assistant |
| **Ramp** | Finance |
| **Tomo** | — |
| **Wajo** | — |

Blog (Oct 2026) also named these as “coming soon” alongside the waitlist: Instinct (now live), Browserbase, Browser Use, Dana, KERNEL, Perplexity, Ramp, Tomo, Wajo.

## Related but not “for Agents” autofill

These are **developer / env** paths on the marketplace — **not** the Grok-style Password Managers connector:

| Integration | What it is | Veil equivalent |
|-------------|------------|-----------------|
| **1Password Environments MCP** (Cursor, Codex, Kiro) | Inject **env vars** from 1Password Environments into the IDE agent context | **`https://veil.nyc/mcp`** — inject at HTTP edge, no env in model |
| **GitHub Copilot Agent Hook** | Hook surface for Copilot | `veil agent` + MCP / `agent bind` for CI |
| **1Password CLI for GitHub Actions** | CI secrets via `op` | `veil agent bind` + `use` / MCP |
| **1Password for NVIDIA OpenShell** | Sandbox coding agent credentials | Veil sessions + grants + MCP in sandbox |
| **Anchor Browser** | Browser automation + 1P inject | Veil fill + extension OR MCP `fetch` for API steps |

## Eligibility (1Password side)

- **1Password for Agents** (autofill integrations): Individual + Families on **1password.com** (not .eu / .ca per support articles at launch).
- **Not** Team/Business for the personal-agent connectors (enterprise is a different SKU story).

## Veil wedge by agent type

| Agent class | 1Password gives | Veil gives |
|-------------|-----------------|------------|
| **Personal assistant** (Muse, Town, Instinct, Grok errands) | Native OAuth + in-app approval + form autofill | **`request_fill`** MCP + Requests + fill execute on device |
| **Coding / API** (Cursor, Codex, Devin, cloud MCP) | Environments MCP (env files) or manual | **Five-tool MCP**, grants, audit, honey, sessions, WIF |
| **Browser automation** (Browserbase, Anchor) | Partner inject into browser session | MCP for API + **`docs/guides/agentic-autofill.md`** (extension in session + `POST /v1/agent/fill/request` + CDP `veilAgentic`) |

We do not need a marketplace listing per competitor to win **developers and small teams** — we need import, onboarding, MCP, and honest docs per host ([grok.md](./grok.md), [onboarding.md](./onboarding.md)).

Partnership queue (when ready): same class as 1P — **Grok Bot Password Managers**, **Muse**, **OpenAI**, **Anthropic** native connectors. Engineering prerequisite for each is unchanged: HITL + fill-outside-model, never secret in context.
