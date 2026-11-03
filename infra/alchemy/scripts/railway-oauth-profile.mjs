/**
 * One-shot Railway OAuth → Alchemy profile (method: oauth).
 * Opens the pairing URL in the default browser, polls up to 5m, then
 * writes ~/.alchemy via `alchemy profile edit`.
 */
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import {
  cancelLoginSession,
  createLoginSession,
  loginSessionUrl,
  pollLoginSessionToken,
  provideAnonymousRailway,
} from "@alchemy.run/railway/LoginSession";
import * as Effect from "effect/Effect";
import * as Redacted from "effect/Redacted";

const root = fileURLToPath(new URL("..", import.meta.url));

const runOAuth = Effect.gen(function* () {
  const code = yield* provideAnonymousRailway(createLoginSession());
  const url = loginSessionUrl(code, { hostname: "veil" });
  process.stderr.write(`\nRailway pairing — confirm in the browser:\n${url}\n\n`);
  spawnSync("open", [url], { stdio: "ignore" });

  const token = yield* provideAnonymousRailway(pollLoginSessionToken(code));
  if (token == null) {
    yield* provideAnonymousRailway(cancelLoginSession(code));
    throw new Error("Railway login timed out after 5 minutes");
  }
  return Redacted.value(token);
});

const token = await Effect.runPromise(runOAuth);

const child = spawnSync(
  "pnpm",
  [
    "exec",
    "alchemy",
    "profile",
    "edit",
    "-c",
    `${root}/alchemy.run.ts`,
    "--add",
    "Railway",
    "--method",
    "stored",
    "--set",
    "token=-",
  ],
  {
    cwd: root,
    input: token,
    encoding: "utf8",
    stdio: ["pipe", "pipe", "pipe"],
  },
);

if (child.status !== 0) {
  process.stderr.write(child.stderr || child.stdout || "profile edit failed\n");
  process.exit(child.status ?? 1);
}

const profilesPath = `${process.env.HOME}/.alchemy/profiles/default/railway.json`;
try {
  const fs = await import("node:fs/promises");
  const raw = await fs.readFile(profilesPath, "utf8");
  const doc = JSON.parse(raw);
  if (doc.values?.method === "stored") {
    doc.values.method = "oauth";
    await fs.writeFile(profilesPath, `${JSON.stringify(doc, null, 2)}\n`, { mode: 0o600 });
  }
} catch {
  // Provider file layout may differ; stored credentials still work.
}

process.stdout.write("Railway OAuth saved to Alchemy profile `default`.\n");
