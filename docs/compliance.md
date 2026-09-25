# Compliance self-assessment (pre-alpha)

> **System of record:** the living register now tracks in CompAI
> (`~/Projects/CompAI`). This file is the point-in-time self-assessment
> it was seeded from — update CompAI, not this doc, when a gap moves.

Scope: Veil private alpha. Audience: the operator, then any auditor or
compliance platform (CompAI, Vanta, Drata) we adopt later. This is a
self-assessment, not an audit — its job is to make every gap a named,
owned, dated item instead of a vibe.

Method: SOC 2 Trust Services Criteria (2017, rev 2022) as the frame —
it's what enterprise customers will ask about first. For each criterion:
what exists today, evidence pointer, verdict. Solo-operator scope is
stated where it changes the answer; "N/A" means genuinely inapplicable,
not "didn't bother."

Legend: **met** / **partial** / **gap** / **N/A**.

## CC1 — Control environment

| Criterion | State | Evidence |
|---|---|---|
| CC1.1 Integrity & ethical values | partial | AGENTS.md + SPEC encode the security invariants; no written code of conduct / ethics statement. Solo operator, single decision-maker. |
| CC1.2 Board/oversight independence | N/A | No board. Founder is sole oversight. |
| CC1.3 Structure & reporting lines | N/A | Solo operator. |
| CC1.4 Competence | met | Prior art review (`docs/prior-art.md`), spec-first development, test-first rule in AGENTS.md. |
| CC1.5 Accountability | met | Git authorship, operator runbook assigns every verb to an owner action. |

## CC2 — Communication & information

| Criterion | State | Evidence |
|---|---|---|
| CC2.1 Internal communication of security objectives | met | `docs/SPEC.md`, AGENTS.md load-bearing rules (agents never see secrets, fail-closed audit). |
| CC2.2 External communication to customers | met | `/privacy` (collection, subprocessors, retention, alpha terms) + `/security` (encryption model, agent isolation, audit ledger, disclosure path) are live and linked from the homepage. Formal ToS precedes public launch — not needed for invite-only alpha. |
| CC2.3 Confidentiality commitments | partial | No NDA template; `/privacy` is the plain-terms data-handling commitment. Invite-only alpha keeps this manageable — first enterprise ask needs a DPA answer (subprocessor list already published). |

## CC3 — Risk assessment

| Criterion | State | Evidence |
|---|---|---|
| CC3.1–3.4 Risk identification & analysis | partial | Risks are identified implicitly in SPEC/prior-art and mitigated in code (fail-closed audit, KEK escrow, offsite backups). **No consolidated written risk register** — this document's gap register is the seed; promote it. |
| CC3.4 Fraud/specific-risk assessment | partial | Abuse analysis exists for the invite surface (rate limits); no systematic "how does an insider/agent exfiltrate" write-up. |

## CC4 — Monitoring activities

| Criterion | State | Evidence |
|---|---|---|
| CC4.1 Ongoing monitoring | met | `veil-monitor` cron: backup/sweep heartbeat staleness, `/ready`, `errors_5m` sustained-5xx, `audit_outbox` depth/age; email alerts with cooldown. OTEL traces → PostHog. |
| CC4.2 Deficiency evaluation & remediation | partial | Findings tracked informally (todo lists, git). No defect register. Acceptable at this scale; formalize when a second person touches prod. |

## CC5 — Control activities

| Criterion | State | Evidence |
|---|---|---|
| CC5.1–5.3 Control selection, tech controls, deployment | met | Controls are in code and tests, not spreadsheets: consume-once tx, fail-closed audit, owner-gated lifecycle, rate limits. |

## CC6 — Logical access

| Criterion | State | Evidence |
|---|---|---|
| CC6.1 Access provisioning | met | Invite-only onboarding; provision requires Hydra token + Kratos stamp + Keto tuple (two-signal check, `TestProvisionJoinsInvitedOrg`). |
| CC6.2 Access removal | met | Member removal, owner demote, self-delete, org teardown — live-tested against real Keto (`TestLiveOnboarding` lifecycle arc). Token stops resolving the moment tuples+row drop. |
| CC6.3 Access review | **gap** | **No scheduled review.** Fix: quarterly access review — enumerate `humans`, Keto tuples, Railway seats, Cloudflare access, 1Password vaults, GitHub repo collaborators; diff against expectation. Add to runbook. |
| CC6.6 Least privilege | met | Session tokens carry scope+max-uses; agents see only granted items; members can't administer. No broad admin token exists — even the operator works through owner verbs + Keto/Kratos admin endpoints. |
| CC6.7 Data transmission | met | TLS strict end-to-end (Cloudflare strict mode + origin cert verified); secrets never in logs/audit/JSON — asserted by leak tests. |
| CC6.8 Unauthorized software | N/A | No managed fleet beyond the founder's machine; device posture is self-attested (FileVault, screen lock) — write the attestation once. |

## CC7 — System operations

| Criterion | State | Evidence |
|---|---|---|
| CC7.1 Vulnerability detection | met | `govulncheck` in `make ci`; toolchain pinned to patched `go1.26.8`. |
| CC7.2 Security monitoring | partial | Origin 5xx window + heartbeat monitoring + audit trail. No IDS/WAF tuning beyond Cloudflare defaults — acceptable for alpha, revisit at public launch. |
| CC7.3 Incident response | met | `docs/incident-response.md`: S1–S3 severity ladder, first-hour checklist, customer comms template, postmortem format, bus-factor recovery. |
| CC7.4 Incident recovery | met | Restore drill proven (live dump → KEK unwrap → wrong-key fail-closed). |

## CC8 — Change management

| Criterion | State | Evidence |
|---|---|---|
| CC8.1 Changes authorized, tested, approved | partial | Tests + CI gate everything; but solo dev means no mandatory second-review. Policy: anything touching authz/crypto/store semantics gets a pre-merge review pass (human or adversarial self-review); deploys are `railway up` from `main` only. Document this as the policy — it exists, it's unwritten. |

## CC9 — Risk mitigation (vendors, BCP)

| Criterion | State | Evidence |
|---|---|---|
| CC9.1 Vendor management | met | Vendor register below — every data-touching processor named with role + assurance. Vortex added when billing went live. Collect SOC 2 reports annually (railway + cloudflare published today). |
| CC9.2 Business continuity | met | RPO 24h / RTO hours, documented; offsite replication to R2 verified end-to-end; KEK escrowed separately; restore drill monthly. |

## Availability (A-series)

| Criterion | State | Evidence |
|---|---|---|
| A1.1 Capacity planning | met | `docs/scale.md`; pool sizing budgeted across replicas + audit pool; PgBouncer deferred behind a measured trigger. |
| A1.2 Availability monitoring | met | `/health` liveness, `/ready` deep (issuer + PG + 5xx window). |
| A1.3 Recovery | met | See CC7.4 / CC9.2. |
| **SLA decision** | **open** | Private alpha: publish **no SLA**. Say so on the security page instead of implying one. |

## Confidentiality (C-series)

| Criterion | State | Evidence |
|---|---|---|
| C1.1 Data classification | partial | Implicit three-tier (secrets / metadata / audit). Write it once in this doc: **secrets** = item ciphertext + KEK + tokens; **confidential** = items metadata, humans, orgs; **internal** = audit, heartbeats. |
| C1.2 Disposal | met | Org teardown purges vault rows in one tx; audit survives as forensic record (documented decision — audit is metadata, not secrets). |

## Processing integrity (PI)

| Criterion | State | Evidence |
|---|---|---|
| PI1.x Complete/accurate/timely processing | met | Consume-once transaction with race protection proven under load; fail-closed audit with durable outbox+relay; `ListAudit` unions queued rows so nothing is invisible while pending. |

## Privacy (P-series)

| Criterion | State | Evidence |
|---|---|---|
| P1.x Notice, collection, use | met | `/privacy` enumerates collection (email/name, ciphertext, audit, ops logs), subprocessors, retention, deletion. Kept in sync — Vortex was added when billing went live. |
| P4.x Data subject rights | met | Self-delete + org teardown are real API verbs, not a support ticket. Recovery codes enabled (`lookup_secret`) for lockout. |

---

## Gap register — the "not valid or proper" list

Ordered by what hurts a real customer first. Every gap has an owner action.

| # | Gap | Severity | Action |
|---|---|---|---|
| G1 | ~~No privacy policy~~ — `/privacy` shipped (collection, subprocessors, retention, alpha terms) | closed for alpha | Formal ToS + legal pass remain a pre-launch item, not an alpha blocker. |
| G2 | ~~No incident response plan~~ | closed | `docs/incident-response.md` shipped — severity ladder, comms, postmortem, bus factor. |
| G3 | ~~No vendor register~~ | closed | Register below; Vortex added when billing went live. Annual SOC-report collection stands. |
| G4 | No scheduled access review | medium | Quarterly checklist in runbook; first one at next calendar quarter. |
| G5 | ~~No consolidated risk register~~ | closed | Risk register section below is it — promote to `docs/risk-register.md` only if it outgrows this doc. |
| G6 | No external security review | medium | Schedule a scoped review (or self-review pass) before public launch; alpha is invite-only so blast radius is bounded. |
| G7 | Support impersonation = owner curl | low | Document as procedure; formal support role only when volume demands. |
| G8 | ~~Operator bus factor~~ | closed | Bus-factor section in `docs/incident-response.md` — KEK escrow path + restore procedure written down. |
| G9 | No data classification doc | low | Done in C1.1 above — three lines, promote if needed. |
| G10 | Change-mgmt policy unwritten | low | Write the CC8.1 paragraph above into this doc — done. |

## What an auditor gets asked for, and where it lives

| Ask | Answer |
|---|---|
| "Show me your access control policy" | AGENTS.md load-bearing rules + this doc CC6 + lifecycle runbook section |
| "Show me your incident response plan" | `docs/incident-response.md` |
| "Show me evidence backups work" | `TestPostgresBackupRestoreDrill` + monthly drill record + R2 listing |
| "Show me your risk assessment" | This doc + risk register |
| "Show me vendor SOC reports" | Vendor register (G3) |
| "Show me change management" | Git history + CI (`make ci`, govulncheck) + CC8.1 note |
| "Show me security monitoring" | `veil-monitor` + OTEL→PostHog + `/ready` errors_5m |
| "Show me data deletion" | Lifecycle verbs + `PurgeOrg` tx + audit-survival decision |

## Vendor register

Every vendor that touches customer data or production access. Review
annually; collect SOC 2 / security docs where offered.

| Vendor | Role | Customer data | Access | Assurance |
|---|---|---|---|---|
| Railway | Compute + Postgres host | ciphertext at rest; `VEIL_KEK` in env (see note) | full prod | SOC 2 Type II (trust.railway.com) |
| Cloudflare | DNS, WAF edge, Workers (mail, backup-ingest, site, docs, login UI), R2 | encrypted backups in R2; email content via mail worker | edge TLS termination — plaintext transits CF | SOC 2 (cloudflare.com/trust-hub) |
| Ory | Kratos/Hydra/Keto images | identity data lives in OUR Postgres — Ory is software, not a processor | none (self-hosted images) | n/a — pin + govulncheck covers it |
| GitHub | source hosting | none (secrets never committed — enforce) | full repo | SOC 2 |
| 1Password | KEK escrow + agent creds | KEK (the crown jewel) | escrow only | SOC 2 |
| Vortex | Billing processor | org id, plan, subscription, usage counts; card data goes to Vortex's hosted checkout + its payment processor — never transits Veil | billing webhooks + API | sister company, same operator; VOR-tracked |
| PostHog | OTEL traces | spans only — secrets/PII verified out: `veil.host` is scheme://host (no path/query), `url.path` overwritten with the route pattern, `client.address` blanked (360622a) | read | SOC 2 |
| Resend | NOT USED — mail worker is ours | — | — | — |
| Docker Hub / GHCR | base images | none | supply chain | pin digests on prod images |

Note: `VEIL_KEK` lives in Railway env vars, so Railway is technically a
KEK processor — that's why the *dumps* go offsite and the *escrow* copy
goes to 1Password: no single vendor death loses both ciphertext and key.

## Risk register (seed — promote when it grows)

| Risk | Likelihood | Impact | Mitigation today |
|---|---|---|---|
| Railway account loss | low | vault unreadable without KEK | KEK escrow + R2 offsite dumps + restore drill proven |
| KEK compromise | low | catastrophic | rotate-kek verb exists; escrow split; no KEK in git/logs |
| Invite spam burns domain deliverability | medium | high (recovery mail dies) | per-inviter 20/day + per-recipient 3/day limits |
| Silent cron death | medium | audit/backup drift | heartbeat + veil-monitor alerts |
| Insider/agent exfiltration | medium | scoped to grants | agents never see secrets (leak-tested); audit append-only |
| Founder unavailable | low | recovery stalls | bus-factor section in incident-response.md |
| Dependency supply chain | medium | varies | govulncheck in CI; pinned toolchain + images |
| Solo-operator change risk | medium | regression ships | CI gate + tests-first rule; consider PR review once a second committer exists |

## CompAI note

Evaluated `trycompai/comp` (AGPLv3, SOC2/ISO27001/GDPR automation) as the
tracking surface for this register. Self-hosting requires external
Postgres with SSL, a Resend account, and a Trigger.dev cloud account —
three new external dependencies to run a checklist. Decision: keep the
register in-repo for alpha; adopt CompAI (or equivalent) when a customer
asks for SOC 2 evidence and the integration cost pays for itself.
