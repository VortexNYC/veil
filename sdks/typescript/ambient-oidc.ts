import { createClient, type Config } from "./client";

export type AmbientOidcOptions = {
  request?: { headers?: { get(name: string): string | null } };
  audience?: string;
  /** When true (or a CryptoKeyPair), signs every outbound request with an RFC 9449 ES256 DPoP proof. */
  dpop?: boolean | CryptoKeyPair;
};

function b64url(bytes: Uint8Array): string {
  let bin = "";
  for (const b of bytes) {
    bin += String.fromCharCode(b);
  }
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function b64urlJson(obj: unknown): string {
  return b64url(new TextEncoder().encode(JSON.stringify(obj)));
}

export async function generateDPoPKeyPair(): Promise<CryptoKeyPair> {
  return (await crypto.subtle.generateKey({ name: "ECDSA", namedCurve: "P-256" }, false, [
    "sign",
    "verify",
  ])) as CryptoKeyPair;
}

export async function signDPoPProof(opts: {
  keyPair: CryptoKeyPair;
  method: string;
  url: string;
  accessToken?: string;
}): Promise<string> {
  const pubJwk = await crypto.subtle.exportKey("jwk", opts.keyPair.publicKey);
  const parsed = new URL(opts.url);
  const htu = `${parsed.protocol}//${parsed.host}${parsed.pathname}`;
  const payload: Record<string, unknown> = {
    htm: opts.method.toUpperCase(),
    htu,
    iat: Math.floor(Date.now() / 1000),
    jti: crypto.randomUUID(),
  };
  if (opts.accessToken) {
    const hash = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(opts.accessToken));
    payload.ath = b64url(new Uint8Array(hash));
  }
  const header = {
    typ: "dpop+jwt",
    alg: "ES256",
    jwk: { kty: "EC", crv: "P-256", x: pubJwk.x, y: pubJwk.y },
  };
  const signingInput = `${b64urlJson(header)}.${b64urlJson(payload)}`;
  const sig = await crypto.subtle.sign(
    { name: "ECDSA", hash: "SHA-256" },
    opts.keyPair.privateKey,
    new TextEncoder().encode(signingInput),
  );
  return `${signingInput}.${b64url(new Uint8Array(sig))}`;
}

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
  let keyPairPromise: Promise<CryptoKeyPair> | undefined;
  const resolveKeyPair = (): Promise<CryptoKeyPair> => {
    if (config?.dpop && typeof config.dpop === "object") {
      return Promise.resolve(config.dpop);
    }
    keyPairPromise ??= generateDPoPKeyPair();
    return keyPairPromise;
  };
  const baseFetch = config?.fetch ?? globalThis.fetch;
  const dpopFetch: typeof fetch | undefined = config?.dpop
    ? async (input, init) => {
        const req = new Request(input, init);
        const auth = req.headers.get("Authorization") ?? "";
        const tok = auth.replace(/^(Bearer|DPoP)\s+/i, "").trim();
        const kp = await resolveKeyPair();
        const proof = await signDPoPProof({
          keyPair: kp,
          method: req.method,
          url: req.url,
          accessToken: tok || undefined,
        });
        const headers = new Headers(req.headers);
        if (tok) {
          headers.set("Authorization", `DPoP ${tok}`);
        }
        headers.set("DPoP", proof);
        return baseFetch(new Request(req, { headers }));
      }
    : config?.fetch;
  return createClient({
    baseUrl: "https://veil.nyc",
    ...config,
    ...(dpopFetch ? { fetch: dpopFetch } : {}),
    auth: async () => (await ambientOidcToken(config)) ?? "",
  });
}
