import * as Railway from "@alchemy.run/railway";
import type { Project } from "@alchemy.run/railway/Project";
import type { VariableRefSource } from "@alchemy.run/railway/ref";
import * as Effect from "effect/Effect";
import * as Redacted from "effect/Redacted";
import { railwayPlainEnvKey } from "./railway-env-keys.ts";

export const serviceVarLogicalId = (prefix: string, name: string) =>
  `${prefix}${name.replace(/[^A-Za-z0-9]/g, "")}`;

export const serviceEnvRefs = (
  source: VariableRefSource,
  names: readonly string[],
): Record<string, string> =>
  Object.fromEntries(names.map((name) => [name, Railway.ref(source, name)]));

const plainFor = (railwayService: string, name: string): string =>
  process.env[railwayPlainEnvKey(railwayService, name)]?.trim() ?? "";

export const defineServiceScopedVariables = (
  project: Project,
  service: object,
  railwayService: string,
  logicalPrefix: string,
  names: readonly string[],
  optionalEmpty: ReadonlySet<string> = new Set(),
) =>
  Effect.gen(function* () {
    const resources: unknown[] = [];
    for (const name of names) {
      const plain = plainFor(railwayService, name);
      if (!plain && !optionalEmpty.has(name)) {
        return yield* Effect.fail(
          new Error(
            `missing ${railwayPlainEnvKey(railwayService, name)} — load Railway env before deploy`,
          ),
        );
      }
      resources.push(
        yield* Railway.Variable(serviceVarLogicalId(logicalPrefix, name), {
          project,
          service,
          name,
          value: Redacted.make(plain),
        }),
      );
    }
    return { resources };
  });
