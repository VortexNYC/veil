import type { Project } from "@alchemy.run/railway/Project";
import * as Effect from "effect/Effect";
import {
  identityLogicalPrefix,
  identityServiceVarNames,
  type IdentityRailwayService,
} from "./identity-names.ts";
import { defineServiceScopedVariables } from "./service-vars.ts";

/** Railway.Service handles from the production plane. */
export type IdentityServiceHandles = Record<IdentityRailwayService, object>;

export const defineIdentityVariables = (
  project: Project,
  services: IdentityServiceHandles,
) =>
  Effect.gen(function* () {
    const bundles: { resources: unknown[] }[] = [];
    for (const railwayService of Object.keys(
      identityServiceVarNames,
    ) as IdentityRailwayService[]) {
      bundles.push(
        yield* defineServiceScopedVariables(
          project,
          services[railwayService],
          railwayService,
          identityLogicalPrefix[railwayService],
          identityServiceVarNames[railwayService],
        ),
      );
    }
    return {
      resources: bundles.flatMap((b) => b.resources),
    };
  });
