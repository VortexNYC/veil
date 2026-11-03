import type { Project } from "@alchemy.run/railway/Project";
import * as Effect from "effect/Effect";
import { defineOffsiteTokenVariable } from "./offsite.ts";
import { defineAuditIngestTokenVariable } from "./audit-ingest.ts";
import { defineMonitorVariables } from "./monitor.ts";
import { defineIdentityVariables } from "./identity.ts";
import { defineBrokerVariables } from "./broker.ts";

export { offsiteTokenRef } from "./offsite.ts";
export { auditIngestTokenRef } from "./audit-ingest.ts";
export { monitorVarRef } from "./monitor.ts";
export { serviceEnvRefs } from "./service-vars.ts";

export const defineProductionVariables = (veilProject: Project) =>
  Effect.gen(function* () {
    const offsite = yield* defineOffsiteTokenVariable(veilProject);
    const auditIngest = yield* defineAuditIngestTokenVariable(veilProject);
    const monitor = yield* defineMonitorVariables(veilProject);
    return { offsite, auditIngest, monitor };
  });

export { defineIdentityVariables, defineBrokerVariables };
