# Key-Service KEK Rotation Runbook

The key-service (`keys.veil.nyc`, `apps/key-service`) holds org KEKs in the
**Secrets Store** (`965d27844ea34c2d9ad83eb61b372dac`), not Worker secrets.

## Critical: two different secret mechanisms

- `wrangler secret put NAME` writes a **legacy Worker secret** — `env.NAME`
  is a string. The key-service does NOT read these.
- `wrangler secrets-store secret <create|update> <store-id> --name NAME
  [--secret-id ID] --value HEX --remote` writes a **Secrets Store secret** —
  the Worker reads it via `env.NAME.get()` (async). This is the only
  mechanism that affects the key-service.

A `wrangler secret put` on this service silently writes a value nothing
reads — auth or key ops keep using the store value and fail confusingly.

## /whoami — verify bound slots before ANY write op

`GET https://keys.veil.nyc/whoami` returns
`{"keys":{"kek":bool,"kek_next":bool}}` — slot presence only, never values.
Run it before and after every secret mutation. Normal state:
`kek:true, kek_next:false`. Rotation window: both true.

## Rotation ceremony (rotate-kek-remote)

1. Mint NEXT and **persist it immediately** to a mode-600 file — the value
   is needed for the promote step and is unrecoverable from the store:
   `openssl rand -hex 32 > /secure/kek-next.hex`
2. `wrangler secrets-store secret create <store> --name VEIL_KEK_NEXT
   --scopes workers --remote --value "$(cat /secure/kek-next.hex)"`
3. Ensure `VEIL_KEK_NEXT` is declared in `wrangler.jsonc`
   `secrets_store_secrets` and deploy the worker. `/whoami` must show
   `kek_next:true`.
4. `veil key rotate-kek-remote` — service rewraps every org_keys row under
   NEXT (ciphertext-only on the wire).
5. Promote: `wrangler secrets-store secret update <store>
   --secret-id <VEIL_KEK id> --value "$(cat /secure/kek-next.hex)" --remote`
   — sets VEIL_KEK to the SAME value NEXT had. Do NOT mint a new value at
   this step (rows are sealed under NEXT; a different value strands them).
6. Verify every org_keys row `/open`s, `veil item list` decrypts, THEN
   delete the NEXT secret + remove its binding + redeploy.
7. Shred the ceremony file.

## Incident notes (2026-10-29)

- Rotating a Secrets Store secret's VALUE destroys the old one — no
  read-back exists by design. A promote with a freshly-minted value strands
  every row sealed under the previous key.
- Recovery was only possible because a pre-rotation pg_dump existed and the
  old `VEIL_KEK` equaled the canonical deployment key in
  `~/.config/vortex/veil-kek`. If either were missing, masters are only
  recoverable via `org_key_shares`/`recovery_wraps`/device wraps.
- `org_keys_incident` snapshot pattern: copy the table before ANY key
  ceremony.
