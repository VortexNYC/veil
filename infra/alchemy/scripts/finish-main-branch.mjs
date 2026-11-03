#!/usr/bin/env node
/**
 * GitHub branch main: Railway CLI connect + sync branch into state/ci.
 * Alchemy deploy with `branch` still fails Railway GraphQL ("no project access") — CLI is the apply path.
 */
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

function run(label, cmd, args, extraEnv = {}) {
  process.stderr.write(`\n=== ${label} ===\n`);
  const env = {
    ...process.env,
    VEIL_RAILWAY_PLAN_SOURCE: "escrow",
    VEIL_RAILWAY_REPO_BRANCH: process.env.VEIL_RAILWAY_REPO_BRANCH?.trim() || "main",
    ...extraEnv,
  };
  if (extraEnv.RAILWAY_API_TOKEN === "") delete env.RAILWAY_API_TOKEN;
  const r = spawnSync(cmd, args, {
    cwd: root,
    encoding: "utf8",
    env,
    stdio: ["inherit", "pipe", "pipe"],
  });
  if (r.stdout) process.stdout.write(r.stdout);
  if (r.stderr) process.stderr.write(r.stderr);
  if (r.status !== 0) process.exit(r.status ?? 1);
}

run("Railway CLI branch connect", "node", ["scripts/set-repo-branch-main.mjs"]);

run("sync branch in state/ci", "node", ["scripts/sync-branch-in-ci-state.mjs"]);

process.stderr.write(
  "\nLive triggers are on main. Committed state updated.\n" +
    "Alchemy deploy with branch still hits Railway GraphQL trigger errors — use CLI above, not deploy.\n" +
    "Commit infra/alchemy/state/ci/ and push.\n",
);
