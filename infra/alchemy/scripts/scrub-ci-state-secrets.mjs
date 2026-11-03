#!/usr/bin/env node
/** Re-scrub infra/alchemy/state/ci for secretlint/gitleaks (in-place). */
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { digestForGit } from "./digest-for-git.mjs";

const dst = join(fileURLToPath(new URL(".", import.meta.url)), "..", "state/ci");

let touched = 0;

const scrubNode = (value) => {
  if (Array.isArray(value)) {
    for (const item of value) scrubNode(item);
    return;
  }
  if (!value || typeof value !== "object") return;
  if (typeof value.digest === "string") {
    const next = digestForGit(value.digest);
    if (next !== value.digest) {
      value.digest = next;
      touched += 1;
    }
  }
  if (value.__redacted__ != null && typeof value.__redacted__ === "string") {
    const next = digestForGit(value.__redacted__);
    if (next !== value.__redacted__) {
      value.__redacted__ = next;
      touched += 1;
    }
  }
  for (const v of Object.values(value)) scrubNode(v);
};

for (const name of readdirSync(dst).filter((n) => n.endsWith(".json"))) {
  const path = join(dst, name);
  const doc = JSON.parse(readFileSync(path, "utf8"));
  const before = JSON.stringify(doc);
  scrubNode(doc);
  if (JSON.stringify(doc) !== before) {
    writeFileSync(path, `${JSON.stringify(doc, null, 2)}\n`);
  }
}

process.stdout.write(`scrub-ci-state-secrets: updated ${touched} digest field(s)\n`);
