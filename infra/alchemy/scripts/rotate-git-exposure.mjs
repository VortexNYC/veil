#!/usr/bin/env node
/**
 * Rotate operational secrets after git history exposure (pre-scrub Alchemy state).
 * Never prints secret values. Stops on first hard failure.
 *
 *   pnpm run rotate:git-exposure              # all automated phases
 *   ROTATE_PHASES=mail,offsite pnpm run ...   # subset
 */
import { randomBytes } from "node:crypto";
import { spawnSync } from "node:child_process";
import { chmodSync, readFileSync, unlinkSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(fileURLToPath(new URL(".", import.meta.url)), "..");
const repoRoot = join(root, "../..");

const phases = (process.env.ROTATE_PHASES?.trim() || "mail,offsite,postgres,identity,github")
  .split(",")
  .map((s) => s.trim())
  .filter(Boolean);

const escrowPath =
  process.env.VEIL_ENV_ESCROW_PATH?.trim() ||
  join(homedir(), ".config/vortex/veil-env-escrow.json");

function token(bytes = 32) {
  return randomBytes(bytes).toString("hex");
}

function run(cmd, args, opts = {}) {
  const r = spawnSync(cmd, args, {
    encoding: "utf8",
    stdio: ["pipe", "pipe", "pipe"],
    ...opts,
  });
  if (r.status !== 0) {
    process.stderr.write(r.stderr || r.stdout || `${cmd} failed\n`);
    process.exit(r.status ?? 1);
  }
  return r;
}

function railwayVar(service, key) {
  const r = spawnSync(
    "railway",
    ["variable", "list", "--service", service, "--json"],
    { cwd: repoRoot, encoding: "utf8" },
  );
  if (r.status !== 0) return "";
  try {
    const v = JSON.parse(r.stdout)[key];
    return typeof v === "string" ? v : "";
  } catch {
    return "";
  }
}

function setRailwayVar(service, key, value) {
  run("railway", ["variable", "set", key, "--service", service, "--stdin"], {
    cwd: repoRoot,
    input: value,
  });
}

function wranglerSecret(appDir, name, value) {
  run("pnpm", ["exec", "wrangler", "secret", "put", name], {
    cwd: join(repoRoot, appDir),
    input: value,
  });
}

function readEscrow() {
  try {
    return JSON.parse(readFileSync(escrowPath, "utf8"));
  } catch {
    process.stderr.write(`cannot read escrow at ${escrowPath}\n`);
    process.exit(1);
  }
}

function patchEscrow(mutator) {
  const escrow = readEscrow();
  mutator(escrow);
  writeFileSync(escrowPath, `${JSON.stringify(escrow, null, 2)}\n`, { mode: 0o600 });
  try {
    chmodSync(escrowPath, 0o600);
  } catch {
    // ok
  }
}

function offsiteFromEscrow(escrow) {
  return (
    escrow["veil-backup"]?.OFFSITE_TOKEN?.trim() ||
    escrow["veil-wal"]?.OFFSITE_TOKEN?.trim() ||
    escrow["veil-audit-export"]?.VEIL_AUDIT_INGEST_TOKEN?.trim() ||
    ""
  );
}

function ghSecret(name, value, repo = "VortexNYC/veil") {
  run("gh", ["secret", "set", name, "-R", repo, "--body", value]);
}

function veilItemRotateSecret(itemName, secretValue) {
  const tmp = join(tmpdir(), `veil-rotate-${randomBytes(8).toString("hex")}`);
  writeFileSync(tmp, `${secretValue}\n`, { mode: 0o600 });
  try {
    const r = spawnSync(
      "veil",
      ["rotate", itemName, "--secret-file", tmp],
      {
        encoding: "utf8",
        env: {
          ...process.env,
          VEIL_ORIGIN: process.env.VEIL_ORIGIN?.trim() || "https://veil.nyc",
        },
      },
    );
    if (r.status !== 0) {
      process.stderr.write(
        r.stderr || `veil item rotate ${itemName} skipped (owner session?)\n`,
      );
    }
  } finally {
    try {
      unlinkSync(tmp);
    } catch {
      // ok
    }
  }
}

function redeploy(services) {
  for (const s of services) {
    process.stderr.write(`redeploy ${s}\n`);
    const r = spawnSync("railway", ["redeploy", "--service", s, "-y"], {
      cwd: repoRoot,
      encoding: "utf8",
    });
    if (r.status !== 0) {
      process.stderr.write(
        r.stderr ||
          r.stdout ||
          `redeploy ${s} skipped (already deploying or unavailable)\n`,
      );
    }
  }
}

if (phases.includes("mail")) {
  process.stderr.write("\n=== mail (Cloudflare worker + veil + kratos courier) ===\n");
  const mail = token(32);
  wranglerSecret("apps/mail", "MAIL_TOKEN", mail);
  setRailwayVar("veil", "VEIL_MAIL_TOKEN", mail);
  setRailwayVar("kratos", "COURIER_HTTP_REQUEST_CONFIG_AUTH_CONFIG_VALUE", `Bearer ${mail}`);
  patchEscrow((e) => {
    if (!e.veil) e.veil = {};
    if (!e.kratos) e.kratos = {};
    e.veil.VEIL_MAIL_TOKEN = mail;
    e.kratos.COURIER_HTTP_REQUEST_CONFIG_AUTH_CONFIG_VALUE = `Bearer ${mail}`;
  });
  redeploy(["veil", "kratos"]);
}

if (phases.includes("offsite")) {
  process.stderr.write("\n=== offsite + audit ingest ===\n");
  const offsite = token(32);
  wranglerSecret("apps/backup-ingest", "OFFSITE_TOKEN", offsite);
  for (const svc of ["veil-backup", "veil-wal", "veil-wal-dr"]) {
    setRailwayVar(svc, "OFFSITE_TOKEN", offsite);
  }
  setRailwayVar("veil-audit-export", "VEIL_AUDIT_INGEST_TOKEN", offsite);
  patchEscrow((e) => {
    for (const svc of ["veil-backup", "veil-wal", "veil-wal-dr"]) {
      if (!e[svc]) e[svc] = {};
      e[svc].OFFSITE_TOKEN = offsite;
    }
    if (!e["veil-audit-export"]) e["veil-audit-export"] = {};
    e["veil-audit-export"].VEIL_AUDIT_INGEST_TOKEN = offsite;
  });
  redeploy(["veil-backup", "veil-wal", "veil-wal-dr", "veil-audit-export"]);
  const backupItem =
    process.env.VEIL_OFFSITE_ITEM?.trim() || "Veil backup-ingest token";
  veilItemRotateSecret(backupItem, offsite);
}

if (phases.includes("postgres")) {
  process.stderr.write("\n=== Postgres password ===\n");
  const current = railwayVar("Postgres", "PGPASSWORD") || railwayVar("Postgres", "POSTGRES_PASSWORD");
  if (!current) {
    process.stderr.write("cannot read current Postgres password from Railway\n");
    process.exit(1);
  }
  const next = token(24);
  const uriCurrent = `postgresql://postgres:${encodeURIComponent(current)}@postgres.railway.internal:5432/postgres`;
  const alter = spawnSync(
    "railway",
    [
      "ssh",
      "--service",
      "veil-wal",
      "--",
      "psql",
      uriCurrent,
      "-v",
      "ON_ERROR_STOP=1",
      "-c",
      `ALTER USER postgres WITH PASSWORD '${next}';`,
    ],
    { cwd: repoRoot, encoding: "utf8" },
  );
  if (alter.status !== 0) {
    process.stderr.write(alter.stderr || alter.stdout || "ALTER USER failed\n");
    process.exit(alter.status ?? 1);
  }
  setRailwayVar("Postgres", "POSTGRES_PASSWORD", next);
  setRailwayVar("Postgres", "PGPASSWORD", next);

  const dsn = (db) =>
    `postgresql://postgres:${next}@postgres.railway.internal:5432/${db}?sslmode=require`;
  setRailwayVar("kratos", "DSN", dsn("kratos"));
  setRailwayVar("keto", "DSN", dsn("keto"));
  setRailwayVar("hydra", "DSN", dsn("railway"));
  patchEscrow((e) => {
    e.kratos = { ...(e.kratos || {}), DSN: dsn("kratos") };
    e.keto = { ...(e.keto || {}), DSN: dsn("keto") };
    e.hydra = { ...(e.hydra || {}), DSN: dsn("railway") };
    e.Postgres = { ...(e.Postgres || {}), PGPASSWORD: next, POSTGRES_PASSWORD: next };
  });
  redeploy([
    "Postgres",
    "kratos",
    "keto",
    "hydra",
    "glue",
    "veil",
    "veil-migrate",
    "veil-sweep",
    "veil-monitor",
    "veil-audit-export",
    "veil-backup",
    "veil-wal",
    "veil-wal-dr",
  ]);
}

if (phases.includes("identity")) {
  process.stderr.write("\n=== identity-plane random secrets (kratos + glue; Hydra SECRETS_SYSTEM skipped — prepend-only, see runbook) ===\n");
  const cookie = token(16);
  const cipher = token(16);
  const gluePw = token(16);
  setRailwayVar("kratos", "SECRETS_COOKIE", cookie);
  setRailwayVar("kratos", "SECRETS_CIPHER", cipher);
  setRailwayVar("glue", "BOOTSTRAP_PASSWORD", gluePw);
  patchEscrow((e) => {
    e.kratos = { ...(e.kratos || {}), SECRETS_COOKIE: cookie, SECRETS_CIPHER: cipher };
    e.glue = { ...(e.glue || {}), BOOTSTRAP_PASSWORD: gluePw };
  });
  redeploy(["kratos", "glue"]);
}

if (phases.includes("github")) {
  process.stderr.write("\n=== GitHub Actions secrets (offsite + keysvc if Veil reachable) ===\n");
  const repo = process.env.GITHUB_REPOSITORY?.trim() || "VortexNYC/veil";
  const offsiteTok = offsiteFromEscrow(readEscrow());
  if (offsiteTok) {
    ghSecret("VEIL_OFFSITE_TOKEN", offsiteTok, repo);
    ghSecret("VEIL_AUDIT_INGEST_TOKEN", offsiteTok, repo);
    process.stderr.write(`Set VEIL_OFFSITE_TOKEN and VEIL_AUDIT_INGEST_TOKEN on ${repo} (from escrow).\n`);
  } else {
    run("node", ["scripts/set-github-offsite-secret.mjs"], { cwd: root });
    run("node", ["scripts/set-github-audit-ingest-secret.mjs"], { cwd: root });
  }
  const keysvc = spawnSync("node", ["scripts/set-github-keysvc-secret.mjs"], {
    cwd: root,
    encoding: "utf8",
  });
  if (keysvc.status !== 0) {
    const tok = railwayVar("veil", "VEIL_KEYSVC_TOKEN");
    if (tok) {
      ghSecret("VEIL_KEYSVC_TOKEN", tok, repo);
      process.stderr.write(`Set VEIL_KEYSVC_TOKEN on ${repo} (from Railway veil).\n`);
    } else {
      process.stderr.write("keysvc GitHub secret skipped — set manually\n");
    }
  }
}

if (phases.includes("kek")) {
  process.stderr.write("\n=== VEIL_KEK (veil key rotate-kek) ===\n");
  process.stderr.write("Run manually with owner session:\n");
  process.stderr.write("  veil key rotate-kek --generate --shares 3 --threshold 2 --shares-dir ~/.config/vortex/kek-shares\n");
  process.stderr.write("See docs/key-rotation.md\n");
}

process.stderr.write(
  "\nManual still required if exposed in git: APNS (.p8), FCM service account, Stripe whsec, Vortex billing API keys, Hydra OIDC pairwise salt (prepend-only for SECRETS_SYSTEM list if grants exist).\n",
);
process.stderr.write("Smoke: curl -fsS https://veil.nyc/ready && veil monitor (once).\n");
