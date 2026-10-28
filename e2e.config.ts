import type { E2EConfig } from "e2e";
import { web } from "@e2e-dev/web";
import { mobile } from "@e2e-dev/mobile";
import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { createHmac } from "node:crypto";
import { createDevinExecutor } from "./e2e/devin-executor.ts";

const file = (env: string, path: string) =>
  process.env[env] ?? readFileSync(`${homedir()}${path}`, "utf8").trim();

function b32decode(s: string): Buffer {
  const A = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = 0, val = 0;
  const out: number[] = [];
  for (const c of s.toUpperCase().replace(/[^A-Z2-7]/g, "")) {
    val = (val << 5) | A.indexOf(c); bits += 5;
    if (bits >= 8) { out.push((val >>> (bits - 8)) & 255); bits -= 8; }
  }
  return Buffer.from(out);
}
// Live TOTP code minted on demand — the seed never leaves the machine.
const totpNow = (): string => {
  const seed = file("E2E_TOTP_SEED", "/.config/veil/kratos-totp");
  const ctr = Math.floor(Date.now() / 30000);
  const buf = Buffer.alloc(8); buf.writeBigUInt64BE(BigInt(ctr));
  const h = createHmac("sha1", b32decode(seed)).update(buf).digest();
  const o = h[h.length - 1] & 15;
  return String(((h[o] & 127) << 24 | h[o + 1] << 16 | h[o + 2] << 8 | h[o + 3]) % 1e6).padStart(6, "0");
};

// Agent steps run through Devin SWE-2 Medium (local `devin --print`),
// no model API key needed. Secrets come from env — never committed:
//   E2E_SECRET_VAULT_PASSWORD, E2E_TOTP_SEED
export default {
  tests: ["e2e/tests/**/*.e2e.ts"],
  targets: [
    {
      name: "web",
      engine: web(),
      app: { url: "https://app.veil.nyc" },
    },
    {
      name: "ios",
      engine: mobile({ platform: "ios" }),
      app: { bundleId: "nyc.veil.phone" },
    },
  ],
  secrets: {
    "vault-password": file("E2E_SECRET_VAULT_PASSWORD", "/.config/veil/kratos-password"),
    "totp-code": totpNow,
  },
  agents: {
    default: {
      executor: createDevinExecutor({ maxTurns: 8 }),
    },
  },
} satisfies E2EConfig;
