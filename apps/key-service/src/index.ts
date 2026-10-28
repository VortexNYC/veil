// veil-key-service — Cloudflare-native KEK envelope service.
// The KEK lives only in the Secrets Store binding; this Worker is the
// only code path that touches it. Origin calls seal/open and never sees
// the key material. Auth is a shared SERVICE_TOKEN also in the store.

interface Env {
  VEIL_KEK: { get(): Promise<string> };
  SERVICE_TOKEN: { get(): Promise<string> };
}

const b64 = (b: ArrayBuffer | Uint8Array): string => {
  const u = b instanceof Uint8Array ? b : new Uint8Array(b);
  let s = "";
  for (const x of u) s += String.fromCharCode(x);
  return btoa(s);
};
const unb64 = (s: string): Uint8Array => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
const unhex = (s: string): Uint8Array => {
  const out = new Uint8Array(s.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return out;
};

async function kek(env: Env): Promise<CryptoKey> {
  const raw = unhex((await env.VEIL_KEK.get()).trim());
  if (raw.length !== 32) throw new Error("bad kek length");
  return crypto.subtle.importKey("raw", raw, "AES-GCM", false, ["encrypt", "decrypt"]);
}

async function authorized(req: Request, env: Env): Promise<boolean> {
  const want = (await env.SERVICE_TOKEN.get()).trim();
  const got = req.headers.get("authorization") ?? "";
  return got === `Bearer ${want}`;
}

const json = (o: unknown, status = 200) =>
  new Response(JSON.stringify(o), { status, headers: { "content-type": "application/json" } });

export default {
  async fetch(req: Request, env: Env): Promise<Response> {
    const url = new URL(req.url);
    if (url.pathname === "/health") return json({ ok: true, service: "veil-key-service" });
    if (!(await authorized(req, env))) return json({ error: "unauthorized" }, 401);
    try {
      if (req.method === "POST" && url.pathname === "/seal") {
        const { plaintext_b64 } = (await req.json()) as { plaintext_b64?: string };
        if (!plaintext_b64) return json({ error: "plaintext_b64 required" }, 400);
        const iv = crypto.getRandomValues(new Uint8Array(12));
        const ct = await crypto.subtle.encrypt(
          { name: "AES-GCM", iv }, await kek(env), unb64(plaintext_b64));
        const out = new Uint8Array(12 + ct.byteLength);
        out.set(iv); out.set(new Uint8Array(ct), 12);
        return json({ ciphertext_b64: b64(out) });
      }
      if (req.method === "POST" && url.pathname === "/open") {
        const { ciphertext_b64 } = (await req.json()) as { ciphertext_b64?: string };
        if (!ciphertext_b64) return json({ error: "ciphertext_b64 required" }, 400);
        const blob = unb64(ciphertext_b64);
        if (blob.length < 29) return json({ error: "ciphertext too short" }, 400);
        const pt = await crypto.subtle.decrypt(
          { name: "AES-GCM", iv: blob.slice(0, 12) }, await kek(env), blob.slice(12));
        return json({ plaintext_b64: b64(pt) });
      }
      return json({ error: "not found" }, 404);
    } catch (e) {
      return json({ error: e instanceof Error ? e.message : "internal" }, 500);
    }
  },
};
