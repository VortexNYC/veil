#!/usr/bin/env node
/** Patch veil escrow with broker non-secret defaults (never prints secrets). */
import { readFileSync, writeFileSync, chmodSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

const root = join(import.meta.dirname, "..");
const escrowPath =
  process.env.VEIL_ENV_ESCROW?.trim() ||
  join(homedir(), ".config/vortex/veil-env-escrow.json");

const defaultsRun = spawnSync(
  "node",
  [
    "--experimental-strip-types",
    "-e",
    "import { brokerEnvDefaults } from './src/variables/broker-env-defaults.ts'; process.stdout.write(JSON.stringify(brokerEnvDefaults));",
  ],
  { cwd: root, encoding: "utf8" },
);
if (defaultsRun.status !== 0) process.exit(defaultsRun.status ?? 1);
const defaults = JSON.parse(defaultsRun.stdout);

/** @type {Record<string, Record<string, string>>} */
let escrow;
try {
  escrow = JSON.parse(readFileSync(escrowPath, "utf8"));
} catch {
  process.stderr.write(`cannot read escrow at ${escrowPath}\n`);
  process.exit(1);
}
if (!escrow.veil || typeof escrow.veil !== "object") escrow.veil = {};
let patched = 0;
for (const [key, value] of Object.entries(defaults)) {
  if (key in escrow.veil && typeof escrow.veil[key] === "string") continue;
  escrow.veil[key] = value;
  patched += 1;
}
writeFileSync(escrowPath, `${JSON.stringify(escrow, null, 2)}\n`, { mode: 0o600 });
try {
  chmodSync(escrowPath, 0o600);
} catch {
  /* ok */
}
process.stderr.write(`merge-broker-escrow-gaps: patched ${patched} default(s) on veil\n`);
