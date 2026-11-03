#!/usr/bin/env node
/**
 * Merge veil-monitor Railway vars into process.env (values never printed).
 */
import { spawnSync } from "node:child_process";
import { fileURLToPath, pathToFileURL } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

const namesRun = spawnSync(
  "node",
  [
    "--experimental-strip-types",
    "-e",
    "import { monitorSharedVarNames } from './src/variables/monitor-names.ts'; process.stdout.write(monitorSharedVarNames.join('\\n'));",
  ],
  { cwd: root, encoding: "utf8" },
);
if (namesRun.status !== 0) {
  process.stderr.write(namesRun.stderr || "monitor var names failed\n");
  process.exit(namesRun.status ?? 1);
}
const varNames = namesRun.stdout.trim().split("\n").filter(Boolean);

const load = spawnSync(
  "node",
  ["scripts/load-railway-service-env.mjs", "veil-monitor", ...varNames],
  { cwd: root, encoding: "utf8", env: process.env },
);
if (load.status !== 0) {
  process.stderr.write(load.stderr || "load monitor env failed\n");
  process.exit(load.status ?? 1);
}

/** @type {Record<string, string>} */
const extra = {};
for (const line of load.stdout.split("\n")) {
  if (!line.includes("=")) continue;
  const i = line.indexOf("=");
  const key = line.slice(0, i);
  const raw = line.slice(i + 1);
  extra[key] = raw.replace(/%0A/g, "\n");
}

export function monitorEnvFromRailway(baseEnv = process.env) {
  return { ...baseEnv, ...extra };
}

const isMain =
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href;

if (isMain) {
  for (const [key, value] of Object.entries(extra)) {
    const encoded = value.replace(/\n/g, "%0A").replace(/\r/g, "");
    process.stdout.write(`${key}=${encoded}\n`);
  }
}
