#!/usr/bin/env node
/**
 * One command: merge KEYSVC into escrow → deploy broker adopt (escrow loaders, no CLI lists) → export CI state.
 * Stops on first failure. Does not retry on 429 — wait and re-run.
 */
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

function railwayTokenFromVeil() {
  if (process.env.RAILWAY_API_TOKEN?.trim()) return process.env.RAILWAY_API_TOKEN.trim();
  const reveal = spawnSync("veil", ["item", "reveal", "railway-alchemy-ci"], {
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

function run(label, cmd, args, extraEnv = {}) {
  process.stderr.write(`\n=== ${label} ===\n`);
  const r = spawnSync(cmd, args, {
    cwd: root,
    encoding: "utf8",
    env: {
      ...process.env,
      VEIL_RAILWAY_PLAN_SOURCE: "escrow",
      ...extraEnv,
    },
    stdio: ["inherit", "pipe", "pipe"],
  });
  if (r.stdout) process.stdout.write(r.stdout);
  if (r.stderr) process.stderr.write(r.stderr);
  if (r.status !== 0) {
    if (
      (r.stderr && r.stderr.includes("ratelimited")) ||
      (r.stdout && r.stdout.includes("ratelimited")) ||
      (r.stderr && r.stderr.includes("429"))
    ) {
      process.stderr.write(
        "\nRailway rate limit — wait 15–20 minutes, then run: pnpm run finish:broker\n",
      );
    }
    process.exit(r.status ?? 1);
  }
}

run("merge KEYSVC into escrow", "node", ["scripts/merge-keysvc-into-escrow.mjs"]);
run("patch broker defaults in escrow", "node", ["scripts/merge-broker-escrow-gaps.mjs"]);
run("patch monitor escrow from veil", "node", ["scripts/merge-monitor-escrow-gaps.mjs"]);

const token = railwayTokenFromVeil();
if (!token) {
  process.stderr.write("need Veil item railway-alchemy-ci or RAILWAY_API_TOKEN for Alchemy deploy\n");
  process.exit(1);
}

if (process.env.VEIL_FINISH_SKIP_SYNC !== "1") {
  run("sync KEYSVC to Railway veil", "node", ["scripts/sync-broker-keysvc-to-railway.mjs"], {
    RAILWAY_API_TOKEN: token,
  });
}

run("deploy broker + shared secrets (Alchemy)", "node", ["scripts/deploy-with-veil-secrets.mjs"], {
  RAILWAY_API_TOKEN: token,
});

run("export CI state", "pnpm", ["run", "export:ci-state"], {
  RAILWAY_API_TOKEN: token,
});

process.stderr.write("\nDone. Commit infra/alchemy/state/ci/ and push. pnpm plan should be clean.\n");
