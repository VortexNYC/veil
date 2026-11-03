#!/usr/bin/env node
/**
 * One-shot: set veil service VEIL_KEYSVC_TOKEN from Veil item (never prints value).
 * Run when Railway omits the var and Alchemy plan/deploy aborts on missing KEYSVC.
 */
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const project = process.env.RAILWAY_PROJECT_ID?.trim() || "92578292-9436-407b-af3e-0f7b19a74289";
const environment = process.env.RAILWAY_ENVIRONMENT?.trim() || "production";
const item = process.env.VEIL_KEYSVC_ITEM?.trim() || "veil-key-service-token";

const reveal = spawnSync("veil", ["item", "reveal", item], {
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
  process.stderr.write(`No password on Veil item ${item}\n`);
  process.exit(1);
}

const set = spawnSync(
  "railway",
  [
    "variable",
    "set",
    `VEIL_KEYSVC_TOKEN=${token}`,
    "-s",
    "veil",
    "-p",
    project,
    "-e",
    environment,
    "--skip-deploys",
  ],
  { cwd: root, encoding: "utf8" },
);
if (set.status !== 0) {
  process.stderr.write(set.stderr || set.stdout || "railway variable set failed\n");
  process.exit(set.status ?? 1);
}
process.stderr.write("sync-broker-keysvc: VEIL_KEYSVC_TOKEN set on Railway veil (--skip-deploys)\n");
