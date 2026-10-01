# KEK escrow

Every durable asset is ciphertext under `VEIL_KEK`. The database can be
rebuilt from R2 + runbooks; the KEK cannot. Today it exists in exactly two
places — `~/.config/vortex/veil-kek` (mode 600, founder's Mac) and the
`Veil KEK (escrow)` item in the agents' 1Password vault. That is two
copies but one operator: the vault is unrecoverable if that operator is
unavailable. This is the last single-point-of-person in the system.

## The custody decision — DECIDED 2026-10-01

**Emergency Kit (physical custody ×2), Shamir deferred to VEIL-77.**
Following the 1Password model: their answer to "what if I lose everything"
isn't splitting — it's a printed Secret Key (Emergency Kit) in a second
physical location. The analog for an infra-level KEK:

- `~/.config/vortex/veil-kek-emergency-kit.txt` (mode 600) — print-ready
  artifact with the KEK + restore pointer. Founder prints **two sealed
  copies in two physical locations** (e.g. office safe + safe-deposit box),
  then shreds/deletes the file if desired — the digital copy already lives
  in the two existing escrow points.
- Shamir 2-of-3 ships inside the rotation ceremony:
  `veil key rotate-kek --generate --shares 3 --threshold 2 --shares-dir D`
  mints the new KEK, stages `share-<i>-<fp>.hex.tmp` (mode 600, dir 700),
  commits the atomic rewrap, and only then finalizes shares — a share of
  a key that never rotated can never look real. The 8-hex `<fp>` is a
  sha256 prefix of the key: it binds a directory to ONE ceremony, so
  `veil key combine` refuses mixed or foreign share files instead of
  interpolating a wrong key. `--generate` requires `--shares` — a minted
  KEK must be born split, never rotated into a vacuum. Splitting uses
  `openbao/openbao/sdk/v2/helper/shamir` (MPL-2.0 — the prior-art doc
  forbids the HashiCorp Vault import).
  `veil key combine --shares-dir D --out F` reconstructs to a mode-600
  file (existing files are chmod'd, never left permissive). The assembled
  key exists only to be typed into Railway VEIL_KEK; custody is the shares.

## Org-level recovery (product, separate layer — filed VEIL-78)

The KEK protects against losing *infra*. The other half of the 1P model —
org survives losing a *member* — is a product feature: org vault keys
wrapped per-member recovery keypair, organizer re-wraps to a recovered
member's new keypair. Requires members to hold a persistent client-side
recovery secret (issued at join), which Veil doesn't have today — Kratos
sessions are server-issued. Filed as its own ticket; not ops custody.

## Ceremony (whichever lands)

1. Generate/split the KEK copy in a single session; never email, never
   chat, never unencrypted sync. Paper or the custodian's own password
   manager only.
2. Each custodian verifies custody WITHOUT revealing their share: hash the
   share/kek (sha256) and confirm it matches the value recorded at split
   time. For the full key, the live proof is `drillrestore` against a
   scratch restore — same check as the quarterly drill.
3. Record in this file: date, custody form (full key / share i-of-n),
   holder identity, storage location class (not the address).
4. Re-verify at the quarterly DR drill: each holder confirms possession by
   hash. A holder who cannot produce the share is treated as compromised —
   run `veil key rotate-kek --generate --shares 3 --threshold 2` to rotate
   to a KEK the missing share can no longer help reconstruct, update
   VEIL_KEK on every replica, and redistribute shares. Never re-share the
   same key.

## Rules that do not bend

- KEK never transits argv, logs, tickets, or chat — stdin/files mode-600.
- Shares never co-locate: different physical locations AND different
  failure domains (no two shares in one safe).
- Escrow content is recorded here as custody metadata only — never the
  key bytes.

## Custody log

| Date | Form | Holder | Location class | Verified |
|---|---|---|---|---|
| 2026-09-23 | full key | founder | local escrow file + 1P vault | unwrap drill pass |
| 2026-09-30 | full key | founder | (same — verified live) | drillrestore PASS |
| 2026-09-30 | env snapshot | founder | `~/.config/vortex/veil-env-escrow.json` + 1P `Veil prod env (escrow)` | VEIL-73 drill — SECRETS_SYSTEM truncated entry found & fixed |
| 2026-10-01 | emergency kit | founder | `~/.config/vortex/veil-kek-emergency-kit.txt` → 2 physical copies | pending print |

Note the escrow now covers more than the KEK: `veil-env-escrow.json` is a
full per-service var snapshot (235 vars, 11 services) including kratos
cipher/cookie secrets, hydra `SECRETS_SYSTEM` (the full rotation list —
verified 2026-09-30 after the drill caught a 48/97-char truncation),
hydra pairwise salt, courier/resend, glue bootstrap, billing vars.
Re-snapshot whenever a prod var changes; treat it as equal-weight to the
KEK — it contains the KEK too.
