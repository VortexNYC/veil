#!/usr/bin/env node
/** Approve an existing device-link token (human JWT + smoke sealed wrap). */
import { readFileSync } from "node:fs";
import nacl from "tweetnacl";
import sealedbox from "tweetnacl-sealedbox-js";

const { seal: sealAnonymous } = sealedbox;
const origin = (process.env.VEIL_ORIGIN ?? "https://veil.nyc").replace(/\/$/, "");
const token = process.env.VEIL_DEVICE_LINK_TOKEN?.trim();
const humanPath = process.env.VEIL_HUMAN_TOKEN_FILE?.trim() ?? `${process.env.HOME}/.config/veil/human.jwt`;
if (!token) {
  console.error("VEIL_DEVICE_LINK_TOKEN required");
  process.exit(1);
}
const humanJWT = readFileSync(humanPath, "utf8").trim();

function decodeWireBytes(v) {
  if (typeof v === "string") {
    const bin = Buffer.from(v, "base64");
    return new Uint8Array(bin);
  }
  return new Uint8Array(v);
}

async function get(path) {
  const res = await fetch(`${origin}${path}`, {
    headers: { Authorization: `Bearer ${humanJWT}` },
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

async function post(path, body) {
  const res = await fetch(`${origin}${path}`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${humanJWT}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
  });
  return { status: res.status, text: await res.text() };
}

const row = await get(`/v1/device-links/${token}`);
if (row.status !== 200 || !row.json?.pubkey) {
  console.error("get link failed", row.status, row.text?.slice(0, 200));
  process.exit(1);
}
const pub = decodeWireBytes(row.json.pubkey);
const master = nacl.randomBytes(32);
const sealed = sealAnonymous(master, pub);
const approve = await post(`/v1/device-links/${token}/approve`, { sealed: [...sealed] });
if (approve.status !== 204) {
  console.error("approve failed", approve.status, approve.text?.slice(0, 200));
  process.exit(1);
}
console.log(`approve-device-link-live ok token=${token.slice(0, 8)}…`);
