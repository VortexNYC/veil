# KEK escrow

Every durable asset is ciphertext under `VEIL_KEK`. The database can be
rebuilt from R2 + runbooks; the KEK cannot. Today it exists in exactly two
places — `~/.config/vortex/veil-kek` (mode 600, founder's Mac) and the
`Veil KEK (escrow)` item in the agents' 1Password vault. That is two
copies but one operator: the vault is unrecoverable if that operator is
unavailable. This is the last single-point-of-person in the system.

## The custody decision (founder's call)

Pick one — ordered by effort:

**A. Second human, full key.** A printed/recorded copy held by counsel or
a second officer in their own safe/vault. Cheapest, weakest against
collusion-free misuse — a full copy under someone else's mattress is a
credential theft vector. Fine if the custodian is a trusted officer.

**B. Split knowledge — 2-of-3 Shamir.** Split the 32-byte KEK into three
shares; any two rebuild it. Shares: founder (1Password/paper), counsel or
second officer, third party (safe-deposit box, or a hardware token in a
different physical location). No single share is a credential; theft of
any one share yields nothing. `ssss-split`/`ssss-combine` is the standard
tool; do not hand-roll a splitter. This is the right answer for an auth
product once it has >1 employee.

**C. Cloud KMS escrow.** Encrypt the KEK under a KMS key in a *different*
provider/account than everything else (e.g. a dedicated AWS account nobody
else touches), store the ciphertext in the repo-adjacent vault of record.
Cross-provider failure domain; adds a vendor dependency and a recurring
cost. Good as a *third* leg, not the only one.

Recommended: **B**, or A-then-B if there is no second officer yet — even a
documented interim custodian beats none.

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
   NOTE: KEK rotation is not implemented today (re-wrapping every
   `org_keys` row under a new KEK is its own work item). A compromised
   share therefore means: assume the KEK could be read, re-cut the escrow,
   and schedule the rotation feature — do not silently re-share the same
   key.

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

Note the escrow now covers more than the KEK: `veil-env-escrow.json` is a
full per-service var snapshot (235 vars, 11 services) including kratos
cipher/cookie secrets, hydra `SECRETS_SYSTEM` (the full rotation list —
verified 2026-09-30 after the drill caught a 48/97-char truncation),
hydra pairwise salt, courier/resend, glue bootstrap, billing vars.
Re-snapshot whenever a prod var changes; treat it as equal-weight to the
KEK — it contains the KEK too.
