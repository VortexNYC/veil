import * as Railway from "@alchemy.run/railway";
import type { Project } from "@alchemy.run/railway/Project";
import * as Effect from "effect/Effect";
import * as Redacted from "effect/Redacted";

/** backup-ingest bearer for audit-export cron (`POST` audit objects). */
export const auditIngestTokenRef = Railway.ref("shared", "VEIL_AUDIT_INGEST_TOKEN");

const auditIngestPlaintext = (): string | undefined => {
  const v =
    process.env.VEIL_AUDIT_INGEST_TOKEN?.trim() ??
    process.env.OFFSITE_TOKEN?.trim();
  return v && v.length > 0 ? v : undefined;
};

/** Shared env var on `veil-audit-export` (same worker family as OFFSITE_TOKEN). */
export const defineAuditIngestTokenVariable = (veilProject: Project) =>
  Effect.gen(function* () {
    const plain = auditIngestPlaintext();
    const shared = yield* Railway.Variable("AuditIngestTokenShared", {
      project: veilProject,
      name: "VEIL_AUDIT_INGEST_TOKEN",
      value: Redacted.make(plain ?? ""),
    });
    return { shared };
  });
