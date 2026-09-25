# Approval requests

The human-in-the-loop half of grants. Level1 already means "agent
prepares, a human approves the last step" — `need_approval` is a
first-class decision, `approvals` rows gate the grant, `veil approve`
exists. What does not exist: a way for the ask to reach a human. Today
a denied agent has nowhere to put the request and a human has nothing
to answer. This is that missing object.

## The dial

Access is a per-(agent, item) dial the owner sets — the grant object
is the whole spectrum, not a binary:

- **No grant** — deny by default; the agent cannot even list the item.
- **Level1** — every use asks a human first. Micro control.
- **Level2** — donated identity, no asks. Full trust.

The other axes compose on top: `actions` narrows *how* (an agent can
`fetch` for browser fill but not `env`-inject into a shell),
`expires_at` narrows *how long*. And the dial is independent per
agent — A can be micro-managed on `prod-deploy` while B holds `level2`
on `dev-db` and C sees nothing but two staging items. Groups apply the
same dial to a set. The owner picks every point; this loop is what
makes the Level1 end usable instead of a hang.

## Prior art

| Player | Their model | What we take | What we skip |
|---|---|---|---|
| Vault control groups | Approval as a factor on the ACL; K-of-N approvers; request wraps an accessor token you poll | Approval-on-the-grant semantics (already ours) | The accessor indirection — our grant *is* the object; retrying `Use` is idempotent. Enterprise license for the whole feature. |
| Infisical access requests | Access policies, multi-step approver chains, approver groups, break-glass bypass, self-approval toggle | The request as a durable object + notify + break-glass framing | Chains, groups, policies — org size makes them YAML-drag. Self-approval is structurally impossible here: the requester is always an agent, never the approver. |
| CIBA | Backchannel: client polls a request id until approved/expired | Agent mental model: retry the same call | A new protocol — `Use` retry already is the poll. |
| Apple / Tailscale | "New device" notification → approve on a trusted surface | Single-action email → approval card | Bearer approve-links — a leaked email must never be able to approve; approval requires the owner session. |

Break-glass already exists by design: a Level2 grant is a donated
identity — no human in the loop, permanently. That is the bypass;
audit carries it.

## The object

`approval_requests` — one row per open ask:

```sql
id, org_id, agent_id, item_id, grant_id, action,
status      -- open | approved | denied | expired | cancelled
created_at, expires_at,
resolved_at, resolved_by,       -- human id
approval_id                     -- set when approved
```

One open request per `(grant_id, action)` — a partial unique index.
An agent hammering `Use` cannot spawn asks or emails.

## The flow

The denial edge files the request. The agent never asks permission to
ask — `need_approval` **is** the ask:

1. Agent calls `Use` on a Level1 grant with no live approval →
   `need_approval` + `request_id` + `request_expires_at`. The row is
   inserted and owners are notified, once, on the denial edge.
2. Agent retries `Use` on backoff. Nothing new to learn, no new tool —
   the retry is the poll.
3. An owner approves → `approvals` row (existing TTL semantics) +
   request resolves `approved`. Next `Use` returns `allow` and audit
   carries `approval_id` — already wired.
4. Owner denies → request resolves `denied`. Next `Use` files a fresh
   request — denial applies to the ask, not the item. Permanent denial
   is `grant revoke`/`agent revoke`, a different lever.
5. Nobody answers → `expired` at `expires_at`. Next `Use` re-files.

## Notify

The denial response is itself a notification: for a human-adjacent
agent the ask lands in the operator's own surface — the Cursor chat,
the Devin session log — in-band, in real time. The human reads
"approval required, request REQ-x filed" where they are already
watching. Email is the backup channel for an owner who stepped away,
not a dependency the agent waits on.

On insert: email every org owner (Kratos → owner emails) via
`veil-mail`. Subject carries the ask: `devin wants github`. Body:
agent, item, action, expiry, and a link to the approvals card at
`app.veil.nyc` — never an approve token. Mail down loses the
notification, not the request: the row is durable and surfaces in
`GET /v1/requests`, the SPA card, and `veil requests`.

## Surfaces

- **Agent/MCP**: no new tools. The `need_approval` denial payload gains
  `request_id` and `request_expires_at`. Agents keep calling `Use`.
- **Origin API** (owner JWT, Keto owner check):
  - `GET /v1/requests?status=open`
  - `GET /v1/requests/stream` — SSE feed, one empty `data: {}` tick per
    committed request change in the owner's org. Postgres `LISTEN/NOTIFY`
    (`approval_requests` trigger on INSERT / status UPDATE) wakes whichever
    replica holds the stream, so ticks arrive for writes made on any
    replica. Ticks carry no data — the client refetches `GET /v1/requests`;
    a missed tick only delays a refresh, never fabricates or loses state.
  - `POST /v1/requests/{id}/approve` `{ttl}` → PutApproval + resolve
  - `POST /v1/requests/{id}/deny` → resolve denied
- **CLI**: `veil request list [--status]` lists asks;
  `veil request approve REQ_ID [--ttl]` and `veil request deny REQ_ID`
  resolve them (grant-level `veil approve GRANT_ID` stays — pre-approval
  for a known window is a valid pattern).
- **SPA**: `app.veil.nyc/requests` — pending asks, approve / deny, expiry
  countdown, status filter. The open view subscribes to the SSE stream
  (new asks and resolutions appear instantly) with a slow poll as the
  reconnect safety net.

## Edges

- Grant revoked or agent revoked while a request is open → `cancelled`.
- Approval lapses → next `Use` is `need_approval` again → re-file.
- Two agents, same item → separate grants → separate requests →
  approvals stay per-(agent,item) pair. Approval never widens ambiently.
- Two owners act at once → the resolve is a conditional update
  (`WHERE status='open'`): first write wins, the loser sees
  `already resolved`. `resolved_by` always names the owner who acted.
- `POST .../approve` is atomic: conditional resolve + approval insert +
  sibling-ask resolution in one transaction. A lost race writes nothing —
  no approval is minted for a request the loser did not win, and a 409
  means the world is exactly as it was.
- An ask on a dead grant edge cannot approve: the conditional requires
  the grant to exist and be unexpired, the agent un-revoked, and the
  item un-archived. Grant-scoped `veil approve GRANT_ID` checks the same
  predicate and fails with `ErrGrantNotLive` writing nothing. A stranded
  ask stays open until its own TTL marks it `expired` — honest state
  over a misleading resolution.
- `request_expired` is audited exactly once, whichever path finds the
  stale ask first: a refile names the expired predecessor's ID
  atomically inside the file transaction, and the sweep expires each
  row and writes its event in one transaction — an expired ask always
  has its audit line. Concurrent refiles cannot double-report the same
  expiry.
- `approval_expired` denial files a request identically — an expired
  approval is just a missing one.

## Audit

New actions: `request_filed`, `request_approved` (with `approval_id`),
`request_denied`, `request_expired`, `request_cancelled`. Agent, item,
grant, and resolving human on every row. `Use` audit already carries
`approval_id`.

### Durability contract

Audit writes never carry secrets. Every request lifecycle transition is
**atomic with its audit event** — the store writes the event inside the
same commit as the state change (VEIL-50):

- File, refile-expiry, approve, grant-approve, deny, cancel, and sweep
  expiry each emit their event inside the state transaction. A request
  row can never commit without the record of how it got there.
- On Postgres each event insert runs under a savepoint: a failed insert
  rolls back to the savepoint and the event is queued in `audit_outbox`
  **inside the same transaction**, which a relay drains into `audit`
  later. If audit and outbox are both unwritable the whole transaction
  fails — the resolution is refused rather than taken unrecorded.
- The request row remains authoritative (`status`, `resolved_at`,
  `resolved_by`, `approval_id`); the audit row and outbox entry are the
  searchable projection, now guaranteed to exist.
- SQLite and memory have no outbox — they are local-dev and test
  backends; a failed audit write there rolls the whole transaction back
  (VEIL-49 tracks parity as a decision, not a gap).
- The `Use` hot path was already on this contract — session calls write
  their event inside `ConsumeSessionAudited`, and agent-token calls use
  `AppendAudit` with the same outbox fallback. Request lifecycle events
  now match that standard.

## Non-goals

Multi-step approval chains, approver groups, K-of-N quorums, and
approval policies — revisit if orgs grow past one-owner decides. Any
MCP tool that asks for secrets. Bearer approve-links in email.

The 1Password-Teams analog worth revisiting first is group grants
(grant → role: "eng gets eng items" as one inherited object), not
approval routing — grants are already per-secret-per-principal ACLs,
but today they are 1:1 and a team multiplies them by N×M. That is a
grant-subject redesign, orthogonal to this loop.

## Build order

1. `approval_requests` table + dedupe index + store CRUD.
2. Denial-edge filing in the `Use`/`Env` need_approval path + response
   fields.
3. `GET`/`POST` request endpoints (owner-scoped) + mail notify.
4. `veil request list` + `veil request approve`/`deny`.
5. SPA approvals card.
