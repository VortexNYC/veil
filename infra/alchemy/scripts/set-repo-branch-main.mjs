#!/usr/bin/env node
/**
 * Wire GitHub deploy triggers to branch `main` on all Dockerfile/repo services.
 * Uses Railway CLI (user session) — works when Alchemy OAuth/API hit
 * "no one in the project has access" on deployment trigger create.
 *
 * Then sync IaC: VEIL_RAILWAY_REPO_BRANCH=main pnpm run deploy:secrets (OAuth profile, offsite in env)
 * or patch state via `node scripts/sync-branch-in-ci-state.mjs` after live Railway matches.
 */
import { spawnSync } from "node:child_process";

const REPO = "VortexNYC/veil";
const BRANCH = process.env.VEIL_RAILWAY_REPO_BRANCH?.trim() || "main";

const services = [
  "kratos",
  "keto",
  "glue",
  "veil",
  "veil-migrate",
  "veil-sweep",
  "veil-backup",
  "veil-wal",
  "veil-wal-dr",
  "veil-audit-export",
  "veil-monitor",
];

for (const service of services) {
  process.stderr.write(`connect ${REPO}@${BRANCH} → ${service}\n`);
  const r = spawnSync(
    "railway",
    ["service", "source", "connect", "--repo", REPO, "--branch", BRANCH, "--service", service],
    { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] },
  );
  if (r.status !== 0) {
    process.stderr.write(r.stderr || r.stdout || `failed: ${service}\n`);
    process.exit(r.status ?? 1);
  }
}

process.stdout.write(`GitHub sources on ${services.length} services → branch ${BRANCH}.\n`);
