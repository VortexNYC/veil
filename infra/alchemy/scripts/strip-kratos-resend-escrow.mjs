#!/usr/bin/env node
/** Drop legacy RESEND_* from kratos escrow bucket (never prints values). */
import { chmodSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

const escrowPath =
  process.env.VEIL_ENV_ESCROW_PATH?.trim() ||
  join(homedir(), ".config/vortex/veil-env-escrow.json");

let escrow;
try {
  escrow = JSON.parse(readFileSync(escrowPath, "utf8"));
} catch {
  process.stderr.write(`cannot read escrow at ${escrowPath}\n`);
  process.exit(1);
}
if (!escrow.kratos || typeof escrow.kratos !== "object") {
  process.stdout.write("strip-kratos-resend-escrow: no kratos bucket\n");
  process.exit(0);
}
let removed = 0;
for (const key of ["RESEND_API_KEY", "RESEND_AUTHORIZATION"]) {
  if (key in escrow.kratos) {
    delete escrow.kratos[key];
    removed += 1;
  }
}
if (removed === 0) {
  process.stdout.write("strip-kratos-resend-escrow: nothing to remove\n");
  process.exit(0);
}
writeFileSync(escrowPath, `${JSON.stringify(escrow, null, 2)}\n`, { mode: 0o600 });
try {
  chmodSync(escrowPath, 0o600);
} catch {
  // ok
}
process.stdout.write(`strip-kratos-resend-escrow: removed ${removed} key(s)\n`);
