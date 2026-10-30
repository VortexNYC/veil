#!/usr/bin/env node
/** Create a live device-link only; prints token to stdout (no secrets). */
import nacl from "tweetnacl";

const origin = (process.env.VEIL_ORIGIN ?? "https://veil.nyc").replace(/\/$/, "");
const newKP = nacl.box.keyPair();

const res = await fetch(`${origin}/v1/device-links`, {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ label: "sim-claim-ui", pubkey: [...newKP.publicKey] }),
});
const text = await res.text();
let json;
try {
  json = JSON.parse(text);
} catch {
  console.error("create failed", res.status, text.slice(0, 200));
  process.exit(1);
}
if (res.status !== 200 || !json?.token) {
  console.error("create failed", res.status, text.slice(0, 200));
  process.exit(1);
}
console.log(json.token);
