# Scale model — origin, Postgres, PgBouncer

What `/v1/use` costs, how connections budget, and what scales horizontally
vs. what is per-process. Baselines are measured (`PERF.md`); projections are
arithmetic from those numbers, not marketing.

## Request geometry

One `POST /v1/use` performs exactly **4 synchronous DB round trips** plus an
asynchronous audit write:

| Query | /request | Mean | Notes |
|---|---|---|---|
| `sessions` resolve | 1 | ~0.015 ms | token hash → session row |
| `UseAuth` (LEFT JOIN agents/items/grants/approvals) | 1 | ~0.034 ms | one-shot authz snapshot |
| `agents` reload | 1 | ~0.014 ms | final check before secret access |
| `items` secret | 1 | ~0.014 ms | ciphertext fetch |
| `audit` COPY | ~0.017 | ~1.07 ms | async, ~40–60 rows/batch |

Session consumption (`uses` increment) is atomic in the `sessions` update —
two replicas racing a `max_uses` session cannot both succeed. Everything
authorization-critical is a single-statement read or a single tx; there is
no cross-replica in-memory coordination to break.

Measured baseline (3 replicas, 150 VUs, direct origin, local Docker pg):
**8,724 req/s**, avg 7.69 ms, p95 22.64 ms, 0% errors. pprof shows the CPU
is dominated by network/runtime wait states, not query execution — the
database is not the limiter at this scale.

## Connection budget

Per replica: `main pool + audit pool` backend connections.

| Knob | Default | Effect |
|---|---|---|
| `pool_max_conns` in DSN | — | main pool size (pgx parses it) |
| `VEIL_PG_MAX_CONNS` | 20 | main pool; **wins over DSN** (ops resize without a DSN change) |
| `VEIL_PG_MIN_CONNS` | 2 | warm floor |
| `VEIL_PG_AUDIT_CONNS` | 2 | audit COPY pool — deliberately separate so audit never queues behind request-path reads |

Budget: `replicas × (VEIL_PG_MAX_CONNS + VEIL_PG_AUDIT_CONNS) ≤ Postgres
max_connections − headroom` (migrations, `veil key` ops verbs, psql —
reserve ~10). Default replica: 22 conns; a stock `max_connections=100`
Postgres fits ~4 replicas comfortably; beyond that you need PgBouncer, not
bigger max_connections (each backend is ~10 MB RAM and context-switch cost).

Server-side guards already set per connection: `statement_timeout=5000`,
`idle_in_transaction_session_timeout=30000` — a wedged client cannot hold a
backend forever.

## PgBouncer

Run it in **transaction pooling** (`pool_mode = transaction`) — sessions are
only ever held for the duration of one query or one explicit tx, and the
code holds no session state. Verified: no `LISTEN/NOTIFY`, no advisory
locks, no temp tables, no `SET SESSION`, no prepared statements that
outlive a query.

Requirements on the origin side:

- **Disable pgx prepared-statement caching.** Cached statements are
  per-backend; transaction pooling moves you between backends. Add
  `default_query_exec_mode=exec` to the DSN (or `prefer_simple_protocol=true`).
- `search_path`-style startup parameters must come through the DSN or
  PgBouncer's `ignore_startup_parameters` — anything set per-session leaks
  across transactions otherwise. Prod code never depends on it; tests do
  (schema isolation), and tests don't run through PgBouncer.
- `FOR UPDATE` (recovery-wrap single-use) and the `RotateOrgKey`/`RotateKEK`
  transactions are tx-scoped — safe under transaction pooling by
  definition.
- PgBouncer's own `default_pool_size` replaces per-replica `MaxConns` as
  the real budget; set `VEIL_PG_MAX_CONNS` ≈ pool size anyway so replicas
  don't queue on PgBouncer itself.

Topology: `replicas → PgBouncer (transaction) → Postgres`. The audit pool
can route through the same PgBouncer; its 2 conns/replica stay separate in
the app either way.

## What is per-process (the scale-out caveats)

Replicas are stateless except three caches/queues:

1. **keyManager** — unwrapped org masters + owner DEKs, per-process, with
   generation guards. `rotate-org`/`rotate-kek`/`reseed` invalidate the
   *local* cache only; other replicas serve stale masters until restart.
   This is why `docs/key-rotation.md` requires redeploy-after-rotate.
2. **audit.Async queue** — in-memory buffer flushed every
   `VEIL_AUDIT_FLUSH_INTERVAL` (default 5 ms; 20 ms batches ~43 rows/COPY
   and was +4% throughput in runs). A process crash loses up to one flush
   interval of queued audit events — the audit write is durable once
   committed; the queue is not. `Close()` drains on shutdown. Fail-closed
   is preserved: a Use never returns the secret without the audit event
   being committed *or* queued for commit — the gap is the flush window,
   documented, not hidden.
3. **`VEIL_MAX_IN_FLIGHT_USE` admission control** — per-replica in-flight
   cap; effective ceiling is `replicas × limit`, so keep it in sync with
   pool sizing (`in-flight > MaxConns` just queues inside pgx).

## Capacity arithmetic

A Use costs ~0.08 ms of DB time (4 RTs at measured means). A single
Postgres can therefore serve tens of thousands of Uses/s *on query time
alone* — the limiters in order are:

1. **Connections** — solved by PgBouncer above ~4 replicas.
2. **Round-trip latency** — dominates p50 (7.69 ms avg vs 0.08 ms DB); it is
   network + HTTP + TLS + crypto, not the query.
3. **Audit write throughput** — COPY batches amortize it; at 8.7k req/s the
   audit pool wrote ~17k batches (≈1 ms each) — ~18 s of serial COPY per
   2-minute run spread over 2 conns. Audit keeps up until COPY latency ×
   batch rate saturates the audit conns; then the async queue grows —
   watch queue depth, not rows/s.
4. **Lock contention** — `sessions` row updates serialize on the session
   row (per-agent, fine); `org_keys`/`owner_keys` only write on rotation.

Users and agents multiply *rows*, not per-request cost: a million items
does not change the 4-RT geometry because every hot-path read is
PK/UNIQUE-indexed (`UseAuth` joins on `agent_id+item_id` UNIQUE,
`sessions` by hash, `items` by PK). Table-size pressure lands on `audit`
(VEIL-4: partitioning/retention) and `sessions` expiry cleanup
(`veil sweep`), not on Use latency.

## Failure semantics under scale

- Pool exhaustion → requests queue inside pgx up to `statement_timeout`;
  they do not fail-open.
- Postgres unreachable → `retryOnDeadConn` retries dead-connection errors;
  authz still fails closed on real errors.
- Half-deployed rotation → mixed-KEK replicas fail closed on unwrap
  (correct-but-down); mixed *org-master* replicas keep working because
  DEK wraps re-resolve under the generation guard — the stale-master risk
  is bounded to the redeploy window, documented in key-rotation.md.
