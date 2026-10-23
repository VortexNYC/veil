import { createClient, type Config } from "./client";

export type AmbientOidcOptions = {
  request?: { headers?: { get(name: string): string | null } };
  audience?: string;
};

type NodeProcess = {
  env?: Record<string, string | undefined>;
  getBuiltinModule?: (
    id: string,
  ) => { readFileSync?: (path: string, enc: string) => string } | undefined;
};

function getProc(): NodeProcess | undefined {
  const g = globalThis as { process?: NodeProcess };
  return g.process;
}

function readTokenFile(path: string): string {
  const fs = getProc()?.getBuiltinModule?.("node:fs");
  if (!fs?.readFileSync) {
    throw new Error("ambientOidcToken: file reading requires a Node.js runtime");
  }
  const value = fs.readFileSync(path, "utf8").trim();
  if (!value) {
    throw new Error(`ambientOidcToken: empty token file ${path}`);
  }
  return value;
}

/**
 * Resolves an ambient workload OIDC token in priority order:
 * 1. Request `x-vercel-oidc-token` header (Vercel Functions)
 * 2. `VERCEL_OIDC_TOKEN_FILE` (strict read on every call, no fallback if unreadable/empty)
 * 3. `VERCEL_OIDC_TOKEN` (`vercel env pull` / Vercel builds)
 * 4. GitHub Actions OIDC (`ACTIONS_ID_TOKEN_REQUEST_URL` + `ACTIONS_ID_TOKEN_REQUEST_TOKEN`)
 * 5. `VEIL_OIDC_TOKEN_FILE`
 * 6. `VEIL_OIDC_TOKEN`
 */
export async function ambientOidcToken(opts?: AmbientOidcOptions): Promise<string | undefined> {
  const headerToken = opts?.request?.headers?.get("x-vercel-oidc-token")?.trim();
  if (headerToken) {
    return headerToken;
  }
  const env = getProc()?.env ?? {};
  const vercelFile = env.VERCEL_OIDC_TOKEN_FILE?.trim();
  if (vercelFile) {
    return readTokenFile(vercelFile);
  }
  const vercelToken = env.VERCEL_OIDC_TOKEN?.trim();
  if (vercelToken) {
    return vercelToken;
  }
  const actionsUrl = env.ACTIONS_ID_TOKEN_REQUEST_URL?.trim();
  const actionsBearer = env.ACTIONS_ID_TOKEN_REQUEST_TOKEN?.trim();
  if (actionsUrl && actionsBearer) {
    const aud = encodeURIComponent(opts?.audience ?? "veil");
    const sep = actionsUrl.includes("?") ? "&" : "?";
    const res = await fetch(`${actionsUrl}${sep}audience=${aud}`, {
      headers: { Authorization: `bearer ${actionsBearer}` },
    });
    if (!res.ok) {
      throw new Error(`ambientOidcToken: GitHub Actions OIDC request failed (${res.status})`);
    }
    const data = (await res.json()) as { value?: string };
    const tok = data.value?.trim();
    if (!tok) {
      throw new Error("ambientOidcToken: empty GitHub Actions OIDC token");
    }
    return tok;
  }
  const veilFile = env.VEIL_OIDC_TOKEN_FILE?.trim();
  if (veilFile) {
    return readTokenFile(veilFile);
  }
  const veilToken = env.VEIL_OIDC_TOKEN?.trim();
  if (veilToken) {
    return veilToken;
  }
  return undefined;
}

export function createAmbientClient(config?: Config & AmbientOidcOptions) {
  return createClient({
    baseUrl: "https://veil.nyc",
    ...config,
    auth: async () => (await ambientOidcToken(config)) ?? "",
  });
}
