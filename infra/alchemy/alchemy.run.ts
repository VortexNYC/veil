import * as Alchemy from "alchemy";
import * as Railway from "@alchemy.run/railway";
import * as Effect from "effect/Effect";
import { defineProductionPlane } from "./src/plane.ts";

/**
 * Veil production Railway plane — Alchemy management path.
 * Parity target: `.railway/railway.ts`. Secrets and preserve() vars stay
 * on Railway until explicitly declared here; Service env only sets literals
 * and ${{Postgres.*}} templates that match the IaC file.
 */
export default Alchemy.Stack(
  "VeilRailway",
  {
    providers: Railway.providers(),
    state: Alchemy.localState(),
  },
  Effect.gen(function* () {
    const veilProject = yield* Railway.Project("VeilProd", {
      name: "veil",
      description: "Veil production — origin, identity, store",
    });

    const plane = yield* defineProductionPlane(veilProject);

    return {
      projectId: veilProject.projectId,
      environmentId: veilProject.environmentId,
      url: veilProject.url,
      postgresServiceId: plane.postgres.serviceId,
      hydraServiceId: plane.hydra.serviceId,
      veilServiceId: plane.veilOrigin.serviceId,
      groups: {
        identity: plane.groups.identity.groupId,
        store: plane.groups.store.groupId,
        broker: plane.groups.broker.groupId,
        backupPlane: plane.groups.backupPlane.groupId,
      },
    };
  }),
);
