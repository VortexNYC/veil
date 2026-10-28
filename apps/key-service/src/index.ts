// veil-key-service — Cloudflare-native KEK envelope service.
// The KEK lives only in the Secrets Store binding; this Worker is the
// only code path that touches it. Origin calls seal/open and never sees
// the key material. Auth is a shared SERVICE_TOKEN also in the store.
//
// Envelope: 0x02 || iv(12) || AES-256-GCM ct — the version byte lets
// callers distinguish service-wrapped blobs from legacy local-AEAD rows.
// AAD is caller-supplied (orgID binding); the service is a seal/open
// oracle for anyone holding SERVICE_TOKEN — that is the design.
// Rotation: VEIL_KEK_NEXT bound during a rotation window; /rewrap opens
// under either key and seals under NEXT — ciphertext only on the wire.

interface Env {
  VEIL_KEK: { get(): Promise<string> };
  VEIL_KEK_NEXT?: { get(): Promise<string> };
  SERVICE_TOKEN: { get(): Promise<string> };
}

const VERSION = 0x02;
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

async function keys(env: Env): Promise<CryptoKey[]> {
  const out: CryptoKey[] = [];
  const cur = unhex((await env.VEIL_KEK.get()).trim());
  if (cur.length !== 32) throw new Error("bad kek length");
  out.push(await crypto.subtle.importKey("raw", cur, "AES-GCM", false, ["encrypt", "decrypt"]));
  if (env.VEIL_KEK_NEXT) {
    try {
      const nxt = unhex((await env.VEIL_KEK_NEXT.get()).trim());
      if (nxt.length === 32)
        out.push(await crypto.subtle.importKey("raw", nxt, "AES-GCM", false, ["encrypt", "decrypt"]));
    } catch { /* NEXT unset or malformed — single-key mode */ }
  }
  return out;
}

const sealTo = (key: CryptoKey, pt: Uint8Array, aad: Uint8Array): Promise<ArrayBuffer> =>
  crypto.subtle.encrypt({ name: "AES-GCM", iv: crypto.getRandomValues(new Uint8Array(12)), additionalData: aad }, key, pt);

async function openAny(ks: CryptoKey[], blob: Uint8Array, aad: Uint8Array): Promise<ArrayBuffer> {
  for (const k of ks) {
    try {
      return await crypto.subtle.decrypt({ name: "AES-GCM", iv: blob.slice(0, 12), additionalData: aad }, k, blob.slice(12));
    } catch { /* wrong key — try next */ }
  }
  throw new Error("no key opens this blob");
}

const pack = (iv_ct: { iv: Uint8Array; ct: ArrayBuffer }): Uint8Array => {
  const out = new Uint8Array(1 + 12 + iv_ct.ct.byteLength);
  out[0] = VERSION; out.set(iv_ct.iv, 1); out.set(new Uint8Array(iv_ct.ct), 13);
  return out;
};

const json = (o: unknown, status = 200) =>
  new Response(JSON.stringify(o), { status, headers: { "content-type": "application/json" } });

export default {
  async fetch(req: Request, env: Env): Promise<Response> {
    const url = new URL(req.url);
    if (url.pathname === "/health") return json({ ok: true, service: "veil-key-service" });
    if ((req.headers.get("authorization") ?? "") !== `Bearer ${(await env.SERVICE_TOKEN.get()).trim()}`)
      return json({ error: "unauthorized" }, 401);
    try {
      const ks = await keys(env);
      if (req.method === "POST" && url.pathname === "/seal") {
        const { plaintext_b64, aad_b64 } = (await req.json()) as { plaintext_b64?: string; aad_b64?: string };
        if (!plaintext_b64) return json({ error: "plaintext_b64 required" }, 400);
        const aad = aad_b64 ? unb64(aad_b64) : new Uint8Array(0);
        const iv = crypto.getRandomValues(new Uint8Array(12));
        const ct = await crypto.subtle.encrypt({ name: "AES-GCM", iv, additionalData: aad }, ks[0], unb64(plaintext_b64));
        return json({ ciphertext_b64: b64(pack({ iv, ct })) });
      }
      if (req.method === "POST" && url.pathname === "/open") {
        const { ciphertext_b64, aad_b64 } = (await req.json()) as { ciphertext_b64?: string; aad_b64?: string };
        if (!ciphertext_b64) return json({ error: "ciphertext_b64 required" }, 400);
        const blob = unb64(ciphertext_b64);
        if (blob[0] !== VERSION || blob.length < 30) return json({ error: "bad envelope" }, 400);
        const aad = aad_b64 ? unb64(aad_b64) : new Uint8Array(0);
        const pt = await openAny(ks, blob.slice(1), aad);
        return json({ plaintext_b64: b64(pt) });
      }
      if (req.method === "POST" && url.pathname === "/rewrap") {
        // Rotation: open each blob under any bound key, reseal under NEXT.
        // Ciphertext only on the wire — masters never leave the service.
        if (ks.length < 2) return json({ error: "no VEIL_KEK_NEXT bound — not in a rotation window" }, 400);
        const { items } = (await req.json()) as { items?: { aad_b64?: string; ciphertext_b64?: string }[] };
        if (!items?.length || items.length > 1000) return json({ error: "items required (<=1000)" }, 400);
        const out: (string | null)[] = [];
        for (const it of items) {
          if (!it.ciphertext_b64) { out.push(null); continue; }
          const blob = unb64(it.ciphertext_b64);
          const aad = it.aad_b64 ? unb64(it.aad_b64) : new Uint8Array(0);
          if (blob[0] !== VERSION || blob.length < 30) { out.push(null); continue; }
          try {
            const pt = await openAny(ks, blob.slice(1), aad);
            const iv = crypto.getRandomValues(new Uint8Array(12));
            const ct = await crypto.subtle.encrypt({ name: "AES-GCM", iv, additionalData: aad }, ks[1], pt);
            out.push(b64(pack({ iv, ct })));
          } catch { out.push(null); }
        }
        return json({ ciphertexts_b64: out });
      }
      return json({ error: "not found" }, 404);
    } catch (e) {
      return json({ error: e instanceof Error ? e.message : "internal" }, 500);
    }
  },
};
