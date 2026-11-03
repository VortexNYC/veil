#!/usr/bin/env node
/**
 * Fill empty identity + broker Railway vars from local escrow JSON (never prints values).
 */
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

const root = join(import.meta.dirname, "..");
const project = "92578292-9436-407b-af3e-0f7b19a74289";
const environment = "production";
const escrowPath =
  process.env.VEIL_ENV_ESCROW?.trim() ||
  join(homedir(), ".config/vortex/veil-env-escrow.json");

const manifestRun = spawnSync(
  "node",
  [
    "--experimental-strip-types",
    "-e",
    "import { identityServiceVarNames, identityRailwayServices } from './src/variables/identity-names.ts'; import { brokerServiceVarNames, brokerRailwayService } from './src/variables/broker-names.ts'; process.stdout.write(JSON.stringify({ identityServiceVarNames, identityRailwayServices, brokerServiceVarNames, brokerRailwayService }));",
  ],
  { cwd: root, encoding: "utf8" },
);
if (manifestRun.status !== 0) process.exit(manifestRun.status ?? 1);
const {
  identityServiceVarNames,
  identityRailwayServices,
  brokerServiceVarNames,
  brokerRailwayService,
} = JSON.parse(manifestRun.stdout);

/** @type {Record<string, readonly string[]>} */
const serviceVarNames = {
  ...identityServiceVarNames,
  [brokerRailwayService]: brokerServiceVarNames,
};

const planeServices = [...identityRailwayServices, brokerRailwayService];

/** @type {Record<string, Record<string, string>>} */
let escrow;
try {
  escrow = JSON.parse(readFileSync(escrowPath, "utf8"));
} catch {
  process.stderr.write(`cannot read escrow at ${escrowPath}\n`);
  process.exit(1);
}

function listService(service) {
  const r = spawnSync(
    "railway",
    ["variable", "list", "-s", service, "-p", project, "-e", environment, "--json"],
    { encoding: "utf8" },
  );
  if (r.status !== 0) process.exit(r.status ?? 1);
  return JSON.parse(r.stdout);
}

function setVar(service, name, value) {
  const r = spawnSync(
    "railway",
    [
      "variable",
      "set",
      `${name}=${value}`,
      "-s",
      service,
      "-p",
      project,
      "-e",
      environment,
      "--skip-deploys",
    ],
    { encoding: "utf8", stdio: ["pipe", "pipe", "pipe"] },
  );
  if (r.status !== 0) process.exit(r.status ?? 1);
}

let restored = 0;
for (const service of planeServices) {
  const names = serviceVarNames[service];
  const bucket = escrow[service];
  if (!bucket) {
    process.stderr.write(`escrow missing "${service}"\n`);
    process.exit(1);
  }
  const live = listService(service);
  for (const name of names) {
    const current = live[name]?.trim() ?? "";
    if (current.length > 0) continue;
    const value = bucket[name]?.trim() ?? "";
    if (!value) {
      process.stderr.write(`escrow missing ${service}.${name}\n`);
      process.exit(1);
    }
    setVar(service, name, value);
    restored += 1;
  }
}

process.stdout.write(`plane escrow restore: ${restored} variable(s)\n`);
