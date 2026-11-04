/**
 * Veil agentic sign-in — Browserbase-shaped partner flow.
 *
 * Env:
 *   VEIL_ORIGIN          default https://veil.nyc
 *   VEIL_AGENT_TOKEN     agent bearer (Hydra)
 *   VEIL_EXTENSION_PATH  unpacked MV3 dir (apps/fill or ~/.veil/extension)
 *   VEIL_LOGIN_URL       HTTPS login page
 *   VEIL_SUCCESS_URL     expected destination after sign-in (origin+path check)
 *   VEIL_ITEM_ID         granted item id
 *
 * Requires: playwright-core, a Chromium build that loads extensions.
 */
import fs from "node:fs";
import { chromium } from "playwright-core";

const ORIGIN = process.env.VEIL_ORIGIN || "https://veil.nyc";
const TOKEN = process.env.VEIL_AGENT_TOKEN || "";
const EXT = process.env.VEIL_EXTENSION_PATH || "";
const LOGIN_URL = process.env.VEIL_LOGIN_URL || "";
const SUCCESS_URL = process.env.VEIL_SUCCESS_URL || "";
const ITEM_ID = process.env.VEIL_ITEM_ID || "";
const EXT_ID = "oboonakemofpalcgghocfoadofidjkkk";

function requireEnv() {
  for (const [k, v] of [
    ["VEIL_AGENT_TOKEN", TOKEN],
    ["VEIL_EXTENSION_PATH", EXT],
    ["VEIL_LOGIN_URL", LOGIN_URL],
    ["VEIL_ITEM_ID", ITEM_ID],
  ]) {
    if (!v) throw new Error(`missing ${k}`);
  }
  if (!fs.existsSync(EXT)) throw new Error("extension path missing");
}

async function agentFetch(path, init = {}) {
  const res = await fetch(`${ORIGIN}${path}`, {
    ...init,
    headers: {
      Authorization: `Bearer ${TOKEN}`,
      "Content-Type": "application/json",
      ...(init.headers || {}),
    },
  });
  if (!res.ok) throw new Error(`${path} ${res.status}`);
  return res.json();
}

async function findWorker(context) {
  const want = `chrome-extension://${EXT_ID}/background.js`;
  for (let i = 0; i < 60; i++) {
    for (const w of context.serviceWorkers()) {
      if (w.url() === want) return w;
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error("Veil service worker not found");
}

export async function signIn({ showApproval }) {
  requireEnv();
  const expected = new URL(SUCCESS_URL || LOGIN_URL);
  if (new URL(LOGIN_URL).protocol !== "https:" || expected.protocol !== "https:") {
    throw new Error("Use HTTPS login and success URLs");
  }

  const filed = await agentFetch("/v1/agent/fill/request", {
    method: "POST",
    body: JSON.stringify({ item_id: ITEM_ID, url: LOGIN_URL }),
  });
  await showApproval(filed.approval_url || "https://app.veil.nyc/requests");

  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 2000));
    const st = await agentFetch(`/v1/agent/fill/request/${filed.request_id}`);
    if (st.status === "approved") break;
    if (st.status === "denied" || st.status === "expired") {
      throw new Error(`request ${st.status}`);
    }
  }

  const context = await chromium.launchPersistentContext("", {
    channel: "chromium",
    args: [
      `--disable-extensions-except=${EXT}`,
      `--load-extension=${EXT}`,
    ],
  });
  try {
    const page = await context.newPage();
    await page.goto(LOGIN_URL);
    const worker = await findWorker(context);
    const tabId = await worker.evaluate(async () => {
      const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
      return tab.id;
    });
    await worker.evaluate(async (id) => {
      const s = await veilAgentic.v1.initialization.whenSettled();
      if (s !== "ready") throw new Error(s);
      await veilAgentic.v1.agenticMode.enable({ tabId: id });
    }, tabId);
    const filled = await worker.evaluate(
      async (args) => veilAgentic.v1.autofill.fillCredential(args),
      { tabId, requestId: filed.request_id, itemId: ITEM_ID, submit: true },
    );
    if (!filled.success) throw new Error(filled.error?.code || "fill failed");
    await page.waitForURL((u) => {
      try {
        const got = new URL(u);
        return got.origin === expected.origin && got.pathname.startsWith(expected.pathname);
      } catch {
        return false;
      }
    }, { timeout: 60_000 });
  } finally {
    await context.close();
  }
}

if (import.meta.url === `file://${process.argv[1]}`) {
  signIn({
    showApproval: async (url) => {
      console.error("Approve in vault:", url);
    },
  }).catch((err) => {
    console.error(err);
    process.exit(1);
  });
}
