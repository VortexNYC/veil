# Veil performance ledger

This file records measured performance for the Postgres-backed origin and the
optimizations we try. Numbers are from a local 2023 Apple Silicon Mac with a
Docker Postgres 16.15 container (`pg-pm-test`) and the `cmd/loadtest` harness.
They are not a production capacity guarantee; they are a repeatable baseline for
comparing changes.

## Methodology

- Target endpoint: `POST /v1/use`.
- k6 script: `tests/load/k6/use.js`.
- Origin: one or three stateless replicas started by `cmd/loadtest`, backed by
the same Postgres DSN.
- Proxy: a local round-robin `httputil.ReverseProxy` inside the harness.
- Upstream: a local `/ok` handler returning `{"ok":true}`.
- Audit: asynchronous via `internal/audit.Async`, flushed to Postgres with
`AppendAudits` (COPY).
- Per-request `slog.Info("use")` logs are suppressed (`VEIL_LOG_LEVEL=warn`)
during the run to avoid log I/O becoming the measured limiter.
- `pg_stat_statements` is enabled; the harness resets it before the workload
starts so the `pgbot-after` snapshot contains only the current run.
- Origin replicas are stopped and their async auditors are closed before the
final pgbot snapshot, so `pg_stat_statements` includes every audit `COPY`.
- pgbot snapshots are captured after each run with
`pgbot inspect --format json --fail-on none`.
- The harness captures a 120s CPU profile (`cpu.pprof`) and a heap snapshot
(`heap.pprof`) under `tests/load/k6/out/<run>/`.

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `PG_TEST_DSN` | required | Postgres DSN for the harness |
| `LOADTEST_ORIGIN_MODE` | `goroutine` | `goroutine` (in-process), `process` (child `veil mcp` replicas), or `external` (URLs in `LOADTEST_ORIGINS`) |
| `LOADTEST_ORIGIN_BINARY` | auto | path to the `veil` binary for `process` mode; auto-built if unset |
| `LOADTEST_ORIGINS` | `""` | comma-separated external origin URLs for `external` mode |
| `LOADTEST_PROXY` | `1` for `goroutine`, `0` otherwise | use the local round-robin `httputil.ReverseProxy` |
| `LOADTEST_UPSTREAM_URL` | `""` | externally reachable upstream; a local `/ok` server is started if unset |
| `LOADTEST_RESET_DB` | `0` | when `1`, truncates load-test tables before seeding (destructive; test DB only) |
| `LOADTEST_REPLICAS` | 1 | origin replica count |
| `VEIL_VUS` | 50 | k6 virtual users |
| `VEIL_AUDIT_FLUSH_INTERVAL` | **removed** (§11) | async audit is gone from the origin path; env no longer read |
| `VEIL_MASTER_KEY` | generated if unset | 64-hex master key for the origin; generated when empty. Required for `external` mode and must match the key used by the external origins. |
| `VEIL_LOG_LEVEL` | warn | suppress per-request INFO logs during benchmarks |
| `LOADTEST_OUT` | `tests/load/k6/out` | artifact directory for k6, pgbot, and pprof output |
| `VEIL_MAX_IN_FLIGHT_USE` | **100** in `veil mcp` child processes; 0 (unlimited) only for in-process `goroutine` origins | per-origin in-flight `Use` limit; must be set explicitly for `process`-mode benchmarks or 150 VUs shed at 3×100 capacity |

## Results

### 1. Baseline: 1 replica, `VEIL_AUDIT_FLUSH_INTERVAL=5ms`

- Requests: **579,511**
- Throughput: **4,829.2 req/s**
- Latency: avg **4.60 ms**, med **4.42 ms**, p95 **8.29 ms**, max **114.10 ms**
- Errors: **0%**
- Cache hit ratio: **1.0**
- Deadlocks: **0**
- No waiting or blocked connections
- Top DB work by `total_ms`:
  - `agents` lookup: 2,318,047 calls, 25,612 ms (4 per request)
  - `items` metadata lookup: 579,512 calls, 7,171 ms
  - `grants` lookup: 579,511 calls, 6,928 ms
  - `sessions` lookup: 579,511 calls, 6,801 ms
  - `items` secret lookup: 579,511 calls, 6,727 ms
  - `approvals` lookup: 579,511 calls, 5,545 ms
  - audit `COPY`: 16,088 calls, 4,586 ms, 579,511 rows (~36 rows/COPY)

### 2. Multi-replica: 3 replicas, `VEIL_AUDIT_FLUSH_INTERVAL=5ms`

- Requests: **628,634**
- Throughput: **5,238.6 req/s**
- Latency: avg **12.83 ms**, med **4.75 ms**, p95 **38.31 ms**, max **3.72 s**
- Errors: **0%**
- Cache hit ratio: **1.0**
- Deadlocks: **0**
- No waiting or blocked connections
- Top DB work by `total_ms`:
  - `agents` lookup: 2,514,539 calls, 30,089 ms (4 per request)
  - audit `COPY`: 38,434 calls, 12,636 ms, 628,074 rows (~16 rows/COPY)
  - `items` metadata lookup: 628,635 calls, 8,447 ms
  - `sessions` lookup: 628,634 calls, 8,317 ms
  - `grants` lookup: 628,634 calls, 8,194 ms
  - `items` secret lookup: 628,634 calls, 7,854 ms

### 3. Multi-replica: 3 replicas, `VEIL_AUDIT_FLUSH_INTERVAL=20ms`

- Requests: **654,059**
- Throughput: **5,449.5 req/s** (+4.0% vs 5ms)
- Latency: avg **12.33 ms**, med **4.87 ms**, p95 **38.15 ms**, max **813.1 ms**
- Errors: **0%**
- Cache hit ratio: **1.0**
- Deadlocks: **0**
- No waiting or blocked connections
- Top DB work by `total_ms`:
  - `agents` lookup: 2,616,239 calls, 31,311 ms (4 per request)
  - `sessions` lookup: 654,059 calls, 8,780 ms
  - `items` metadata lookup: 654,060 calls, 8,739 ms
  - audit `COPY`: 15,167 calls, 8,728 ms, 654,059 rows (~43 rows/COPY)
  - `grants` lookup: 654,059 calls, 8,491 ms
  - `items` secret lookup: 654,059 calls, 8,098 ms

### 4. Consolidated authorization reads + redundant agent fetch removal

Code changes:

- `Store.UseAuth` returns a single snapshot of agent + item + grant + approval
  in one query (SQLite `LEFT JOIN`, Postgres `LEFT JOIN`).
- `Broker.Use` uses `UseAuth` for the authorization decision, then reloads the
  agent once immediately before secret access.
- `App.UseFetch` no longer fetches the agent itself; `Broker.Use` owns the
  lookup.
- `cmd/loadtest` now resets `pg_stat_statements` before each run, captures
  `cpu.pprof` and `heap.pprof`, and removes the stale diagnostic print.

With a fresh `loadtest` DB, `pg_stat_statements` reset, and 20ms audit flush:

- Run A: 3 replicas, 150 VUs
  - Requests: **835,227**
  - Throughput: **6,960.0 req/s** (+27.7% vs the prior 5,449.5 req/s 3-replica 20ms baseline)
  - Latency: avg **9.64 ms**, med **3.42 ms**, p95 **27.72 ms**, max **1.96 s**
  - Errors: **0%**
  - Cache hit ratio: **1.0**
  - No waiting/blocked connections or deadlocks
- Run B: 3 replicas, 150 VUs, 5ms audit flush
  - Requests: **629,717**
  - Throughput: **5,247.6 req/s**
  - Latency: avg **12.80 ms**, med **3.84 ms**, p95 **34.53 ms**, max **923.92 ms**
- Run C: 1 replica, 50 VUs, 20ms audit flush
  - Requests: **618,791**
  - Throughput: **5,156.5 req/s**
  - Latency: avg **4.29 ms**, med **3.25 ms**, p95 **9.46 ms**, max **491.7 ms**

Clean `pgbot-after` (Run A) hot-path queries per request:

| Query | Calls | Mean (ms) | Total (ms) | /request |
|---|---|---|---|---|
| `UseAuth` (LEFT JOIN of agents/items/grants/approvals) | 835,227 | 0.0362 | 30,227.5 | 1 |
| `agents` (session resolution + final reload) | 1,670,454 | 0.0141 | 23,506.0 | 2 |
| `sessions` (session resolution) | 835,227 | 0.0155 | 12,973.8 | 1 |
| `items` secret lookup | 835,227 | 0.0145 | 12,141.3 | 1 |
| `audit` COPY | 14,606 | 1.0791 | 15,761.2 | 0.017 (batches) |

pprof (Run A CPU) top-line observations:

- **Network syscalls dominate CPU time**: `syscall.rawsyscalln` 54.38%,
  `runtime.pthread_cond_wait` 12.21%, `runtime.usleep` 11.35%,
  `runtime.kevent` 8.06%. These are runtime/network wait states and local
  loopback I/O, not DB query execution.
- `github.com/jackc/pgx/v5.(*Conn).Query` 22.72%,
  `internal/store.(*Postgres).UseAuth` 5.25%,
  `internal/store.(*Postgres).Agent` 9.21%,
  `internal/store.(*Postgres).Secret` 4.30%.
- `internal/app.(*App).PrincipalFromSession` 8.72% (session + agent lookup),
  `internal/publicapi.(*Server).useItem` 24.39% (full handler).

### 5. Direct-origin geometry + consolidated session resolution

Code changes:

- `cmd/loadtest` can bypass the local `httputil.ReverseProxy` with
  `LOADTEST_PROXY=0`. k6 is then passed a comma-separated `VEIL_ORIGINS` list
  containing every replica URL and distributes requests across them.
- `internal/publicapi/api.go` adds a `/v1/use` fast path for session tokens:
  `AgentFromSession` resolves the token hash to the session's `agent_id` without
  loading the agent. `Broker.Use` then performs the authoritative `UseAuth`
  snapshot and the final agent reload before secret access.
- `PrincipalFromSession` still resolves the agent for endpoints that need the
  full principal; only `/v1/use` skips the session-side agent lookup.

With 3 replicas, 150 VUs, `VEIL_AUDIT_FLUSH_INTERVAL=20ms`, and a fresh loadtest
DB:

| Run | Proxy | Requests | Throughput | avg | p95 | max |
|---|---|---|---|---|---|---|
| `3rep-20ms-proxy-session` | yes | 800,260 | 6,669.1 req/s | 10.07 ms | 29.02 ms | 3.03 s |
| `3rep-20ms-direct-session` | no | 1,046,804 | 8,724.5 req/s | 7.69 ms | 22.64 ms | 1.65 s |

Direct-origin vs. proxied: **+30.9% throughput**, **-23.6% avg latency**,
**-22.0% p95 latency**. This is the same workload and the same Mac; the only
difference is removing the `httputil.ReverseProxy` hop.

Hot-path DB queries (`3rep-20ms-direct-session`):

| Query | Calls | Mean (ms) | Total (ms) | /request |
|---|---|---|---|---|
| `UseAuth` (LEFT JOIN of agents/items/grants/approvals) | 1,046,804 | 0.0339 | 35,465.1 | 1 |
| `sessions` (session resolution) | 1,046,804 | 0.0152 | 15,878.0 | 1 |
| `agents` (final reload before secret) | 1,046,804 | 0.0137 | 14,350.1 | 1 |
| `items` secret lookup | 1,046,804 | 0.0139 | 14,595.4 | 1 |
| `audit` COPY | 17,351 | 1.065 | 18,479.2 | 0.017 (batches) |

`agents` calls dropped from 2 per request (post-`UseAuth` consolidated) to 1.
Per `Use`, the origin now performs exactly 4 synchronous DB round trips
(`sessions`, `UseAuth`, final `agents`, `items` secret) plus an async audit
`COPY`.

pprof (`3rep-20ms-direct-session` CPU) is still dominated by network/runtime
wait states (`syscall.rawsyscalln` ~50%, runtime waits ~30%), confirming the
database is not the primary limiter even when the proxy is removed.

### 6. Session-aware `UseAuth` join

Code changes:

- `Store.UseAuthSession` resolves a session by secret hash and joins the
  authorization decision (`sessions` → `agents` → `items` → `grants` →
  `approvals`) in a single query for Postgres. Memory and SQLite resolve the
  session and reuse the existing `UseAuth` path.
- `Broker.UseSession` calls `Store.UseAuthSession`, then performs the final
  `Store.Agent` reload and the rest of the `Use` flow.
- `App.UseFetchSession` hashes the session token and calls `Broker.UseSession`.
- `internal/publicapi/api.go` routes session-token `POST /v1/use` directly to
  `App.UseFetchSession`; OIDC tokens still use `App.UseFetch`.

With 3 replicas, 150 VUs, `VEIL_AUDIT_FLUSH_INTERVAL=20ms`, direct origin, and a
fresh loadtest DB:

- Requests: **1,392,393**
- Throughput: **11,603.3 req/s** (+33.0% vs the prior 8,724.5 req/s direct run)
- Latency: avg **5.77 ms**, med **2.33 ms**, p95 **16.12 ms**, max **804.5 ms**
- Errors: **0%**
- Cache hit ratio: **0.9995**
- No waiting/blocked connections or deadlocks

Hot-path DB queries (`3rep-20ms-direct-useauthsession`):

| Query | Calls | Mean (ms) | Total (ms) | /request |
|---|---|---|---|---|
| `UseAuthSession` (LEFT JOIN sessions/agents/items/grants/approvals) | 1,392,393 | 0.044 | 61,255.7 | 1 |
| `items` secret lookup | 1,392,393 | 0.0149 | 20,781.1 | 1 |
| `agents` final reload | 1,392,393 | 0.0146 | 20,339.6 | 1 |
| `audit` COPY | 22,484 | 0.9084 | 20,425.5 | 0.016 (batches) |

The separate `sessions` lookup is gone. Per `Use` there are now **3**
synchronous DB round trips (`UseAuthSession`, final `agents`, `items` secret)
plus an async audit `COPY`.

## Interpretation

1. **Authorization read consolidation worked.** `UseAuth` collapsed the
   previously separate `agents`, `items`, `grants`, and `approvals` lookups into
   a single query, and removing `App.UseFetch`'s redundant `Store.Agent` call
   cut `agents` lookups from 4 to 2 per request. The 3-replica 20ms result is
   6,960 req/s, a ~27.7% gain over the prior 5,449 req/s baseline.
2. **The database is not the primary limiter.** Hot-path DB queries still run in
   ~0.015–0.044 ms with a cache hit ratio near 1.0 and no waiting/blocked
   connections. Per `Use` there are now 3 synchronous DB round trips
   (`UseAuthSession`, final `agents`, `items` secret), plus an async audit
   `COPY`.
3. **The local reverse proxy was a real limiter in this test geometry.** Removing
   the `httputil.ReverseProxy` hop and running k6 directly against the origin
   replicas increased throughput by ~31% and lowered latency by ~23% in the same
   150-VU/20ms configuration. This is a single-Mac measurement and not a
   production capacity claim, but it confirms the proxy was adding measurable
   overhead to the loopback path.
4. **Session-resolution consolidation worked.** Resolving only `sessions.agent_id`
   in `/v1/use` and letting `Broker.Use`/`UseAuth` own the authoritative agent
   snapshot dropped `agents` lookups from 2 to 1 per request. Fail-closed
   revocation behavior is preserved by the final `Store.Agent` reload before
   secret access.
5. **The remaining time is still local network/runtime overhead.** The pprof CPU
   profile for the direct-origin run shows the majority of samples in
   `syscall.rawsyscalln` and runtime wait states. This is loopback I/O and Go
   runtime scheduling between k6, the origins, and the upstream test server, not
   DB work or JSON/crypto/scrub.
6. **Run-to-run variance is high on a single Mac.** Identical 3-replica 20ms
   runs varied from ~5.2k to ~8.7k req/s depending on proxy and distribution.
   The DB metrics are stable, so the variance is in the local network stack and
   k6 scheduling. Treat the numbers as directional, not a capacity guarantee.
7. **A 20ms audit flush still outperforms 5ms.** The clean 20ms direct-origin run
   was ~8,724 req/s vs ~6,669 req/s for the proxied 20ms run and ~5,248 req/s
   for the consolidated `UseAuth` 5ms run. Larger audit batches reduce `COPY`
   overhead.
8. **Session-aware `UseAuth` is the next big win.** Joining `sessions` into the
   `UseAuth` snapshot removed the standalone `sessions` call and cut the hot
   path to 3 DB round trips. Throughput rose from 8,724 req/s to 11,603 req/s
   (+33%) and p95 fell from 22.64 ms to 16.12 ms (-29%) in the same
   150-VU/20ms direct-origin configuration. Fail-closed behavior is preserved:
   `UseAuthSession` returns `ErrNotFound` for missing or expired sessions, and
   the final `Store.Agent` reload still guards against revocation races.

## Decisions

| Decision | Status | Rationale |
|---|---|---|
| Async audit with bounded flush | **Reversed** (§11) | Origin is an authorization system — a crash-lost audit row is a lost security event. Sync INSERT costs ~0.6ms against ~10× DB headroom. |
| `VEIL_AUDIT_FLUSH_INTERVAL`/`VEIL_AUDIT_BUFFER` | **Removed** (§11) | Env knobs only existed for the async auditor. |
| Sync audit on the Postgres origin | **Adopted** (§11) | Decision rows durable before response; hardened in §12 — allow-path audit commits in the consume transaction before disclosure. |
| Hourly `veil sweep` via Railway cron | **Adopted** (§11) | Bounded hot-path indexes; key-free service (DSN only), `restartPolicyType: NEVER`, exits after each run. |
| Per-request `slog.Info` in benchmark | **Suppressed in harness** | Avoids log I/O distorting results; production can enable INFO. |
| `Store.UseAuth` consolidation | **Kept** | Cuts DB round trips and raises throughput; fail-closed final agent reload is preserved. |
| Remove `App.UseFetch` pre-call | **Kept** | The broker already reloads the agent; the extra `Store.Agent` was pure overhead. |
| Add pprof to harness | **Kept** | Confirms the remaining time is network/runtime, not DB. |
| Bypass `httputil.ReverseProxy` in load-test geometry | **Kept** | Separates proxy overhead from origin capacity; shows a meaningful gain in this harness. |
| `UseAuthSession` join for session-token `/v1/use` | **Kept** | Removes the standalone `sessions` lookup and the intermediate `AgentFromSession` step; fail-closed revocation race guard is preserved. |
| `mcp` command respects `VEIL_LOG_LEVEL` | **Kept** | Prevents child-process origins from emitting per-request `slog.Info("use")` logs that become the measured limiter. |

### 7. Multi-process origin mode

Code changes:

- `cmd/loadtest` supports `LOADTEST_ORIGIN_MODE=process` to start the Veil
  binary as child `mcp` processes on ephemeral ports, simulating separate
  Railway-like origin replicas against the same Postgres.
- `LOADTEST_ORIGIN_BINARY` lets the harness use a pre-built binary; otherwise it
  builds a temporary `veil` binary.
- `internal/cli/cli.go` (`mcp` command) now honors `VEIL_LOG_LEVEL`, so child
  origins can suppress per-request INFO logs during benchmarks.
- The harness seeds from a short-lived Postgres app, truncates load-test tables
  when `LOADTEST_RESET_DB=1`, and passes the seeded token/item to `k6`.

Code changes:

- `cmd/loadtest` now seeds one session per VU (`VEIL_VUS`) and passes a
  comma-separated `VEIL_TOKENS` list to `k6`.
- `tests/load/k6/use.js` picks a token from `VEIL_TOKENS` based on the VU id,
  so concurrent VUs do not share a single `sessions` row.

#### 7.1 Initial run: single shared session

With 3 child-process replicas, 150 VUs,
`VEIL_AUDIT_FLUSH_INTERVAL=20ms`, `LOADTEST_PROXY=0` (direct origin), and
`VEIL_MAX_IN_FLIGHT_USE=300`:

| Metric | Value |
|---|---|
| Requests | 52,200 |
| Throughput | 434.99 req/s |
| Latency avg / med / p95 / max | 155.83 ms / 25.76 ms / 810.41 ms / 4,475.12 ms |
| Errors | 0% |

Hot-path DB queries (`pgbot-after`):

| Query | Calls | Mean (ms) | Total (ms) |
|---|---|---|---|
| `ConsumeSession` (`UPDATE sessions SET uses = uses + 1 FROM agents ...`) | 52,200 | **91.70** | **4,786,852.11** |
| `UseAuthSession` (LEFT JOIN `sessions → agents → items → grants → approvals`) | 52,200 | 0.0574 | 2,998.57 |
| `audit` COPY | 7,317 | 0.1429 | 1,045.68 |
| `items` secret lookup | 52,200 | 0.0102 | 534.18 |

The single shared session created a row-lock hot spot: every `Use` updated the
same `sessions` row. This is a load-test artifact, not a realistic production
pattern.

#### 7.2 Corrected run: one session per VU

Same configuration, but the harness creates 150 sessions and `k6` maps each VU
id to its own token:

| Metric | Value |
|---|---|
| Requests | 548,936 |
| Throughput | 4,574.24 req/s |
| Latency avg / med / p95 / max | 14.68 ms / 7.02 ms / 37.02 ms / 939.86 ms |
| Errors | 0% |
| Cache hit ratio | 1.0 |
| Deadlocks / waiting connections | 0 |

Hot-path DB queries (`pgbot-after`):

| Query | Calls | Mean (ms) | Total (ms) |
|---|---|---|---|
| `UseAuthSession` (LEFT JOIN `sessions → agents → items → grants → approvals`) | 548,936 | 0.0595 | 32,665.19 |
| `ConsumeSession` (`UPDATE sessions SET uses = uses + 1 FROM agents ...`) | 548,936 | 0.0381 | 20,937.21 |
| `items` secret lookup | 548,936 | 0.0171 | 9,414.16 |
| `audit` COPY | 11,861 | 0.7563 | 8,970.74 |

Interpretation of this run:

- The `UseAuthSession` join is now the largest single consumer of DB time, and
  it still averages only ~0.06 ms per request. The `ConsumeSession` row update
  is the second, at ~0.04 ms per request — exactly where it should be for a
  per-session `uses` counter.
- Per `Use`, the session-token path performs **3 synchronous DB round trips**
  (`UseAuthSession`, `ConsumeSession`, `items` secret) plus an async audit
  `COPY`. There is no standalone final `agents` reload because `UseAuthSession`
  and `ConsumeSession` both verify the agent and session state.
- Throughput went from **435 req/s** to **4,574 req/s** (+952%) just by
  removing the single-row session lock. This is still lower than the
  in-process `UseAuthSession` run (11,603 req/s) because of child-process
  scheduling and single-Mac loopback overhead, but it is a clean multi-process
  baseline.

### 8. Multi-host: Mac k6 → Railway (origin ×3, Postgres, upstream)

Topology:

- **k6 + `cmd/loadtest` (external mode)** run on the local Mac and drive the
  public Railway domain `origin-production-5416.up.railway.app`.
- **Railway project `veil-loadtest`** (throwaway): Postgres 18, three `origin`
  replicas, and `upstream` (`hashicorp/http-echo`) — all in `sfo`.
- Origin→Postgres and origin→upstream stay on `*.railway.internal`; only
  k6→origin crosses the public internet (~84 ms WAN RTT NYC→SFO).
- Seeding goes through `railway connect --tunnel-only` (SSH tunnel to
  `127.0.0.1:55432`); `VEIL_MASTER_KEY` matches the origins' key.
- `VEIL_MAX_IN_FLIGHT_USE=1000` per replica (3,000 total admission capacity).
- The Railway Postgres image ships only `plpgsql` — no `pg_stat_statements` —
  so pgbot reports connections/locks/cache but no per-query timing.

Code changes:

- `internal/publicapi/api.go`: the generic `/v1/use` error path now logs the
  underlying error (`slog.Warn("use failed", "item", …, "err", …)`); the client
  still gets the opaque `400 use failed`.
- `internal/broker/broker.go`: non-sentinel store errors are stage-tagged
  (`useauth:` / `consume:` / `secret:`) so logs identify the failing call.
- `tests/load/k6/use.js`: a `veil_fail_status` counter plus sampled failure
  bodies for the first VUs.

#### 8.1 500 VUs

| Metric | Value |
|---|---|
| Requests | 263,073 |
| Throughput | 2,192.06 req/s |
| Latency avg / med / p95 / max | 102.57 / 94.66 / 145.09 / 937.02 ms |
| Errors | **0%** |

Railway edge metrics for the same window: origin-side p50 **11 ms**,
p95 **27 ms**; origin CPU ~0.25/8 per replica; Postgres CPU low. WAN transit
accounts for ~80 ms of the ~103 ms client-visible average — the service itself
is fast.

#### 8.2 2000 VUs — rescheduled replica set (bad run)

| Metric | Value |
|---|---|
| Requests | 647,269 |
| Offered throughput | 5,390.52 req/s |
| Checks | 66.57% |
| Edge status split | 430,888 2xx / 214,162 4xx / 2,219 5xx |

The 4xx were literal `400 use failed` responses — origin-side generic store
errors before the audit point. The run overlapped a Railway reschedule of the
replica set; ~1/3 of requests failed, matching one wedged replica's traffic
share. Same signature as the earlier zombie-pool incident: `pg_stat_activity`
showed ~63 "idle" `veil` connections that were blackholed on the
client side — pgx pool conns whose TCP died silently (no FIN) during the
reschedule. **A graceful `pg_terminate_backend` of all 80 conns mid-burst on a
healthy deploy produced zero errors** — the failure needs silent packet-level
death, which a reschedule produces and a clean kill does not.

#### 8.3 2000 VUs — fresh replicas (valid run)

| Metric | Value |
|---|---|
| Requests | 665,123 |
| Offered throughput | 5,540.47 req/s |
| Checks | 99.54% |
| Failures | 3,026 (0.45%) — every sampled failure `503 origin overloaded` |
| Latency avg / med / p95 / max | 162.53 / 107.47 / 470.35 / 879.22 ms |

All sampled failures were **designed admission shedding**: per-replica in-flight
cap 1000, uneven edge→replica distribution at 2000 VUs, momentary overflow on
the hot replica. No `400` store errors observed; no `fetch_failed` denies.

Post-run DB state: 60 `veil` pool conns (3×`MaxConns=20`), 0
deadlocks, no lock waits. Under load the dominant wait was `LWLock WALWrite`
(~18 waiters) — commits bound on WAL flush, not row locks. The shared
`loadtest-agent` row in `ConsumeSession`'s `UPDATE … FROM agents` did **not**
produce observable tuple-lock pileups.

#### 8.4 Findings

1. **WAN dominates client latency.** At 500 VUs the service adds only ~10–30 ms
   on top of ~85 ms of transit. Measuring Veil's server-side ceiling from a
   single remote client requires saturating concurrency, not accuracy of p50.
2. **The audit channel is the next real limiter.** `Async.Append` blocks on the
   request path up to `AuditTimeout` (500 ms) when the 1024-event buffer
   saturates; at ~5.5k req/s sustained it began dropping events
   (`audit event dropped` / `context deadline exceeded` warns, ~1.4k in the
   582k-req run). Consequence: **the audit table is not a reliable failure
   classifier under load** — dropped events self-select for exactly the busiest
   windows.
3. **Rescheduled replicas can strand pgx pools.** A replica whose pooled conns
   blackhole during a reschedule emits fast generic errors (`400 use failed`)
   until the pool detects and replaces them. pgx recovers on its own once a
   query observes the dead conn, but each bad conn costs one request. Options:
   bounded retry on conn-level errors in the `UseSession` path, or
   `HealthCheckPeriod`/keepalive tuning. Not yet changed.
4. **Admission shedding works as designed.** `503 origin overloaded` is the
   only observed failure mode on a healthy deploy; it is fail-fast and cheap.
   Raising the cap trades sheds for queueing (run at cap 1000: p95 470 ms vs
   cap 300: faster rejects, more sheds).
5. **Client-cancel noise exists but is invisible.** `r.Context()` propagates
   into the upstream fetch; a disconnecting client produces `context canceled`
   fetch aborts → `deny/fetch_failed` audit + a 400 written to a dead conn.
   Harmless but noisy in logs under client-timeout load.

### 9. Audit isolation + corrected local baseline + OIDC cost

Post-§8 changes, all in this branch:

- **Dedicated audit pool**: `AppendAudits` runs `CopyFrom` on a separate
  2-connection pool (`application_name=veil-audit`). Request-path
  pool contention can no longer starve the audit worker.
- **Backlog drain**: the async auditor now drains the queue when the flush
  timer fires and batches up to 256 rows per COPY (was: flush only what
  arrived during the interval, max 64). Drain capacity is no longer bounded by
  `events-per-interval / (interval + flush)`.
- **Bounded safe retry**: `/v1/use` store calls (`UseAuthSession`,
  `ConsumeSession`, `ItemSecretOwner`, `OwnerWrapped`) and the audit COPY get
  exactly one retry when `pgconn.SafeToRetry` — i.e. only when the error is a
  connection-level failure that provably sent no bytes. Covers the
  reschedule-stranded-conn class from §8.4.3 without retrying partial work.

#### 9.1 Harness bug found

`originBinary()` preferred `exec.LookPath("veil")` over building
the working tree. A day-old PATH binary (predating the `ses_` session-token
scheme) answered every seeded session with 401 — **8.4M requests, 100%
unauthorized** — while looking superficially healthy. The earlier local
baselines predate `ses_` seeding so their absolute numbers were measured on
that binary's code path; treat them as order-of-magnitude only. The harness now
always builds `./cmd/veil` unless `LOADTEST_ORIGIN_BINARY` is set.

#### 9.2 Corrected local baseline (fixed binary, 200 agents)

`process` mode, 3 child origins, local docker Postgres 18
(`pg_stat_statements` preloaded), `LOADTEST_AGENTS=200`, `VEIL_VUS=2000`,
`VEIL_MAX_IN_FLIGHT_USE=1000`, loopback upstream:

| Metric | Value |
|---|---|
| Requests | 581,667 |
| Offered throughput | 4,847 req/s |
| Checks | 96.94% |
| Failures | 17,762 (3.05%) — all pre-audit (503 admission shed class) |
| Latency avg / med / p95 | 186 / 89 / 655 ms |
| Audit `allow` rows | **563,905 = exactly the 2xx count — zero drops** |

pg_stat_statements, ~564k uses over 200 agent rows:

| Query | calls | mean |
|---|---|---|
| `UseAuthSession` (join) | 563,905 | 0.094 ms |
| `ConsumeSession` (`UPDATE … FROM agents`) | 563,905 | 0.064 ms |
| `ItemSecretOwner` | 563,905 | 0.020 ms |
| `COPY audit` | 22,621 | 0.666 ms (≈25 rows/batch = 5 ms interval × arrival rate) |

~0.18 ms of synchronous DB work per `Use`; the 200-agent spread shows **no
agent-row contention** in `ConsumeSession`. Client-visible p95 655 ms at 2000
loopback VUs is k6 goroutine scheduling on the Mac, not the server (medians
match §8's origin-side numbers once WAN is removed).

#### 9.3 Multi-tenant re-run on Railway (500 agents, fixed origin)

Same topology as §8 (3 origin replicas, shared Railway PG, private upstream),
but `LOADTEST_AGENTS=500` — 2000 sessions spread across 500 agent rows — and
the origin running the audit-pool fix:

| Metric | Value |
|---|---|
| Requests | 589,607 |
| Offered throughput | 4,912 req/s |
| Checks | 97.31% |
| Failures | 15,828 (2.68%) — every sampled failure `503 origin overloaded` |
| Latency avg / med / p95 | 183 / 98 / 572 ms |

Zero 400s and zero 401s across 500 distinct agents — spreading
`ConsumeSession`'s `UPDATE … FROM agents` across 500 rows changes nothing vs
the single-agent run, confirming there was no agent-row hotspot to begin with.

Mid-run `pg_stat_activity`: 60 `veil` conns + 3
`veil-audit` conns — pool separation visible in production.

**Audit-drop validation**: replayed the run's 2000 dumped session tokens in a
120k-request sustained burst (conc 400) — `audit.allow` delta was **exactly
120,000 for 120,000 `200 allow` responses — zero drops** at WAN scale. The
same regime pre-fix dropped ~1.4k events.

#### 9.4 OIDC workload-auth cost (benchmark, no network)

`internal/workload` benchmarks against a local httptest issuer with
provider+JWKS warm (`BenchmarkAgentVerify`, `…Parallel`):

| | per-op | implied capacity |
|---|---|---|
| Serial | 32.2 µs | ~31k auth/s per core |
| Parallel (14 cores) | 6.6 µs | ~150k auth/s aggregate |

Per request that is RS256 verify + 3 store reads (`WorkloadsForIssuer`,
`Workload`, `Agent`) — against Postgres add ~0.15 ms of indexed lookups.
Discovery and JWKS are cached per issuer (verified: exactly 1 discovery across
the whole parallel run). The bearer path is nowhere near the bottleneck at the
measured `/v1/use` rates. Caveat found: `Checker.provider` holds its mutex
across `oidc.NewProvider` — a cold issuer serializes *all* workload auths
behind one discovery HTTP call. Fine at startup; worth a per-issuer
singleflight if cold issuers ever appear mid-flight.

## Next optimization

The session-token hot path is **3 synchronous DB round trips** per `Use`
(`UseAuthSession`, `ConsumeSession`, `items` secret) plus an async audit `COPY`
on its own pool — ~0.18 ms DB time total at 4.8k req/s.

Before adding a fail-closed cache or distributed rate limiting:

1. **Audit channel: superseded** (§11). The origin audits synchronously now —
   one `INSERT` per decision, durable before response, ~0.6ms. `AppendAudits`
   (batch COPY on `auditPool`) remains for bulk writers only.
2. **Connection budget.** `MaxConns=20` main + 2 audit per replica. Against
   `max_connections=100` the replica ceiling is ~4–5 before headroom for
   migrations/admin is gone. Document or gate replica scale-out; PgBouncer
   becomes the answer past ~8 replicas.
3. **Investigate the agent-token `Use` path only if it becomes a hot path.** The
   agent-token `Use` still performs a final `Store.Agent` reload after `UseAuth`
   to guard against revocation races. Folding that reload into `UseAuth` would
   require holding a row lock on the agent through the upstream `fetch`, which
   would serialize concurrent uses by the same agent and is not a win for the
   high-volume session path. For now, keep the reload; it is a tiny fraction of
   DB time in the session load test.
4. **Only after the hot path is flat and measured away from loopback, consider a
   short-lived, fail-closed cache** for session/grant metadata with explicit
   invalidation and distributed rate limiting.

### 10. Post-cutover baseline on production Postgres (2026-09-18)

Same Mac, same harness, current `main` — first runs **after** the SQLite→
Postgres cutover and the sqlc-generated accessor layer. This is the regression
gate for both changes.

Full Go suite first: `go test -race -shuffle=on ./...` with `PG_TEST_DSN`
against Docker Postgres — **all green** including `internal/store`,
`internal/audit`, and the sqlite→pg migration test.

| Run | Requests | Throughput | avg | med | p95 | max | errors |
|---|---|---|---|---|---|---|---|
| 3 replicas, 150 VUs, 20ms flush, direct | 754,711 | 6,289.3 req/s | 10.67 ms | 5.26 ms | 26.58 ms | 895 ms | 0% |
| 1 replica, 50 VUs, 20ms flush, proxied | 412,070 | 3,433.9 req/s | 6.48 ms | 4.08 ms | 14.02 ms | 777 ms | 0% |
| 3 replicas, 500 VUs, 200 agents, direct | 435,294 | 3,625.9 req/s | 61.82 ms | 22.88 ms | 155.24 ms | 13.98 s | 0% |

pgbot (3rep-150vu-direct) — the sqlc hot path per `Use`:

| Query | Calls | Mean | Max | /request |
|---|---|---|---|---|
| `UseAuthSession` (sessions⋈agents⋈items⋈grants⋈approvals) | 754,711 | 0.0492 ms | 4.23 ms | 1 |
| `ConsumeSession` (atomic uses++ UPDATE) | 754,711 | 0.0330 ms | 12.07 ms | 1 |
| `ItemSecretOwner` | 754,711 | 0.0149 ms | 3.44 ms | 1 |
| audit `COPY` | 13,024 | 0.62 ms | 242.9 ms | 0.017 (~58 rows/batch) |

~0.097 ms of synchronous DB time per request — **0.61 DB-seconds per second
of load across all 3 replicas**. Postgres is not the limiter; the query means
match the pre-sqlc hand-written accessors within noise (the sqlc swap is
performance-neutral).

**Correctness under concurrency** (500-VU run): `sum(sessions.uses)` =
**435,294 for exactly 435,294 requests** — the atomic `ConsumeSession` recheck
lost zero updates. Audit rows = 435,294, 1:1 with requests — zero drops. 200
agents × 500 sessions spread contention; hottest session consumed 3,335 uses.

**The knee**: 500 VUs saturated the 3-replica local setup — throughput fell
*below* the 150-VU run (3,626 vs 6,289 req/s) with p95 155ms and 14s max as
requests queued behind saturated connections. `ConsumeSession` mean held at
0.085ms (WAL/row-lock fine); `UseAuthSession` mean tripled to 0.118ms under
CPU contention — the failure mode is **queueing, not locks**. Capacity per
origin replica at this geometry: ~2,000–3,000 req/s before tail latency
degrades; horizontal replicas are the lever (DB headroom is ~10×).

**Production canary** (real vault, `veil.nyc`, post-cutover): 30 sequential
`/v1/use github` → api.github.com — **p50 261ms, p95 365ms, 0 fails** (includes
the WAN upstream hop). `/v1/items` origin-only: **p50 114ms, p95 131ms** —
the server-side share after Mac→Railway RTT is ~30–50ms: OIDC verify +
workload lookup + grant-filtered items query on the migrated `veil` database.

### 11. Synchronous audit + expiration sweep (2026-09-18)

Two durability changes on the back of the §10 measurements.

**Sync audit.** The Postgres origin switched from `audit.Async` (in-memory
buffer, batch COPY on a 2-conn pool, crash-loss window = buffered events) to
`audit.Sync` — every `Use` decision is an `INSERT` durable in Postgres before
the response returns. Rationale: this is an authorization system; a lost
audit row is a lost security event, and the measured cost is small.

Measured single-row `INSERT INTO audit` on the test Postgres: **0.60 ms/op**
(1,661 writes/s on one connection — irrelevant ceiling, the origin pool has
20). Per-request DB time goes ~0.097ms → ~0.7ms; Postgres still is not the
limiter. `AppendAudits` (batch COPY on `auditPool`) remains for bulk writers.

Ordering note: audit rows were written *after* the decision — for allows, the
secret had already been fetched and injected before `auditUse` fired. Sync
therefore bought "durable before response," not "deny if unauditable." This
gap is closed in §12: the allow-path audit row now commits inside the
`ConsumeSession` transaction *before* the secret is touched.

**Expiration sweep.** Sessions, grants, and approvals previously grew forever
— expiry is enforced at read time but rows were never deleted. `Store.Sweep`
(new interface method, all three impls) deletes terminally-expired rows older
than a cutoff: `sessions` past expiry or revoked, `grants`/`approvals` past
expiry. The 24h keep window preserves a grace period for forensics. `veil
sweep --keep 24h` runs it key-free (raw `pgxpool`/`sql.DB` — no decrypt), so
the `veil-sweep` Railway service carries only `VEIL_POSTGRES_DSN`, runs
`0 * * * *`, and exits. `EnsureSQLiteSchema` was factored out of
`SQLite.migrate` so the keyless CLI path can create missing tables on old
vaults (`rewrapLegacy` stays behind `OpenSQLite` — it needs the key).

Test coverage: `TestSweep` runs the conformance suite across Memory/SQLite/
Postgres — expired+revoked sessions deleted, keep-window rows preserved,
grants/approvals honored, second pass is a no-op.

### 12. Transactional consume+audit — zero unaudited allows (2026-09-18)

§11 made audit durable before the response; this makes it durable before
*disclosure*. `Store.ConsumeSessionAudited` runs the atomic `ConsumeSession`
UPDATE and the audit `INSERT` in one Postgres transaction (SQLite: one
`database/sql` tx; Memory: one lock). If the audit insert fails, the consume
rolls back and the request errors — the session use is not burned, no secret
is fetched, no credential leaves the origin. Agent-token calls have no
consume to transact with, so their allow-path audit is a synchronous
`AppendAudit` before `Store.Secret`; failure there also fails closed.

Semantics that changed on purpose: the audit row records the authorization
decision *at release time*. A downstream fetch failure no longer writes a
second `fetch_failed` row — the `allow` row stands (the credential was
released into the attempt); upstream outcome remains in logs and spans. Deny
paths still audit post-decision and stay best-effort: an unaudited deny
discloses nothing.

Cost: the audit INSERT shares the consume's transaction and connection — no
extra round trip, so the §11 0.6ms figure is the total added cost, not per
request on top of consume.

Test coverage: `TestSessionConsumeAudited` (conformance — event committed
with the consumed agent's identity, refused consume appends nothing),
`TestSessionConsumeAuditedRollback` (audit table dropped under SQLite and
Postgres — consume rolls back, `uses` stays 0), and
`TestUseAuditFailureFailsClosed` (broker — both token paths error with zero
audit rows and no upstream call).

### 13. Post-transactional-audit baseline (2026-09-23)

First load runs **after** §11+§12 — every `Use` now commits
`ConsumeSession`+`InsertAudit` in one transaction, so a synchronous WAL commit
sits on the hot path. Same Mac, same harness, `process` mode, direct origins,
`VEIL_MAX_IN_FLIGHT_USE=1000`, fresh `loadtest` DB.

| Run | Requests | Throughput | avg | med | p95 | errors |
|---|---|---|---|---|---|---|
| 3 replicas, 150 VUs, 200 agents | 444,366 | 3,703 req/s | 18.17 ms | 10.08 ms | 47.24 ms | 0% |
| 1 replica, 50 VUs, 50 agents | 347,123 | 2,892 req/s | 7.69 ms | 6.15 ms | 14.18 ms | 0% |

**Consistency proof (both runs)**: `count(audit.decision='allow')` =
`sum(sessions.uses)` = request count exactly — 444,366 and 347,123. Every
request consumed exactly one use and committed exactly one audit row. Zero
sheds, zero store errors, zero unaccounted requests.

pg_stat_statements, per-`Use` synchronous DB time:

| Query | mean (3rep/150vu) | mean (1rep/50vu) |
|---|---|---|
| `UseAuthSession` | 0.096 ms | 0.051 ms |
| `ConsumeSession` (UPDATE) | 0.073 ms | 0.040 ms |
| `InsertAudit` (in same tx) | 0.045 ms | 0.031 ms |
| `ItemSecretOwner` | 0.018 ms | 0.014 ms |
| **total** | **0.231 ms** | **0.136 ms** |

Interpretation:

- **The durability change costs real, bounded latency.** §10 measured
  ~0.097 ms DB per `Use` with audit on the async COPY path; §13 measures
  ~0.14–0.23 ms with the audit INSERT inside the consume transaction. The
  audit INSERT itself is ~0.03–0.05 ms; the rest of the added latency is the
  COMMIT — the fsync that pg_stat_statements doesn't attribute to any
  statement. Docker-on-Mac fsync (virtiofs) almost certainly overstates the
  production number; the Railway §8/§9 runs are the better latency reference.
- **Throughput fell vs §10's pre-§12 run** (6,289 → 3,703 req/s at the same
  3rep/150vu geometry). Part is commit serialization — every `Use` holds a
  connection through COMMIT now — and part is the documented ±40% run-to-run
  variance of this harness on one Mac. Directionally: sync audit is not free,
  and it is cheap. Postgres is still not the limiter (≤0.23 ms DB per
  request; ~0.86 DB-seconds per second of load at 3.7k req/s).
- **The fail-closed invariant holds under load**: the 1:1:1
  request/consume/audit count is not a test assertion, it's the actual
  database state after 791k combined requests across the two runs.
- One footgun found and documented: child-process origins default
  `VEIL_MAX_IN_FLIGHT_USE` to **100** (cli.go), not unlimited — the harness
  must set it explicitly or 150 VUs shed at 3×100 capacity.

## Scalability model — thousands of users and agents

Measured basis (this doc): a `Use` costs ~0.14–0.23ms DB time (auth read +
transactional consume+audit commit + secret read, §13); auth reads are
index-point lookups (`sessions.secret_hash` unique, `grants(agent_id,item_id)`,
`workloads(issuer)`).

- **Users** (humans) barely touch the hot path — login/TOTP/grant-admin flows
  are Kratos/Hydra + occasional vault writes. Thousands of humans is a Kratos
  sizing question, not a broker one.
- **Agents** scale along two axes: count × request rate. Count is cheap —
  agents/items/grants rows are small and indexed; 200-agent runs show zero
  lookup degradation. Rate is the constraint: N agents × R req/s each = total
  `Use` load. At ~0.23ms DB time each (§13), a single modest Postgres core
  sustains roughly **4k `Use`/s**; the origin replicas exhaust first (~2–3k
  req/s each at 150 VUs of headroom), so scale-out is `replicas += n` until the
  connection budget binds (~4–5 replicas at `max_connections=100` —
  PgBouncer past that, see Deferred #1).
- **Writes that grow unboundedly**: `audit` (1 row per use + lifecycle events)
  and `sessions` (per-agent leases). Audit is the first real scale task —
  partition/retention policy before billions of rows, not now.
- **Real-world posture**: prod canary p50 ~115ms origin-only — dominated by
  client WAN RTT, not the datastore. The SQLite wall this removed was the
  single-writer serialization under concurrent agents; Postgres turns
  contention into queueing, which scales with replicas.

## Deferred decisions (2026-09-17)

Deliberately not done. Each has a trigger; act when the trigger fires, not before.

1. **PgBouncer / replica scale-out.** Trigger: planned replica count approaches
   the connection ceiling (~4–5 origins at `max_connections≈100`, 22 conns
   each), or connection churn shows up as a measured limiter. Until then
   connection pooling middleware adds a hop and a failure domain for zero gain.
2. **Per-issuer singleflight in `workload.provider`.** `Checker.provider` holds
   its mutex across `oidc.NewProvider` — cold-issuer discovery serializes all
   workload auths behind one HTTP call. Trigger: issuers being added while
   traffic is live, or concurrent first-auths against a new issuer. Issuers are
   configured ahead of time today; the cold case is effectively startup-only.
3. **Audit drop-oldest vs block-and-shed.** **Resolved by §11** — the async
   queue is gone from the origin path, so there is no drop policy left to
   pick. Sync INSERT failure logs an error and returns the decision.

