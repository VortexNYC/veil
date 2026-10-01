# Groups

Teams. Today every principal is in exactly one org (`org_id`) and owns
or is owned `user`-or-`org` — that model already covers "my agent" vs
"the company's agent." What it cannot express is *a set*: five humans
and their twelve agents who should all see the same twenty items are
sixty grants administered by hand. Groups collapse that to one.

## Model

Three objects, only one is new:

- **`group`** — `{id, org_id, name}`. An org-scoped row in the vault
  store like items and agents. Holds metadata only.
- **Membership** — a `group_members` table in the vault store:
  `{group_id, member_kind, member_id}` edges. Groups are vault data —
  Keto stays org owner/member and is *not* the group store. Humans and
  agents can both be members — `group:ci` for the build fleet is a
  first-class case.
- **Grant subject** — `grants.agent_id` becomes `subject` with a kind:
  `agent` | `human` | `group`. Existing rows migrate as `agent`.

The 1Password mapping: a shared vault is an **org-owned item + a grant
to a group**. Personal items stay user-owned with 1:1 grants — nothing
about the solo model changes.

## Personas — where grant levels live

The two agent classes want different levels, and the group is where
that difference is expressed:

- **Human-adjacent agents** (laptop IDE, CLI) — `level1` + the
  approval-request loop. A human is near enough to answer in seconds.
- **Deployed agents** (CI runners, cloud coding sessions, services) —
  `level2` on a narrow item set, always. A headless job that hits
  `need_approval` doesn't get approved — it hangs and dies. Granting
  `level1` to a fleet agent is a failure mode, not a flow.

Group membership is the provisioning path for the second class:
instances are ephemeral, `group:ci` is stable. Spawn → member →
inherit → die → membership ends; grants never churn. A filed approval
request from a deployed agent is telemetry — the owner under-scoped
the group's grants; fix the set, don't approve the recurrence.

## Evaluation

`Use` resolves the caller's groups once — the store's `UseAuth` already
expands membership at read time (the caller's own rows plus, for a
user-owned agent, the owner's) — then grant lookup is `subject IN
(self, groups)`. Rules stay deterministic:

1. Direct grants evaluate first, group grants second.
2. Any allow wins; deny beats allow; `need_approval` from the matching
  grant flows into the approval-request loop untouched.
3. Level conflict (direct `level2` + group `level1` on the same item) →
  the direct grant wins — specificity beats inheritance.

Membership lives in the same store row space as the grant, so expansion
is one round trip — no cache needed this slice.

## Administration

Org owners create groups, manage membership (`veil group member
add/remove`), and grant to them — same "owners administer, members
consume" split as grants today. A member granted via a group sees the item through
list/fill exactly as if granted directly; the grant's provenance
(`subject=group:eng`) is visible in audit and `grant list`.

## Edges

- Member leaves the org → org teardown (`PurgeOrg`) removes the rows;
  revoking an agent ends its `Use` regardless of remaining membership
  edges.
- Group deleted → its grants are revoked; members keep direct grants.
- An agent's owner revokes the agent → its sessions die regardless of
  group membership — existing revoke semantics, unchanged.
- Group + direct grant on the same item → direct wins (rule 3); the
  group grant is shadowed, not an error.

## Non-goals

Nested groups (group-in-group) — membership edges name only agents and
humans; the UX doesn't earn nesting at this size. Group-owned items —
items stay `user`|`org`; a group with write access is a grant, not an
owner. Cross-org groups — orgs stay isolated.

## Build order

1. `groups` table + `subject_kind`/`subject_id` on grants (migrate
    `agent` default) + store CRUD.
2. Keto membership writes + expand on the `Use` path with TTL cache.
3. `veil group` verbs: `add`, `member add/remove`, `list`;
   `grant add --group`.
4. SPA: group card (members, granted items) beside the approvals card.
