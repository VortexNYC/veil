#!/usr/bin/env node
/** Add `branch: main` to committed Railway.Service props that use VortexNYC/veil. */
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(fileURLToPath(new URL(".", import.meta.url)), "..");
const dst = join(root, "state/ci");
const branch = process.env.VEIL_RAILWAY_REPO_BRANCH?.trim() || "main";
const repo = "VortexNYC/veil";

let patched = 0;
for (const name of readdirSync(dst).filter((n) => n.endsWith(".json"))) {
  const path = join(dst, name);
  const doc = JSON.parse(readFileSync(path, "utf8"));
  if (doc.resourceType !== "Railway.Service") continue;
  if (doc.props?.repo !== repo) continue;
  if (doc.props.branch === branch) continue;
  doc.props.branch = branch;
  writeFileSync(path, `${JSON.stringify(doc, null, 2)}\n`);
  patched += 1;
}

process.stdout.write(`sync-branch-in-ci-state: patched ${patched} service file(s) → branch ${branch}\n`);
