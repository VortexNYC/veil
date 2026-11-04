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
      ...init.headers,
    },
  });
  if (!res.ok) throw new Error(`${path} ${res.status}`);
  return res.json();
}

async function findWorker(context) {
  for (let i = 0; i < 60; i++) {
    for (const w of context.serviceWorkers()) {
      const u = w.url();
      if (u.startsWith("chrome-extension://") && u.endsWith("/background.js")) {
        return w;
      }
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
  await showApproval(
    filed.approval_url || "https://app.veil.nyc/requests",
    filed.request_id,
  );

  const deadline = Date.now() + 120_000;
  let approved = false;
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 2000));
    const st = await agentFetch(`/v1/agent/fill/request/${filed.request_id}`);
    if (st.status === "approved") {
      approved = true;
      break;
    }
    if (st.status === "denied" || st.status === "expired") {
      throw new Error(`request ${st.status}`);
    }
  }
  if (!approved) throw new Error("approval timeout");

  const profile =
    process.env.VEIL_AGENTIC_PROFILE ||
    `${process.env.HOME}/.veil/agentic-chromium-profile`;
  const launchOpts = {
    headless: process.env.VEIL_HEADLESS === "1",
    ignoreDefaultArgs: ["--disable-extensions"],
    args: [
      `--disable-extensions-except=${EXT}`,
      `--load-extension=${EXT}`,
    ],
  };
  const exe = process.env.VEIL_CHROMIUM_EXECUTABLE?.trim();
  if (exe) launchOpts.executablePath = exe;
  const context = await chromium.launchPersistentContext(profile, launchOpts);
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
    console.error(`fill ok request=${filed.request_id}`);
    if (process.env.VEIL_SKIP_SUCCESS_URL === "1") {
      await new Promise((r) => setTimeout(r, 5000));
      return;
    }
    await page.waitForURL(
      (u) => {
        try {
          const got = new URL(u);
          return (
            got.origin === expected.origin &&
            got.pathname.startsWith(expected.pathname)
          );
        } catch {
          return false;
        }
      },
      { timeout: Number(process.env.VEIL_SUCCESS_TIMEOUT_MS || 120_000) },
    );
  } finally {
    await context.close();
  }
}

if (import.meta.url === `file://${process.argv[1]}`) {
  signIn({
    showApproval: async (url, requestId) => {
      console.error("Approve in vault:", url, requestId || "");
    },
  }).catch((err) => {
    console.error(err);
    process.exit(1);
  });
}
