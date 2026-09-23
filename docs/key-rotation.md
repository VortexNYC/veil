# Key rotation & KEK escrow

The key hierarchy, top to bottom:

| Key | Lives where | Seals | Rotated by |
|---|---|---|---|
| `VEIL_KEK` | deployment env (Railway) | every `org_keys` row, AAD-bound to the org | `veil key rotate-kek` |
| org master | `org_keys` row (wrapped) | every `owner_keys` row for the org | `veil key rotate-org ORG` |
| owner DEK | `owner_keys` row (wrapped) | that owner's item ciphertexts | never — DEKs don't rotate, the wrap does |
| recovery wrap | `recovery_wraps` row (wrapped) | a copy of the org master, sealed under **owner-held** material | owner re-mints; deleted on org rotation |
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
   - One transaction: `SELECT ... FOR UPDATE` on the org's `org_keys` row →
     mint new master → rewrap every `owner_keys` row → `key_version++`
     (guarded: a concurrent rotation loses with `ErrRotationConflict` —
     retry) → delete the org's recovery wraps.
   - The row lock serializes against owner-DEK mints and recovery-wrap
     stores (`FOR SHARE` readers), on **every** replica: a mint either lands
     before rotation (and gets rewrapped) or waits and seals under the new
     master. No wrap can commit under a master rotation just retired.
2. **Redeploy every origin replica.** Key caches are per-process; only the
   rotating process knows the master changed. Stale replicas self-heal on
   read — a wrap opened under the wrong master invalidates the org's cached
   keys and re-resolves — and new DEK mints seal under the *committed*
   master via the row lock, never a cached one. Still redeploy: bounded
   staleness is the contract, not the goal.
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
   - One transaction: `LOCK TABLE org_keys IN SHARE ROW EXCLUSIVE MODE` →
     unwrap each row under the old KEK → rewrap under the new (AAD stays
     the org id). Any row that fails to unwrap aborts the entire rotation.
   - The table lock blocks concurrent org inserts for the rotation's
     duration — an org provisioned mid-rotation on a same-process store
     waits on the KEK mutex instead and seals under the adopted key. On a
     *stale* replica (old `VEIL_KEK`, not yet redeployed) an insert can
     still land under the retired KEK after the lock releases — another
     reason the coordinated restart in step 3 is mandatory, not advisory.
   - `cmk_id` rows are skipped — an external CMK owns its own wrap.
3. Set `VEIL_KEK` to the new value on every replica and redeploy **in the
   same deploy**. Mixed-KEK replicas fail closed: old-KEK replicas unwrap
   nothing and error every read — correct but down. This is the one
   rotation that requires a coordinated restart.
4. Verify: `Secret()` reads on two orgs; then destroy the old KEK's escrow.

### Lost KEK (incident)

If the KEK is lost and not escrowed, `org_keys` rows cannot be unwrapped —
but the org is recoverable if an owner holds a recovery wrap
(`recovery_wraps`). Recovery material lives with the owner, not the
deployment, so it survives the loss:

1. Stand up the origin with a **new** `VEIL_KEK` (escrow it first).
2. `veil key recover-org ORG --owner-kind user --owner-id HUMAN --recovery-file FILE`
   — `RecoverOrgKey`: opens the wrap, re-seals the recovered master under
   the new KEK, and stamps `used_at` — **in one transaction**. A failed
   reseed cannot burn the wrap: wrong recovery material, a missing
   `org_keys` row, or a mid-transaction crash all leave the wrap intact
   for retry. Every pre-loss item ciphertext decrypts again — masters and
   DEKs never changed.
3. The owner immediately mints a fresh recovery wrap (`store-recovery`) —
   the old one is spent.

If no owner held a wrap and the KEK is not escrowed, the org is gone:
`org_keys` can't be unwrapped, every ciphertext stays sealed. There is no
backdoor — that is the product. Owners re-add secrets under a freshly
provisioned org key. This is why escrow is step 0 and why owners should
mint recovery wraps at onboarding, not after an incident.

### Owner recovery wraps (`veil key store-recovery` / `recover-org`)

A recovery wrap is a per-owner copy of the org master sealed under
owner-held recovery material — escrow that does not depend on the
deployment KEK or the database's availability guarantees.

- **Mint:** owner generates a 32-byte recovery key, keeps it offline
  (printed, hardware key, second vault — anywhere that is not this
  deployment), then `veil key store-recovery ORG --owner-kind user
  --owner-id HUMAN --recovery-file FILE [--expires 720h]`.
- **Semantics:** one wrap per owner; re-minting replaces it. Opening is
  **single-use** — first success stamps `used_at`, and replays, concurrent
  opens, wrong material, wrong owner, and expiry all fail closed. The wrap
  is AAD-bound to org+owner: a row copied to another principal opens
  nothing.
- **Rotation interaction:** `rotate-org` **deletes** the org's recovery
  wraps — a wrap that opens a dead master is a trap. Minting takes `FOR
  SHARE` on the `org_keys` row inside the same transaction, so a wrap can
  never commit a master rotation just retired. Owners re-mint after every
  org rotation.
- **Use:** `recover-org` (above) consumes the wrap and re-seeds the org
  atomically. Composing `OpenRecoveryWrap` + `ReseedOrgKey` by hand spends
  the wrap on the first commit — don't.
- **No plaintext, ever:** the recovery key never persists; the master never
  leaves the store unwrapped at rest.

## Concurrency model

One rule orders everything: **`kekMu` is always the outermost lock.** Every
verb that reads or writes `p.kek` acquires it before beginning any
transaction or touching a row/table lock; `rotate-kek` takes it exclusively
(`Lock()`), everything else shared (`RLock()`). A caller can therefore never
hold a Postgres lock while waiting on the mutex — the cycle that would
deadlock a `FOR SHARE` reader against `rotate-kek`'s table lock cannot
form.

At the database layer, one subtlety drives the design: `SELECT ... FOR
UPDATE` takes only `ROW SHARE` on the table — `ROW EXCLUSIVE` arrives at
the first `UPDATE`. Any verb that both locks an `org_keys` row and later
writes it therefore takes `LOCK TABLE org_keys IN ROW EXCLUSIVE MODE` up
front. Without it, the mid-transaction upgrade deadlocks against
`rotate-kek`'s `SHARE ROW EXCLUSIVE` (it holds the table lock while
waiting on the row lock; proven by `TestPostgresKEKVsOrgRotateCrossStore`,
which caught the deadlock the naive design produced).

- `rotate-org`, `recover-org`, `ReseedOrgKey`: `ROW EXCLUSIVE` table lock
  → `FOR UPDATE` row lock → work. `ROW EXCLUSIVE` doesn't self-conflict,
  so rotations on *different* orgs still run concurrently; same-org
  serializes on the row lock.
- DEK mints and recovery-wrap mints take `FOR SHARE` on the org row —
  compatible with `ROW EXCLUSIVE` at the table level, conflicting at the
  row level, so they serialize against `FOR UPDATE` writers exactly as
  intended while never blocking each other.
- `rotate-kek` takes `SHARE ROW EXCLUSIVE` on the whole `org_keys` table;
  inserts and all lock-taking verbs serialize for its duration.
- Lock order across tables is `org_keys` → `recovery_wraps` everywhere,
  matching `rotate-org`'s row-lock-then-delete shape.
- Cache correctness is generation-tagged per org in-process; across
  replicas the committed row is authoritative — a stale replica that fails
  an unwrap invalidates its cached keys and re-resolves rather than
  serving a wrong key.

### Compromised KEK (incident)

1. Mint and escrow a new KEK. 2. `veil key rotate-kek`. 3. Then
`rotate-org` for every org — a stolen KEK plus a stolen dump yields every
org master, so the masters must move too. Order matters: KEK first (so the
new wraps land under a clean key), then orgs.

## What rotation does not do

- Does not re-seal item ciphertexts (by design — see the table).
- Does not bump ciphertext epochs — `items.secret` AAD binding is the
  `VEIL1` marker (see *Ciphertext epochs*), orthogonal to `key_version`.
- Does not give agents anything. Rotation is store-level; no secret or key
  material crosses the HTTP boundary.

## Ciphertext epochs (AAD binding)

`crypto.SealEpoch`/`OpenEpoch` in `internal/crypto/box.go` own the format.
A stored blob is either:

- **Legacy (epoch 0/1)** — `nonce||ct`, sealed nil-AAD. Readable forever,
  no longer written.
- **Epoch 2 (`VEIL1`)** — `VEIL1||nonce||ct(AAD-bound)`. The 5-byte marker
  makes a legacy nonce colliding with it a ~2^-40 event, so the marker is
  authoritative, not advisory: a marked blob that fails its AAD open errors
  — there is no legacy fallback for marked bytes.

AAD formats live in `internal/store/aad.go`, domain-tagged so a blob from
one row type can never satisfy another's context:

- `items.secret` → `veil/item/v1 · org_id · item_id`
- `owner_keys.wrapped` → `veil/ownerkey/v1 · org_id · owner_kind · owner_id`
- `item_versions.secret` → byte-copies of `items.secret`, same item binding
- `org_keys.wrapped` → already `org_id`-bound via `SealAAD` (epoch-1 AAD
  predates the marker — left as-is)
- `replica/vault.go` — stays nil-AAD: the vault file is one sealed blob,
  there are no rows to transplant between

Migration is **lazy at the write edge**: `PutItem` and every DEK/wrap mint
or re-seal write `VEIL1`; reads dispatch on the marker. `RotateOrgKey`
converges `owner_keys` to `VEIL1` as a side effect (it re-seals every wrap).
Legacy item ciphertexts are never mass-rewritten — a `PutItem` re-seals
that row; `RestoreVersion`/`SnapshotItem` copy the blob verbatim and keep
whatever epoch it was sealed at; untouched rows stay readable.

The security property: a `VEIL1` blob transplanted to a different `item_id`,
`org_id`, or owner row fails closed (`crypto.ErrAuth`). A *legacy* blob
still transplants freely — that is exactly the gap the epoch closes, which
is why new writes are always marked.
