#!/usr/bin/env node
/**
 * Load broker (`veil` service) vars into prefixed process env (never prints values).
 */
import { spawnSync } from "node:child_process";
import { fileURLToPath, pathToFileURL } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

/** Veil item password → VEIL_KEYSVC_TOKEN when Railway/escrow omit it. */
function keysvcTokenFromVeil() {
  const item = process.env.VEIL_KEYSVC_ITEM?.trim() || "veil-key-service-token";
  const reveal = spawnSync("veil", ["item", "reveal", item], {
    encoding: "utf8",
    env: {
      ...process.env,
      VEIL_ORIGIN: process.env.VEIL_ORIGIN?.trim() || "https://veil.nyc",
    },
  });
  if (reveal.status !== 0) return "";
  const match = reveal.stdout.match(/^password:\s*(.+)$/m);
  return match?.[1]?.trim() ?? "";
}

const namesRun = spawnSync(
  "node",
  [
    "--experimental-strip-types",
    "-e",
    "import { brokerServiceVarNames, brokerRailwayService } from './src/variables/broker-names.ts'; process.stdout.write(JSON.stringify({ names: brokerServiceVarNames, service: brokerRailwayService }));",
  ],
  { cwd: root, encoding: "utf8" },
);
if (namesRun.status !== 0) process.exit(namesRun.status ?? 1);
const { names, service } = JSON.parse(namesRun.stdout);

const load = spawnSync(
  "node",
  ["scripts/load-railway-service-env.mjs", service, ...names],
  { cwd: root, encoding: "utf8", env: process.env },
);
if (load.status !== 0) {
  process.stderr.write(load.stderr || "load broker env failed\n");
  process.exit(load.status ?? 1);
}

/** @type {Record<string, string>} */
const extra = {};
for (const line of load.stdout.split("\n")) {
  if (!line.includes("=")) continue;
  const i = line.indexOf("=");
  const key = line.slice(0, i);
  const raw = line.slice(i + 1);
  extra[`VEIL_RAILWAY_${service}_${key}`] = raw.replace(/%0A/g, "\n");
}

const defaultsRun = spawnSync(
  "node",
  [
    "--experimental-strip-types",
    "-e",
    "import { brokerEnvDefaults } from './src/variables/broker-env-defaults.ts'; process.stdout.write(JSON.stringify(brokerEnvDefaults));",
  ],
  { cwd: root, encoding: "utf8" },
);
if (defaultsRun.status === 0) {
  const defaults = JSON.parse(defaultsRun.stdout);
  for (const [key, value] of Object.entries(defaults)) {
    const envKey = `VEIL_RAILWAY_${service}_${key}`;
    if (!extra[envKey]?.trim() && value !== "") extra[envKey] = value;
    else if (!(envKey in extra) && value === "") extra[envKey] = "";
  }
}

const keysvcEnvKey = `VEIL_RAILWAY_${service}_VEIL_KEYSVC_TOKEN`;
if (!extra[keysvcEnvKey]?.trim()) {
  const fromEnv = process.env.VEIL_KEYSVC_TOKEN?.trim();
  if (fromEnv) extra[keysvcEnvKey] = fromEnv;
  else {
    const token = keysvcTokenFromVeil();
    if (token) extra[keysvcEnvKey] = token;
  }
}

export function brokerEnvFromRailway(baseEnv = process.env) {
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
