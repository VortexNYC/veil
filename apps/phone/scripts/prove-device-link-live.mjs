#!/usr/bin/env node
/**
 * Live-origin device-link handoff using the same tweetnacl-sealedbox-js stack
 * as apps/phone. Approve uses VEIL_HUMAN_TOKEN_FILE (smoke master, not recovery
 * share — same contract as internal/livetest/device_link_test.go).
 */
import { readFileSync } from "node:fs";
import nacl from "tweetnacl";
import sealedbox from "tweetnacl-sealedbox-js";

const { open: openAnonymous, seal: sealAnonymous } = sealedbox;

const origin = (process.env.VEIL_ORIGIN ?? "https://veil.nyc").replace(/\/$/, "");
const humanPath = process.env.VEIL_HUMAN_TOKEN_FILE?.trim();
if (!humanPath) {
  console.error("VEIL_HUMAN_TOKEN_FILE required");
  process.exit(1);
}
const humanJWT = readFileSync(humanPath, "utf8").trim();

function b64(bytes) {
  return Buffer.from(bytes).toString("base64");
}
function fromB64(s) {
  return new Uint8Array(Buffer.from(s, "base64"));
}

async function post(path, { auth, body }) {
  const headers = { "Content-Type": "application/json" };
  if (auth) headers.Authorization = `Bearer ${auth}`;
  const res = await fetch(`${origin}${path}`, {
    method: "POST",
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  let json = null;
  try {
    json = text ? JSON.parse(text) : null;
  } catch {
    json = text;
  }
  return { status: res.status, json, text };
}

const master = nacl.randomBytes(32);
const newKP = nacl.box.keyPair();
const sealed = sealAnonymous(master, newKP.publicKey);
if (sealed.length !== 80) {
  console.error(`sealed len ${sealed.length} want 80`);
  process.exit(1);
}

const create = await post("/v1/device-links", {
  body: { label: "prove-js", pubkey: [...newKP.publicKey] },
});
if (create.status !== 200 || !create.json?.token) {
  console.error("create failed", create.status, create.text?.slice(0, 200));
  process.exit(1);
}
const token = create.json.token;

const approve = await post(`/v1/device-links/${token}/approve`, {
  auth: humanJWT,
  body: { sealed: [...sealed] },
});
if (approve.status !== 204) {
  console.error("approve failed", approve.status, approve.text?.slice(0, 200));
  process.exit(1);
}

const claim = await post(`/v1/device-links/${token}/claim`, {});
if (claim.status !== 200 || !claim.json?.sealed) {
  console.error("claim failed", claim.status, claim.text?.slice(0, 200));
  process.exit(1);
}

const claimed =
  typeof claim.json.sealed === "string"
    ? fromB64(claim.json.sealed)
    : new Uint8Array(claim.json.sealed);
if (claimed.length !== sealed.length) {
  console.error("claim blob length mismatch");
  process.exit(1);
}

const opened = openAnonymous(claimed, newKP.publicKey, newKP.secretKey);
if (!opened || opened.length !== 32) {
  console.error("open failed");
  process.exit(1);
}
for (let i = 0; i < 32; i++) {
  if (opened[i] !== master[i]) {
    console.error("master mismatch");
    process.exit(1);
  }
}

if (/ghp_|gho_|lin_api_/.test(JSON.stringify(claim.json))) {
  console.error("leak pattern in response");
  process.exit(1);
}

console.log(`prove-device-link-live ok token=${token.slice(0, 8)}… master=${b64(master).slice(0, 12)}…`);
