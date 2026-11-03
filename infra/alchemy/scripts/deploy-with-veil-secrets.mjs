#!/usr/bin/env node
/**
 * Run `alchemy deploy --adopt` with backup-plane secrets from Veil (never printed).
 *
 *   VEIL_OFFSITE_ITEM — default "Veil backup-ingest token" (OFFSITE + audit ingest)
 */
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { monitorEnvFromRailway } from "./load-monitor-env.mjs";
import { identityEnvFromRailway } from "./load-identity-env.mjs";
import { brokerEnvFromRailway } from "./load-broker-env.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const item = process.env.VEIL_OFFSITE_ITEM?.trim() || "Veil backup-ingest token";

function revealPassword(itemName) {
  const reveal = spawnSync("veil", ["item", "reveal", itemName], {
    encoding: "utf8",
    env: {
      ...process.env,
      VEIL_ORIGIN: process.env.VEIL_ORIGIN?.trim() || "https://veil.nyc",
    },
  });
  if (reveal.status !== 0) {
    process.stderr.write(reveal.stderr || "veil item reveal failed\n");
    process.exit(reveal.status ?? 1);
  }
  const match = reveal.stdout.match(/^password:\s*(.+)$/m);
  const token = match?.[1]?.trim();
  if (!token) {
    process.stderr.write(`No password on Veil item ${itemName}\n`);
    process.exit(1);
  }
  return token;
}

const backupIngestToken =
  process.env.VEIL_OFFSITE_TOKEN?.trim() ||
  process.env.OFFSITE_TOKEN?.trim() ||
  revealPassword(item);

const deployArgs = process.argv.slice(2);
const alchemyArgs =
  deployArgs.length > 0 ? deployArgs : ["deploy", "--adopt", "-y", "--no-input"];

const run = spawnSync("pnpm", ["exec", "alchemy", ...alchemyArgs], {
  cwd: root,
  encoding: "utf8",
  env: brokerEnvFromRailway(
    identityEnvFromRailway(
      monitorEnvFromRailway({
        ...process.env,
        VEIL_OFFSITE_TOKEN: backupIngestToken,
        VEIL_AUDIT_INGEST_TOKEN: backupIngestToken,
      }),
    ),
  ),
});
if (run.stdout) process.stdout.write(run.stdout);
if (run.stderr) process.stderr.write(run.stderr);
process.exit(run.status ?? 1);
