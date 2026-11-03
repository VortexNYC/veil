#!/usr/bin/env node
/**
 * Load identity-plane service vars into prefixed process env (never prints values).
 * Keys: VEIL_RAILWAY_<service>_<VAR> (see identityPlainEnvKey).
 */
import { spawnSync } from "node:child_process";
import { fileURLToPath, pathToFileURL } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

const manifestRun = spawnSync(
  "node",
  [
    "--experimental-strip-types",
    "-e",
    "import { identityServiceVarNames, identityRailwayServices } from './src/variables/identity-names.ts'; process.stdout.write(JSON.stringify({ identityServiceVarNames, identityRailwayServices }));",
  ],
  { cwd: root, encoding: "utf8" },
);
if (manifestRun.status !== 0) {
  process.stderr.write(manifestRun.stderr || "identity manifest load failed\n");
  process.exit(manifestRun.status ?? 1);
}

/** @type {{ identityServiceVarNames: Record<string, string[]>, identityRailwayServices: string[] }} */
const { identityServiceVarNames, identityRailwayServices } = JSON.parse(manifestRun.stdout);

/** @type {Record<string, string>} */
const extra = {};

for (const service of identityRailwayServices) {
  const names = identityServiceVarNames[service];
  if (!names?.length) continue;
  const load = spawnSync(
    "node",
    ["scripts/load-railway-service-env.mjs", service, ...names],
    { cwd: root, encoding: "utf8", env: process.env },
  );
  if (load.status !== 0) {
    process.stderr.write(load.stderr || `load ${service} env failed\n`);
    process.exit(load.status ?? 1);
  }
  for (const line of load.stdout.split("\n")) {
    if (!line.includes("=")) continue;
    const i = line.indexOf("=");
    const key = line.slice(0, i);
    const raw = line.slice(i + 1);
    extra[`VEIL_RAILWAY_${service}_${key}`] = raw.replace(/%0A/g, "\n");
  }
}

export function identityEnvFromRailway(baseEnv = process.env) {
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
