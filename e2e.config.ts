import type { E2EConfig } from "e2e";
import { web } from "@e2e-dev/web";
import { mobile } from "@e2e-dev/mobile";
import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { createDevinExecutor } from "./e2e/devin-executor.ts";

const file = (env: string, path: string) =>
  process.env[env] ?? readFileSync(`${homedir()}${path}`, "utf8").trim();

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
    "totp-code": file("E2E_SECRET_TOTP_CODE", "/.config/veil/kratos-totp"),
  },
  agents: {
    default: {
      executor: createDevinExecutor({ maxTurns: 8 }),
    },
  },
} satisfies E2EConfig;
