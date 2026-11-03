/**
 * Set GitHub repo secret RAILWAY_API_TOKEN for CI.
 *
 * Priority:
 * 1. $RAILWAY_API_TOKEN (already exported)
 * 2. Veil item reveal (default `railway-alchemy-ci`; override VEIL_RAILWAY_CI_ITEM)
 * 3. Railway account token via `railway api` mutation (name: github-veil-alchemy-ci)
 * 4. ~/.alchemy/profiles/default/railway.json (oauth/stored token)
 *
 * Veil path needs VEIL_ORIGIN (default https://veil.nyc) and a live human JWT
 * (VEIL_HUMAN_TOKEN_FILE + remint files, same as `veil item add`).
 *
 * Usage: node scripts/set-github-railway-secret.mjs [--repo VortexNYC/veil]
 */
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const repoArg = process.argv.indexOf("--repo");
const repo = repoArg >= 0 ? process.argv[repoArg + 1] : "VortexNYC/veil";

const veilItem =
  process.env.VEIL_RAILWAY_CI_ITEM?.trim() || "railway-alchemy-ci";

/** @returns {string} */
function tokenFromVeil(itemName) {
  const env = {
    ...process.env,
    VEIL_ORIGIN: process.env.VEIL_ORIGIN?.trim() || "https://veil.nyc",
  };
  const reveal = spawnSync("veil", ["item", "reveal", itemName], {
    encoding: "utf8",
    env,
  });
  if (reveal.status !== 0) {
    return "";
  }
  const match = reveal.stdout.match(/^password:\s*(.+)$/m);
  const value = match?.[1]?.trim();
  return typeof value === "string" && value.length > 0 ? value : "";
}

const fromEnv = process.env.RAILWAY_API_TOKEN?.trim();
let token = fromEnv && fromEnv.length > 0 ? fromEnv : "";

if (token.length === 0) {
  token = tokenFromVeil(veilItem);
}

if (token.length === 0) {
  const created = spawnSync(
    "railway",
    [
      "api",
      "mutation($input: ApiTokenCreateInput!) { apiTokenCreate(input: $input) }",
      "--variables",
      JSON.stringify({ input: { name: "github-veil-alchemy-ci" } }),
    ],
    { encoding: "utf8" },
  );
  if (created.status === 0) {
    try {
      const body = JSON.parse(created.stdout);
      const value = body?.data?.apiTokenCreate;
      if (typeof value === "string" && value.length > 0) token = value;
    } catch {
      /* try profile fallback */
    }
  }
}

if (token.length === 0) {
  const profilePath = join(
    process.env.HOME ?? "",
    ".alchemy/profiles/default/railway.json",
  );
  try {
    const doc = JSON.parse(readFileSync(profilePath, "utf8"));
    const value = doc.values?.token;
    if (typeof value === "string" && value.length > 0) token = value;
  } catch {
    /* missing profile */
  }
}

if (token.length === 0) {
  console.error(
    `No Railway token. Export RAILWAY_API_TOKEN, store item ${veilItem} in Veil, create one at https://railway.com/account/tokens, or run pnpm login:railway.`,
  );
  process.exit(1);
}

const set = spawnSync("gh", ["secret", "set", "RAILWAY_API_TOKEN", "-R", repo, "--body", token], {
  encoding: "utf8",
});
if (set.status !== 0) {
  process.stderr.write(set.stderr || set.stdout || "gh secret set failed\n");
  process.exit(set.status ?? 1);
}

console.log(`Set RAILWAY_API_TOKEN on ${repo} (value not printed).`);
