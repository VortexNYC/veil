#!/usr/bin/env node
/** Print preserve() migration checklist (no secrets). */
import { preserveManifest } from "../src/preserve-manifest.ts";

let total = 0;
for (const [service, names] of Object.entries(preserveManifest)) {
  console.log(`\n${service} (${names.length})`);
  for (const name of names) {
    console.log(`  - ${name}`);
    total += 1;
  }
}
console.log(`\nTotal: ${total} variables → Railway.Variable adopt slice`);
