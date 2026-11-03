#!/usr/bin/env node
/** Set GitHub repo secret VEIL_OFFSITE_TOKEN for Alchemy CI plan (from Veil item). */
import { spawnSync } from "node:child_process";

const repoArg = process.argv.indexOf("--repo");
const repo = repoArg >= 0 ? process.argv[repoArg + 1] : "VortexNYC/veil";
const item = process.env.VEIL_OFFSITE_ITEM?.trim() || "Veil backup-ingest token";

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
  "gh",
  ["secret", "set", "VEIL_OFFSITE_TOKEN", "-R", repo, "--body", token],
  { encoding: "utf8" },
);
if (set.status !== 0) {
  process.stderr.write(set.stderr || set.stdout || "gh secret set failed\n");
  process.exit(set.status ?? 1);
}

console.log(`Set VEIL_OFFSITE_TOKEN on ${repo} (value not printed).`);
