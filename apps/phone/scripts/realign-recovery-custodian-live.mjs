#!/usr/bin/env node
/** POST /v1/recovery/realign-custodian — re-wrap share to VEIL_CUSTODIAN_PUB_HEX (64 hex). */
import { readFileSync } from "node:fs";

const origin = (process.env.VEIL_ORIGIN ?? "https://veil.nyc").replace(/\/$/, "");
const pubHex = process.env.VEIL_CUSTODIAN_PUB_HEX?.trim().replace(/^0x/i, "");
const humanPath = process.env.VEIL_HUMAN_TOKEN_FILE?.trim() ?? `${process.env.HOME}/.config/veil/human.jwt`;
if (!pubHex || pubHex.length !== 64) {
  console.error("VEIL_CUSTODIAN_PUB_HEX required (32-byte custodian pubkey as 64 hex chars)");
  process.exit(1);
}
const humanJWT = readFileSync(humanPath, "utf8").trim();
const pubkeyB64 = Buffer.from(pubHex, "hex").toString("base64");

const res = await fetch(`${origin}/v1/recovery/realign-custodian`, {
  method: "POST",
  headers: {
    Authorization: `Bearer ${humanJWT}`,
    "Content-Type": "application/json",
  },
  body: JSON.stringify({ pubkey: pubkeyB64 }),
});
const text = await res.text();
if (res.status !== 200) {
  console.error("realign failed", res.status, text.slice(0, 300));
  process.exit(1);
}
console.log("realign-recovery-custodian-live ok");
