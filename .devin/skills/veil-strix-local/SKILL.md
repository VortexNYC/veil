---
name: veil-strix-local
description: Veil repo — local Devin + self-hosted Strix auth pentest. OSS CLI only; no Strix Cloud; no production veil.nyc. See also ~/.agents/skills/devin-strix-local/SKILL.md.
---

# Veil + Strix (this repo)

When working in **VortexNYC/veil**, follow **`devin-strix-local`** (`~/.agents/skills/devin-strix-local/SKILL.md`).

Quick context:

- Default bar: `make security-fast` at repo root.
- Strix targets: `internal/publicapi`, `internal/human`, `internal/dpop`, `identity/glue`, `internal/mcpserver`, `apps/vault/src` (auth UI only).
- LLM env: `~/.config/vortex/strix.env` — never in git.
- Output: `strix_runs/` — do not commit.

Project skill duplicates the global playbook so Devin discovers it from `.devin/skills/` when cwd is this repository.
