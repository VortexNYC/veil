import * as Railway from "@alchemy.run/railway";
import type { Project } from "@alchemy.run/railway/Project";
import * as Effect from "effect/Effect";
import * as Redacted from "effect/Redacted";

/** Railway template for backup-ingest bearer on backup/wal services. */
export const offsiteTokenRef = Railway.ref("shared", "OFFSITE_TOKEN");

const offsitePlaintext = (): string | undefined => {
  const v =
    process.env.VEIL_OFFSITE_TOKEN?.trim() ?? process.env.OFFSITE_TOKEN?.trim();
  return v && v.length > 0 ? v : undefined;
};

/**
 * Project-level OFFSITE_TOKEN (backup-ingest Bearer). Set `VEIL_OFFSITE_TOKEN`
 * at deploy via `pnpm run deploy:secrets`. Plaintext never in git; state stores
 * digest only. Always declared so CI plan does not drop the resource.
 */
export const defineOffsiteTokenVariable = (veilProject: Project) =>
  Effect.gen(function* () {
    const plain = offsitePlaintext();
    const shared = yield* Railway.Variable("OffsiteTokenShared", {
      project: veilProject,
      name: "OFFSITE_TOKEN",
      value: Redacted.make(plain ?? ""),
    });
    return { shared };
  });
