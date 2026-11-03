#!/usr/bin/env node
/**
 * Load service-scoped Railway variables into process env (for Alchemy plan/deploy).
 * Does not print values. Appends KEY=value lines to stdout for `>> $GITHUB_ENV`.
 *
 * Usage: node scripts/load-railway-service-env.mjs <service> [VAR ...]
 * Default VAR list: all keys on the service (excludes RAILWAY_*).
 */
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

const service = process.argv[2];
if (!service) {
  process.stderr.write("usage: load-railway-service-env.mjs <service> [VAR ...]\n");
  process.exit(1);
}

const allow = process.argv.slice(3);
const project = process.env.RAILWAY_PROJECT_ID?.trim() || "92578292-9436-407b-af3e-0f7b19a74289";
const environment = process.env.RAILWAY_ENVIRONMENT?.trim() || "production";

const escrowPath =
  process.env.VEIL_ENV_ESCROW?.trim() ||
  join(homedir(), ".config/vortex/veil-env-escrow.json");

function writePairs(doc, keys) {
  for (const key of keys) {
    const value = doc[key];
    if (typeof value !== "string") continue;
    const encoded = value.replace(/\n/g, "%0A").replace(/\r/g, "");
    process.stdout.write(`${key}=${encoded}\n`);
  }
}

function escrowBucket() {
  try {
    const escrow = JSON.parse(readFileSync(escrowPath, "utf8"));
    const bucket = escrow[service];
    return bucket && typeof bucket === "object" ? bucket : null;
  } catch {
    return null;
  }
}

/** True when every key has a string in escrow (empty string allowed). */
function escrowCovers(keys) {
  const bucket = escrowBucket();
  if (!bucket) return false;
  for (const key of keys) {
    if (!(key in bucket) || typeof bucket[key] !== "string") return false;
  }
  return true;
}

function loadFromEscrow(keys, label) {
  const bucket = escrowBucket();
  if (!bucket || !escrowCovers(keys)) return false;
  process.stderr.write(`load-railway-service-env: ${label} for ${service}\n`);
  writePairs(bucket, keys);
  return true;
}

const forceLive = process.env.VEIL_RAILWAY_FORCE_LIVE === "1";
const planSource = process.env.VEIL_RAILWAY_PLAN_SOURCE?.trim() || "";
/** Local deploy/plan: never hit Railway variable list when escrow is complete. */
const strictEscrow = planSource === "escrow";
/** CI and optional local: escrow when present, else Railway CLI. */
const preferEscrow = strictEscrow || planSource === "escrow-first";

if (allow.length > 0 && !forceLive && (preferEscrow || escrowCovers(allow))) {
  if (loadFromEscrow(allow, strictEscrow ? "escrow (strict)" : "escrow-first")) {
    process.exit(0);
  }
  if (strictEscrow) {
    process.stderr.write(
      `escrow incomplete for ${service} — run pnpm run merge:keysvc-escrow or refresh escrow\n`,
    );
    process.exit(1);
  }
  process.stderr.write(
    `load-railway-service-env: escrow miss for ${service}, using Railway CLI\n`,
  );
}

const list = spawnSync(
  "railway",
  ["variable", "list", "-s", service, "-p", project, "-e", environment, "--json"],
  { encoding: "utf8", env: process.env },
);

const rateLimited =
  (list.stderr && list.stderr.includes("ratelimited")) ||
  (list.stdout && list.stdout.includes("ratelimited"));

if (list.status !== 0 || rateLimited) {
  const keys = allow.length > 0 ? allow : [];
  if (keys.length === 0) {
    process.stderr.write(list.stderr || list.stdout || "railway variable list failed\n");
    process.exit(list.status ?? 1);
  }
  if (!loadFromEscrow(keys, "escrow fallback")) {
    process.stderr.write(list.stderr || list.stdout || "railway variable list failed\n");
    process.exit(list.status ?? 1);
  }
  process.exit(0);
}

/** @type {Record<string, string>} */
let doc;
try {
  doc = JSON.parse(list.stdout);
} catch {
  process.stderr.write("invalid railway variable list JSON\n");
  process.exit(1);
}

const keys =
  allow.length > 0
    ? allow
    : Object.keys(doc).filter((k) => !k.startsWith("RAILWAY_") && k !== "ALCHEMY_RPC_TOKEN");

writePairs(doc, keys);
