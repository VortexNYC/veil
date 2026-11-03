#!/usr/bin/env node
/** Copy shared APNS/monitor fields from escrow veil → veil-monitor; fill known empties. */
import { readFileSync, writeFileSync, chmodSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

const escrowPath =
  process.env.VEIL_ENV_ESCROW?.trim() ||
  join(homedir(), ".config/vortex/veil-env-escrow.json");

/** @type {Record<string, Record<string, string>>} */
let escrow;
try {
  escrow = JSON.parse(readFileSync(escrowPath, "utf8"));
} catch {
  process.stderr.write(`cannot read escrow at ${escrowPath}\n`);
  process.exit(1);
}
const veil = escrow.veil;
if (!veil || typeof veil !== "object") {
  process.stderr.write("escrow missing veil bucket\n");
  process.exit(1);
}
if (!escrow["veil-monitor"] || typeof escrow["veil-monitor"] !== "object") {
  escrow["veil-monitor"] = {};
}
const mon = escrow["veil-monitor"];
const shared = [
  "VEIL_APNS_KEY",
  "VEIL_APNS_KEY_ID",
  "VEIL_APNS_TEAM_ID",
  "VEIL_APNS_BUNDLE",
  "VEIL_APNS_PROD",
  "POSTHOG_PROJECT_KEY",
  "VEIL_ALERT_TO",
  "VEIL_NTFY_TOPIC",
];
let patched = 0;
for (const key of shared) {
  if (typeof mon[key] === "string" && mon[key].length > 0) continue;
  if (typeof veil[key] === "string" && veil[key].length > 0) {
    mon[key] = veil[key];
    patched += 1;
  }
}
if (!("VEIL_APNS_KEY_PATH" in mon)) {
  mon.VEIL_APNS_KEY_PATH = typeof veil.VEIL_APNS_KEY_PATH === "string" ? veil.VEIL_APNS_KEY_PATH : "";
  patched += 1;
}
if (!("VEIL_APNS_PROD" in mon) || mon.VEIL_APNS_PROD === "") {
  mon.VEIL_APNS_PROD = typeof veil.VEIL_APNS_PROD === "string" && veil.VEIL_APNS_PROD ? veil.VEIL_APNS_PROD : "1";
  patched += 1;
}
writeFileSync(escrowPath, `${JSON.stringify(escrow, null, 2)}\n`, { mode: 0o600 });
try {
  chmodSync(escrowPath, 0o600);
} catch {
  /* ok */
}
process.stderr.write(`merge-monitor-escrow-gaps: patched ${patched} field(s) on veil-monitor\n`);
