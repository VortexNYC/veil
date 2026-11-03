#!/usr/bin/env node
/** Add veil.VEIL_KEYSVC_TOKEN to local escrow from Veil item (never prints value). */
import { readFileSync, writeFileSync, chmodSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

const escrowPath =
  process.env.VEIL_ENV_ESCROW?.trim() ||
  join(homedir(), ".config/vortex/veil-env-escrow.json");
const item = process.env.VEIL_KEYSVC_ITEM?.trim() || "veil-key-service-token";

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

/** @type {Record<string, Record<string, string>>} */
let escrow;
try {
  escrow = JSON.parse(readFileSync(escrowPath, "utf8"));
} catch {
  process.stderr.write(`cannot read escrow at ${escrowPath}\n`);
  process.exit(1);
}
if (!escrow.veil || typeof escrow.veil !== "object") escrow.veil = {};
escrow.veil.VEIL_KEYSVC_TOKEN = token;
writeFileSync(escrowPath, `${JSON.stringify(escrow, null, 2)}\n`, { mode: 0o600 });
try {
  chmodSync(escrowPath, 0o600);
} catch {
  /* ok */
}
process.stderr.write("merge-keysvc-into-escrow: updated veil.VEIL_KEYSVC_TOKEN in escrow\n");
