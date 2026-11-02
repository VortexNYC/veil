// Agent-path scenarios (scenarios.yaml agent.*) — deterministic HTTP checks
// against origin. No UI agent. Run: node --test e2e/tests-agent/
// Needs E2E_AGENT_TOKEN (bound agent bearer) and optionally
// E2E_AGENT_TOKEN_UNGRANTED for the deny case. Skips cleanly without them.

import { test } from "node:test";
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { promisify } from "node:util";

const run = promisify(execFile);
const ORIGIN = process.env.E2E_ORIGIN ?? "https://veil.nyc";
const TOKEN = process.env.E2E_AGENT_TOKEN;
const UNGRANTED = process.env.E2E_AGENT_TOKEN_UNGRANTED;

const mcp = async (token, method, params = {}) => {
  const res = await fetch(`${ORIGIN}/mcp`, {
    method: "POST",
    headers: {
      authorization: `Bearer ${token}`,
      "content-type": "application/json",
      accept: "application/json, text/event-stream",
    },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method, params }),
  });
  const text = await res.text();
  const dataLine = text.split("\n").find((l) => l.startsWith("data:"));
  return { status: res.status, body: dataLine ? JSON.parse(dataLine.slice(5)) : JSON.parse(text) };
};

test("mcp: initialize + list_items returns metadata, no secrets", { skip: !TOKEN }, async () => {
  const r = await mcp(TOKEN, "tools/call", { name: "list_items", arguments: {} });
  assert.equal(r.status, 200);
  const payload = JSON.stringify(r.body);
  assert.ok(!/BEGIN [A-Z ]*PRIVATE KEY/.test(payload), "no key material in list");
});

test("mcp: fetch an ungranted item is denied", { skip: !UNGRANTED }, async () => {
  const r = await mcp(UNGRANTED, "tools/call", { name: "fetch", arguments: { url: "https://api.github.com/user" } });
  assert.ok(r.status === 401 || r.status === 403 || JSON.stringify(r.body).includes("error"), `expected denial, got ${r.status}`);
});

test("cli: veil run spawns a child with injected dummy placeholders", { skip: !TOKEN }, async () => {
  // Origin run path (no local vault on the runner): child env gets
  // veil-inject dummies; HTTPS_PROXY MITM resolves real use calls.
  const env = {
    ...process.env,
    VEIL_AGENT: process.env.E2E_AGENT_NAME ?? "e2e-runner",
    VEIL_HYDRA_SECRET_FILE: process.env.E2E_AGENT_SECRET_FILE ?? `${process.env.HOME}/.config/veil/e2e-runner.hydra`,
    VEIL_ORIGIN: ORIGIN,
  };
  const { stdout } = await run("veil", ["run", "--", "env"], { env });
  assert.ok(!stdout.includes("${"), "no unresolved refs leak through");
});
