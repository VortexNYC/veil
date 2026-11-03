import * as Railway from "@alchemy.run/railway";
import type { Project } from "@alchemy.run/railway/Project";
import * as Effect from "effect/Effect";
import * as Redacted from "effect/Redacted";
import {
  monitorLogicalId,
  monitorSharedVarNames,
  type MonitorSharedVarName,
} from "./monitor-names.ts";

export const monitorVarRef = (name: MonitorSharedVarName) =>
  Railway.ref("shared", name);

const envPlain = (name: MonitorSharedVarName): string =>
  process.env[name]?.trim() ?? "";

/** Shared Variables for veil-monitor pager / analytics env. */
export const defineMonitorVariables = (veilProject: Project) =>
  Effect.gen(function* () {
    const entries: { name: MonitorSharedVarName; resource: unknown }[] = [];
    for (const name of monitorSharedVarNames) {
      const resource = yield* Railway.Variable(monitorLogicalId(name), {
        project: veilProject,
        name,
        value: Redacted.make(envPlain(name)),
      });
      entries.push({ name, resource });
    }
    const refs = Object.fromEntries(
      monitorSharedVarNames.map((name) => [name, monitorVarRef(name)]),
    ) as Record<MonitorSharedVarName, string>;
    return {
      resources: entries.map((e) => e.resource),
      refs,
    };
  });
