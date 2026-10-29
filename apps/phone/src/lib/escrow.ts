// iPhone-native KEK escrow custodian — VEILSHR1 parcels sealed with NaCl
// SealAnonymous (X25519). Same envelope as `veil key escrow-seal/open` in Go:
// VEILSHR1 || ordinal(1) || ceremony-fp(4) || share(33).
// Custodian private key lives only in expo-secure-store on this device.

import * as Crypto from "expo-crypto";
import * as SecureStore from "expo-secure-store";
import nacl from "tweetnacl";

nacl.setPRNG((x, n) => {
  const rnd = Crypto.getRandomBytes(n);
  x.set(rnd);
});

const MAGIC = new Uint8Array([0x56, 0x45, 0x49, 0x4c, 0x53, 0x48, 0x52, 0x31]); // "VEILSHR1"
const KEY_PRIV = "escrow.custodian.key";
const KEY_PUB = "escrow.custodian.pub";

export function hex(b: Uint8Array): string {
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}
export function unhex(s: string): Uint8Array {
  const out = new Uint8Array(s.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return out;
}
function b64decode(s: string): Uint8Array {
  const bin = globalThis.atob(s.trim());
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export async function custodianPubHex(): Promise<string> {
  const existing = await SecureStore.getItemAsync(KEY_PUB);
  if (existing) return existing;
  const kp = nacl.box.keyPair();
  const pub = hex(kp.publicKey);
  await SecureStore.setItemAsync(KEY_PRIV, hex(kp.secretKey));
  await SecureStore.setItemAsync(KEY_PUB, pub);
  return pub;
}

export interface OpenedShare { ordinal: number; fp: string; shareHex: string }

export async function openParcel(parcelB64: string): Promise<OpenedShare> {
  const privHex = await SecureStore.getItemAsync(KEY_PRIV);
  if (!privHex) throw new Error("no custodian key on this device — generate one first");
  const blob = b64decode(parcelB64);
  if (blob.length < 56 + 13) throw new Error("parcel too short");
  // Plain NaCl box: eph(32) || nonce(24) || ct — see escrow-seal --format phone.
  const eph = blob.slice(0, 32), nonce = blob.slice(32, 56), ct = blob.slice(56);
  const env = nacl.box.open(ct, nonce, eph, unhex(privHex));
  if (!env) throw new Error("parcel does not open with this device's custodian key");
  if (env.length < 13 || !MAGIC.every((b, i) => env[i] === b))
    throw new Error("parcel is not a VEILSHR1 envelope");
  const ordinal = env[8];
  const fp = hex(env.slice(9, 13));
  const share = env.slice(13);
  if (share.length !== 33) throw new Error(`share is ${share.length}B, want 33`);
  return { ordinal, fp, shareHex: hex(share) };
}

// registerThisDevice — one-shot post-sign-in: stable device id + the
// custodian pubkey go to the linked-devices inventory.
export async function registerThisDevice(label: string): Promise<void> {
  const { registerLinkedDevice } = await import("./api");
  let id = await SecureStore.getItemAsync("veil.device.id");
  if (!id) {
    id = `ios-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
    await SecureStore.setItemAsync("veil.device.id", id);
  }
  const pubHex = await custodianPubHex();
  await registerLinkedDevice(id, label, pubHex);
}
