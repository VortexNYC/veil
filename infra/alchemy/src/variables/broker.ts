import type { Project } from "@alchemy.run/railway/Project";
import {
  brokerLogicalPrefix,
  brokerRailwayService,
  brokerServiceVarNames,
} from "./broker-names.ts";
import { brokerOptionalEmptyVarNames } from "./broker-env-defaults.ts";
import { defineServiceScopedVariables } from "./service-vars.ts";

export const defineBrokerVariables = (project: Project, veilOrigin: object) =>
  defineServiceScopedVariables(
    project,
    veilOrigin,
    brokerRailwayService,
    brokerLogicalPrefix,
    brokerServiceVarNames,
    brokerOptionalEmptyVarNames,
  );
