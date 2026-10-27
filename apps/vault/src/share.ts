import nacl from "tweetnacl";

// VTX-783 share links — the browser mirror of internal/share (Go).
//
//   key  = 32 random bytes           → URL fragment (#k=), base64url
//   pwk  = PBKDF2-SHA-256("veil-share-pw\0"+pass, salt, 600k)   when passphrase set
//   keff = key XOR pwk             (key alone when no passphrase)
//   blob = nonce(24) || secretbox(plaintext, nonce, keff)
//   meta = {v:1, alg:"nacl.secretbox", pw:true, kdf:"pbkdf2-sha256", pw_salt:b64, pw_iter:600000}
//
// The origin stores only the blob. The key never leaves the fragment —
// fragments are not sent in HTTP requests, Referer, or server logs.

export const SHARE_VERSION = 1;
const SHARE_ALG = "nacl.secretbox";
const SHARE_KDF = "pbkdf2-sha256";
const SHARE_PW_ITER = 600000;

export interface ShareMeta {
  v: number;
  alg: string;
  pw?: boolean;
  kdf?: string;
  pw_salt?: string;
  pw_iter?: number;
  [key: string]: unknown;
}

function b64(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) {
    s += String.fromCharCode(b);
  }
  return btoa(s);
}

function b64urlEncode(bytes: Uint8Array): string {
  return b64(bytes).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}

function b64decode(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) {
    out[i] = bin.charCodeAt(i);
  }
  return out;
}

function b64urlDecode(s: string): Uint8Array {
  const pad = s.length % 4 === 0 ? "" : "=".repeat(4 - (s.length % 4));
  return b64decode(s.replaceAll("-", "+").replaceAll("_", "/") + pad);
}

// passKey stretches the passphrase — PBKDF2-SHA-256 at 600k iterations so
// a stolen link still costs real work per guess. Byte-identical to
// internal/share.passKey on the Go side.
async function passKey(pass: string, salt: Uint8Array, iter: number): Promise<Uint8Array> {
  const enc = new TextEncoder();
  const domain = enc.encode("veil-share-pw\0");
  const pw = enc.encode(pass);
  const data = new Uint8Array(domain.length + pw.length);
  data.set(domain);
  data.set(pw, domain.length);
  const base = await crypto.subtle.importKey("raw", data, "PBKDF2", false, ["deriveBits"]);
  const bits = await crypto.subtle.deriveBits(
    { name: "PBKDF2", hash: "SHA-256", salt: salt as BufferSource, iterations: iter },
    base,
    256,
  );
  return new Uint8Array(bits);
}

function mixKey(key: Uint8Array, pwKey: Uint8Array): Uint8Array {
  const out = new Uint8Array(key);
  for (let i = 0; i < out.length; i++) {
    out[i] ^= pwKey[i];
  }
  return out;
}

// sealShare encrypts the pasted plaintext under a fresh link key. Returns
// the blob (standard b64, for POST /v1/shares), the meta params, and the
// base64url key that goes in the link fragment.
export async function sealShare(
  plaintext: string,
  passphrase: string,
): Promise<{ blobB64: string; meta: ShareMeta; key: string }> {
  const key = new Uint8Array(32);
  crypto.getRandomValues(key);
  const meta: ShareMeta = { v: SHARE_VERSION, alg: SHARE_ALG };
  let keff = key;
  if (passphrase) {
    const salt = new Uint8Array(16);
    crypto.getRandomValues(salt);
    keff = mixKey(key, await passKey(passphrase, salt, SHARE_PW_ITER));
    meta.pw = true;
    meta.kdf = SHARE_KDF;
    meta.pw_salt = b64(salt);
    meta.pw_iter = SHARE_PW_ITER;
  }
  const nonce = new Uint8Array(24);
  crypto.getRandomValues(nonce);
  const box = nacl.secretbox(new TextEncoder().encode(plaintext), nonce, keff);
  const blob = new Uint8Array(nonce.length + box.length);
  blob.set(nonce);
  blob.set(box, nonce.length);
  return { blobB64: b64(blob), meta, key: b64urlEncode(key) };
}

// openShare reverses sealShare. Returns null on any mismatch — a wrong
// passphrase and a wrong key are the same failure.
export async function openShare(
  blobB64: string,
  meta: ShareMeta | undefined,
  keyB64: string,
  passphrase: string,
): Promise<string | null> {
  if (meta && (meta.alg !== SHARE_ALG || meta.v !== SHARE_VERSION)) {
    return null;
  }
  let key: Uint8Array;
  try {
    key = b64urlDecode(keyB64);
  } catch {
    return null;
  }
  if (key.length !== 32) {
    return null;
  }
  let keff = key;
  if (meta?.pw) {
    if (!passphrase || meta.kdf !== SHARE_KDF || !meta.pw_salt || !meta.pw_iter) {
      return null;
    }
    if (meta.pw_iter <= 0 || meta.pw_iter > 4_000_000) {
      return null;
    }
    keff = mixKey(key, await passKey(passphrase, b64decode(meta.pw_salt), meta.pw_iter));
  }
  const blob = b64decode(blobB64);
  if (blob.length < 24 + 16) {
    return null;
  }
  const plain = nacl.secretbox.open(blob.subarray(24), blob.subarray(0, 24), keff);
  if (!plain) {
    return null;
  }
  return new TextDecoder().decode(plain);
}
