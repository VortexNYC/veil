# Key rotation & KEK escrow

The key hierarchy, top to bottom:

| Key | Lives where | Seals | Rotated by |
|---|---|---|---|
| `VEIL_KEK` | deployment env (Railway) | every `org_keys` row, AAD-bound to the org | `veil key rotate-kek` |
| org master | `org_keys` row (wrapped) | every `owner_keys` row for the org | `veil key rotate-org ORG` |
| owner DEK | `owner_keys` row (wrapped) | that owner's item ciphertexts | never — DEKs don't rotate, the wrap does |
| item ciphertext | `items.secret` | the secret itself | n/a |

Item ciphertexts seal under owner DEKs. Rotating an org master **rewraps the
DEKs** — it never re-seals items, so rotation is O(#owners), not O(#items).
Rotating the KEK **rewraps the masters** — DEKs and items don't move at all.

## Escrow (VEIL-8)

A database backup without the KEK that sealed its `org_keys` rows is
inert ciphertext — proven by `TestPostgresBackupRestoreDrill`. Escrow is
therefore not optional; it is the difference between a backup and a brick.

- **Current KEK:** `VEIL_KEK` env on every origin replica, hex-encoded
  32 bytes. Generate with `openssl rand -hex 32` or `veil gen`.
- **Escrow target:** store the KEK in a system that is not the database —
  the same blast radius must not cover both. Railway variables alone are
  not escrow (losing the project loses the key *and* its history). Use a
  second provider's secret store, or a sealed offline copy.
- **Prior KEKs:** after `rotate-kek`, the old KEK opens nothing — every row
  was rewrapped in one transaction. Escrow only needs the *current* KEK.
  Keep the old one long enough to verify the rotation committed (one
  `Secret()` read under the new KEK), then it may be destroyed. It is not
  needed for restore.
- **Recovery order:** restore the database first (see
  `docs/backup-restore.md`), *then* set `VEIL_KEK` to the escrowed value.
  The wrong order is harmless — unwraps just fail closed — but the wrong
  KEK is indistinguishable from corruption at read time, so verify escrow
  against a known org before you need it.

## Procedures

Both verbs need `--dsn`/`VEIL_POSTGRES_DSN` and the current KEK as hex via
`VEIL_KEK` or `--kek-file`. Keys never travel in argv.

### Org master rotation (`veil key rotate-org ORG`)

When: scheduled hygiene, suspected org-key exposure, or before offboarding
an owner whose devices are all suspect.

1. `veil key rotate-org aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa`
   - One transaction: mint new master → rewrap every `owner_keys` row →
     `key_version++` (guarded: a concurrent rotation loses with
     `ErrRotationConflict` — retry).
2. **Redeploy every origin replica.** Key caches are per-process; only the
   rotating process knows the master changed. Replicas keep serving under
   stale masters until restart — writes they make are still correct (DEK
   wraps re-resolve per mint under generation guards), but bounded staleness
   is the contract.
3. Verify: read one item through the API; check `org_keys.rotated_at`.

Rollback: there is none by design — a rotated master is a *better* master.
If rotation commits but a replica wedges, restart it. If rotation aborts
mid-transaction, nothing committed; rerun the verb.

### Deployment KEK rotation (`veil key rotate-kek`)

When: KEK suspected-compromised, or scheduled. `VEIL_KEK_NEW` (or
`--new-kek-file`) holds the new key.

1. Escrow the new KEK **before** rotating. After commit, the old KEK opens
   nothing — losing the new one mid-procedure is the only unrecoverable
   moment.
2. `veil key rotate-kek`
   - One transaction: unwrap each `org_keys` row under the old KEK → rewrap
     under the new (AAD stays the org id). Any row that fails to unwrap
     aborts the entire rotation.
   - `cmk_id` rows are skipped — an external CMK owns its own wrap.
3. Set `VEIL_KEK` to the new value on every replica and redeploy **in the
   same deploy**. Mixed-KEK replicas fail closed: old-KEK replicas unwrap
   nothing and error every read — correct but down. This is the one
   rotation that requires a coordinated restart.
4. Verify: `Secret()` reads on two orgs; then destroy the old KEK's escrow.

### Lost KEK (incident)

If the KEK is lost and not escrowed: `org_keys` rows cannot be unwrapped,
org masters are unrecoverable, and every item ciphertext in the vault is
permanently sealed. There is no backdoor — that is the product. The
recovery path is owner-level: owners re-add secrets under a freshly
provisioned org key. This is why escrow is step 0, not an afterthought.

### Compromised KEK (incident)

1. Mint and escrow a new KEK. 2. `veil key rotate-kek`. 3. Then
`rotate-org` for every org — a stolen KEK plus a stolen dump yields every
org master, so the masters must move too. Order matters: KEK first (so the
new wraps land under a clean key), then orgs.

## What rotation does not do

- Does not re-seal item ciphertexts (by design — see the table).
- Does not version ciphertext AAD — `key_version` exists on `org_keys` for
  the epoch-stamped-AAD work (VEIL-15), not used yet.
- Does not give agents anything. Rotation is store-level; no secret or key
  material crosses the HTTP boundary.
