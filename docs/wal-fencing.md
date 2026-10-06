# WAL fencing — why two archivers can never corrupt the archive

Status: **implemented design record** (VEIL-74). Audience: the operator
touching `veil-wal` / `veil-wal-dr`, failover, or a second Postgres.

The WAL archive is the PITR leg of the backup plane. Its correctness rule
is unusual: **the archive does not need a single writer — it needs that no
writer can ever move an object backwards.** Everything else follows from
three object-level invariants, enforced server-side in
`apps/backup-ingest`, not by the archivers being well-behaved.

## Topology

| Piece | Owns | Region | Slot |
|---|---|---|---|
| `veil-wal` | streams + ships | sfo | `wal_archive` |
| `veil-wal-dr` | streams + ships | iad | `wal_archive_dr` |
| `backup-ingest` worker | fences writes | Cloudflare | — |
| R2 `veil-backups` | the archive | offsite | — |

Both archivers run the *same* `scripts/wal-archive.sh`; only `WAL_SLOT`
differs. A replication slot is single-writer — the slot name is the
archiver's identity, and slot + service are paired in `.railway/railway.ts`,
not in the script.

## The three invariants

1. **Namespace fencing — `arc-<system_identifier>/`.** Every object lives
   under the source cluster's sysid. A rebuilt cluster gets a fresh sysid,
   so its `wal-*`/`base-*` names can never collide with or overwrite the
   dead cluster's archive. Restore picks the sysid embedded in the chosen
   base backup's name; the runbook enumerates `arc-*` prefixes.

2. **Immutable completed objects.** Completed WAL segments, `.backup`
   labels, and `.history` files are content-addressed by name — the worker
   accepts an identical re-PUT (idempotent retry → 200) and refuses a
   different-size collision (409). Two archivers pushing the same segment
   write the same bytes; last-write-wins is safe because the winner is
   byte-identical.

3. **Monotonic `.partial` — etag CAS.** The in-flight segment is the only
   mutable object. The worker compares sizes: a `.partial` PUT shorter than
   the stored object gets 409 (the archiver treats that as benign — a peer's
   copy is strictly newer); a concurrent same-length race loses with 503
   and retries. A partial can only grow. The real RPO bound is this lag,
   proven in the 2026-10-01 drill: ~15s behind the live tail.

   **Push cadence is the egress budget.** A `.partial` PUT re-uploads the
   whole in-flight file (preallocated 16MB), so pushing on every content
   change billed ~250MB/hr per archiver on an idle database — the 15s
   heartbeat write alone was enough churn to re-trigger it. Since
   2026-10-06 the shipper pushes `.partial` at most once per
   `WAL_PARTIAL_INTERVAL` (default 300s) or when it has grown
   `WAL_PARTIAL_MIN_GROWTH` (default 1MB) since the last push; completed
   segments are unaffected and ship on sight. RPO tail: ≤5min idle,
   ~15s under write load.

## Slot ownership

- `pg_receivewal --create-slot --if-not-exists` — the slot is pinned to the
  service, survives the service being down (`max_slot_wal_keep_size` caps
  what a dead archiver pins), and its confirmed LSN is where streaming
  resumes.
- Two processes on the *same* slot cannot attach — Postgres refuses the
  second receiver, so a same-slot misconfig degrades to a flapping
  reconnect loop, not corruption. The monitor pages on it (no active slot).
- A slot that overflowed its keep cap reports `wal_status=lost`; the script
  drops and recreates it, streaming resumes from current LSN, and the beat
  gap + `lost` status both page — the gap in archived WAL is real and loud.

## Failure matrix

| Failure | What happens | Data loss |
|---|---|---|
| One archiver host dies | Its slot retains WAL server-side; the other archiver keeps the archive current. Beat gap pages on the per-slot beat (`wal-archive:<slot>`). | none |
| Both archivers die, Postgres alive | Slots retain WAL up to `max_slot_wal_keep_size`; archive stalls, both beats + `no active slot` page. Catch-up on restart streams retained WAL. | none while slots hold; overflow → `lost` pages |
| Postgres dies | WAL is already offsite to the last `.partial` push. Both archivers die with it. Restore = last base + archived WAL. | ≤ `WAL_PARTIAL_INTERVAL` (300s idle; growth-bounded under load) |
| Cluster rebuilt (new sysid) | Archivers reconnect, read the new sysid, write under a new `arc-` namespace. Old archive untouched; restore picks the correct namespace per base backup. | pre-rebuild data is the old namespace's archive |
| Replica promoted (same sysid, new timeline) | Timeline digit in the WAL name changes; `.history` objects are archived too; recovery follows the timeline history. No name collisions across timelines. | ≤ push interval |
| Archiver behind (slow region) | Its `.partial` PUTs lose the CAS (409) and are skipped; its complete segments are identical bytes. A slow writer can only ever be ignored. | none |

## The one thing the archive assumes

Postgres itself stays single-writer. If a split-brain ever produced two
primaries with the same sysid writing WAL, both archivers could stream
divergent timelines into one namespace — Postgres solves this at failover
with timeline switching, and the `.history` objects make the winning
branch explicit at restore. There is no scenario where a shorter or older
object overwrites a longer or newer one; divergence shows up as extra
objects, never as lost ones.

## Monitoring contract

- `wal-archive` beat — some shipper is alive (backstop).
- `wal-archive:<slot>` beat per slot in `pg_replication_slots` — *this*
  archiver's shipper loop is alive. A dead primary invisible behind the
  DR standby was the pre-VEIL-74 gap; the per-slot row closes it.
- `WalSlots` — at least one slot active, none `lost`, slots exist at all.
- Stale beat > `--wal-stale` (10m) or any `lost` slot pages once per
  `--alert-cooldown` through `veil monitor`.

Drill proof: `docs/backup-restore.md` — 2026-10-01 PITR drill replayed the
archived stream to a `recovery_target_time` and hit it exactly; the
weekly `scripts/drill-restore.sh` re-proves the dump leg.
