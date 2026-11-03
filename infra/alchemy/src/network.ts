import * as Railway from "@alchemy.run/railway";
import type { Project } from "@alchemy.run/railway/Project";
import type { Service } from "@alchemy.run/railway";
import * as Effect from "effect/Effect";

/** Platform private mesh + Postgres DNS prefix (`postgres`, matches `.railway/railway.ts`). */
export const defineProductionNetwork = (veilProject: Project, postgres: Service) =>
  Effect.gen(function* () {
    const mesh = yield* Railway.PrivateNetwork("PrivateMesh", {
      environment: veilProject,
    });

    const postgresEndpoint = yield* Railway.PrivateNetworkEndpoint("PostgresPrivateDns", {
      network: mesh,
      service: postgres,
      name: "postgres",
    });

    return { mesh, postgresEndpoint };
  });
