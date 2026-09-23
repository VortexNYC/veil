# Incident response

The runbook has the verbs. This doc has the judgment: severity, who
tells whom, and what a clean incident looks like. Alpha is invite-only —
"customers" means a handful of orgs you can email directly. That is an
advantage: comms can be personal, honest, and fast.

## Severity ladder

| Sev | Definition | Examples | Response |
|---|---|---|---|
| **S1** | Secrets exposed, or data lost, or auth bypass | KEK leaked; audit shows Use by a principal that had no grant; ciphertext returned to an agent; purge hit the wrong org | Stop the bleed first (kill-switch, rotate, revoke). Comms within **24h** to affected orgs — what happened, what leaked, what we did, what they must do. Postmortem mandatory. |
| **S2** | Control broken, no confirmed exposure | Missing backup heartbeat; audit relay down (outbox growing); session token outlived revocation; Keto unreachable so authz fails open/closed incorrectly | Fix inside a day. Comms only if customer-visible. Postmortem if the cause wasn't obvious. |
| **S3** | Degraded but correct | Origin 5xx window elevated; sweep late; R2 upload failed but volume dump fine | Fix on the normal track. Log it. |

When unsure between S1 and S2, treat as S1 for 30 minutes while you
prove which it is. Downgrading later is free; upgrading late is not.

## First hour — the checklist

1. **Contain.** Pick the verb before you investigate:
   - Compromised human → runbook kill-switch (Kratos inactive → revoke
     agents → drop Keto tuples → drop humans row).
   - Compromised org → surviving owner runs `DELETE /v1/org`, or operator
     does the Keto wipe + `PurgeOrg` by hand.
   - KEK suspicion → `veil key rotate-kek` (docs/key-rotation.md) +
     coordinated redeploy.
   - Origin bad → `railway service redeploy --service veil` to last good.
   - Postgres bad → the store fails closed; no secret leaves unaudited.
     Fix Postgres, everything recovers.
2. **Preserve.** Snapshot before you clean: `audit` rows for the org in
   the window (append-only, detached partitions still read), Railway
   logs (`railway logs --service veil`), Keto tuples, the `humans` row.
   Teardown never deletes audit — that is deliberate.
3. **Decide the sev** from the ladder. S1 means comms drafting starts
   now, not after root cause.

## Comms

S1 template — send to every affected org owner, from you, plain text:

```text
Subject: Veil security incident — <date>

What happened: <one sentence, no jargon>
What was exposed: <exactly which items/orgs, or "we have no evidence
  any secret left the vault">
What we did: <containment verb + when>
What you must do: <rotate these credentials / nothing / re-onboard>
Timeline: <detected X, contained Y>
Postmortem follows within 5 days.
```

Rules: name what leaked or say "no evidence of exposure" — never vague
"might have been affected." If secrets did leave the vault, the "what
you must do" line is a rotation list, per item, per grant. The audit
table can produce that list — use it.

## Postmortem (S1 mandatory, S2 if non-obvious)

```markdown
# Postmortem — <date>

## What happened
## Timeline (detected / contained / resolved)
## Root cause
## What worked
## What didn't
## Action items (each: owner, due)
```

Blameless: the question is always "what property of the system allowed
this," never "who fumbled." An incident where the fail-closed path
fired and nothing leaked is a *success story* — write it down too;
that's the evidence an auditor wants to see.

## Bus factor

If the founder is unavailable, the recovery story is:

1. `VEIL_KEK` is escrowed in 1Password (`Veil KEK (escrow)`) — vault
   access is the choke point; a trusted second must hold it.
2. Railway project access + this repo + `docs/runbook.md` +
   `docs/backup-restore.md` are the entire operational surface.
3. Restore = `pg_restore` all four DBs + boot origin with the KEK.
   The drill doc is the procedure; it was proven on a real dump.

Write the second person down by name in 1Password next to the KEK.
Undocumented trust is the same as no recovery path.
