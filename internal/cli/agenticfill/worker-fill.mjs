/**
 * Agentic fill on the extension service worker for an agent-browser / vb session.
 * Canonical copy lives here for `veil agent fill run`; examples/agentic-autofill mirrors it.
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { chromium } from "playwright-core";

const SESSION = process.env.AGENT_BROWSER_SESSION || "veil-dogfood";
const ITEM_ID = process.env.VEIL_ITEM_ID || "";
const REQUEST_ID = process.env.REQUEST_ID || "";
const PAGE_MATCH = process.env.PAGE_URL_SUBSTR || "github.com/login";

function emitResult(obj) {
  console.error("VEIL_RESULT=" + JSON.stringify(obj));
}

function devToolsPorts() {
  const ports = [];
  const profile = process.env.VEIL_AGENTIC_PROFILE || path.join(process.env.HOME, ".veil/agent-browser-profile");
  const portFile = path.join(profile, "DevToolsActivePort");
  if (fs.existsSync(portFile)) {
    const port = Number(fs.readFileSync(portFile, "utf8").split("\n")[0]);
    if (port > 0) ports.push({ port, dir: profile });
  }
  const tmp = os.tmpdir();
  for (const name of fs.readdirSync(tmp)) {
    if (!name.startsWith("agent-browser-chrome-")) continue;
    const dir = path.join(tmp, name);
    const pf = path.join(dir, "DevToolsActivePort");
    if (!fs.existsSync(pf)) continue;
    const port = Number(fs.readFileSync(pf, "utf8").split("\n")[0]);
    if (port > 0) ports.push({ port, dir });
  }
  return ports;
}

function hostPrefix(pageMatch) {
  return (pageMatch || PAGE_MATCH).split("/")[0];
}

async function pickBrowser(ports, pageMatch) {
  const prefix = hostPrefix(pageMatch);
  for (const { port, dir } of ports) {
    let browser;
    try {
      browser = await chromium.connectOverCDP(`http://127.0.0.1:${port}`);
    } catch {
      continue;
    }
    try {
      const pages = browser.contexts().flatMap((c) => c.pages());
      const page = pages.find((p) => {
        try {
          return p.url().includes(prefix);
        } catch {
          return false;
        }
      });
      let hasVeil = false;
      for (const w of browser.contexts().flatMap((c) => c.serviceWorkers())) {
        const ok = await w
          .evaluate(() => typeof globalThis.veilAgentic === "object")
          .catch(() => false);
        if (ok) {
          hasVeil = true;
          break;
        }
      }
      if (page && hasVeil) {
        return { port, dir, pageUrl: page.url(), page };
      }
    } finally {
      await browser.close();
    }
  }
  throw new Error("no agent-browser Chrome with Veil worker + matching page");
}

function installNativeMessagingHost(userDataDir) {
  const nm = path.join(userDataDir, "NativeMessagingHosts");
  fs.mkdirSync(nm, { recursive: true });
  const srcCfT =
    process.env.HOME +
    "/Library/Application Support/Google/Chrome for Testing/NativeMessagingHosts/nyc.veil.fill.json";
  const srcBr =
    process.env.HOME + "/Library/Application Support/Google/Chrome/NativeMessagingHosts/nyc.veil.fill.json";
  const src = fs.existsSync(srcCfT) ? srcCfT : srcBr;
  if (fs.existsSync(src)) {
    fs.copyFileSync(src, path.join(nm, "nyc.veil.fill.json"));
  }
}

async function loginSurfaceState(page) {
  return page.evaluate(() => {
    const pw = document.querySelector('input[type="password"]');
    const user = document.querySelector(
      'input[type="email"], input[type="text"][name*="user" i], input[name="login"], input#login_field, input[name="identifier"], input[name="username"]',
    );
    return {
      url: location.href,
      hasPassword: !!pw,
      hasUser: !!user,
      userLen: user && "value" in user ? String(user.value).length : 0,
      passwordLen: pw && "value" in pw ? String(pw.value).length : 0,
    };
  });
}

export async function verifyLoginFields({ pageMatch }) {
  const ports = devToolsPorts();
  const prefix = hostPrefix(pageMatch);
  let browser;
  for (const { port, dir } of ports) {
    try {
      browser = await chromium.connectOverCDP(`http://127.0.0.1:${port}`);
    } catch {
      continue;
    }
    try {
      const pages = browser.contexts().flatMap((c) => c.pages());
      const page = pages.find((p) => {
        try {
          return p.url().includes(prefix);
        } catch {
          return false;
        }
      });
      if (!page) continue;
      await page.waitForTimeout(2500);
      const st = await loginSurfaceState(page);
      const offLogin = !/\/login(\?|$|\/)/i.test(st.url) && !st.url.includes("session");
      console.error(
        `verify-fields url=${st.url} hasUser=${st.hasUser} userLen=${st.userLen} hasPassword=${st.hasPassword} passLen=${st.passwordLen}`,
      );
      if (offLogin && st.passwordLen === 0 && st.userLen === 0) {
        console.error("verify-fields ok (left login URL)");
        return { ...st, loggedIn: true };
      }
      if (!st.hasPassword && !st.hasUser) {
        throw new Error("verify-fields: no login form on page (wrong tab or site redirected)");
      }
      if (st.passwordLen === 0 && st.userLen === 0) {
        throw new Error("verify-fields: fill reported ok but credential fields are empty");
      }
      if (st.hasPassword && st.passwordLen === 0) {
        throw new Error("verify-fields: password field empty after fill+submit — check item login/username");
      }
      if (st.hasUser && st.userLen === 0) {
        throw new Error("verify-fields: username/login field empty after fill+submit — check github item login value");
      }
      console.error("verify-fields ok");
      return st;
    } finally {
      await browser.close();
    }
  }
  throw new Error("verify-fields: no browser tab for " + prefix);
}

export async function workerFill({ itemId, requestId, pageMatch }) {
  if (!itemId || !requestId) throw new Error("need VEIL_ITEM_ID and REQUEST_ID");
  const ports = devToolsPorts();
  const { port, dir, pageUrl } = await pickBrowser(ports, pageMatch);
  installNativeMessagingHost(dir);

  const browser = await chromium.connectOverCDP(`http://127.0.0.1:${port}`);
  try {
    let worker;
    for (const w of browser.contexts().flatMap((c) => c.serviceWorkers())) {
      const ok = await w
        .evaluate(() => typeof globalThis.veilAgentic === "object")
        .catch(() => false);
      if (ok) {
        worker = w;
        break;
      }
    }
    if (!worker) throw new Error("Veil service worker not found");

    const tabId = await worker.evaluate(async (match) => {
      const tabs = await chrome.tabs.query({});
      const hit = tabs.find((t) => t.url && t.url.includes(match)) || tabs[0];
      if (!hit?.id) throw new Error("no tab");
      return hit.id;
    }, pageMatch || PAGE_MATCH);

    const submit = process.env.VEIL_AGENTIC_NO_SUBMIT !== "1";

    async function runFill(submitForm) {
      return worker.evaluate(
        async ({ tabId, requestId, itemId, submitForm }) => {
          const s = await veilAgentic.v1.initialization.whenSettled();
          if (s !== "ready") return { ok: false, code: s };
          await veilAgentic.v1.agenticMode.enable({ tabId });
          return veilAgentic.v1.autofill.fillCredential({
            tabId,
            requestId,
            itemId,
            submit: submitForm,
          });
        },
        { tabId, requestId, itemId, submitForm },
      );
    }

    let result = await runFill(submit);
    if (!result?.success) {
      throw new Error(result?.error?.code || "fill failed");
    }

    // Multi-step logins (GitHub: username → password): refill until both fields set or URL moves.
    for (let step = 0; step < 3; step++) {
      await new Promise((r) => setTimeout(r, 2200));
      const pages = browser.contexts().flatMap((c) => c.pages());
      const page = pages.find((p) => {
        try {
          return p.url().includes(hostPrefix(pageMatch));
        } catch {
          return false;
        }
      });
      if (!page) break;
      const st = await loginSurfaceState(page);
      if (!/\/login(\?|$|\/)/i.test(st.url) && !st.url.includes("/session")) {
        break;
      }
      const needsUser = st.hasUser && st.userLen === 0;
      const needsPass = st.hasPassword && st.passwordLen === 0;
      const onSession = /\/session/i.test(st.url);
      if (!needsUser && !needsPass && !onSession) {
        break;
      }
      if (onSession && st.passwordLen === 0) {
        result = await runFill(submit);
        if (!result?.success) {
          throw new Error(result?.error?.code || "fill failed (session step)");
        }
        continue;
      }
      result = await runFill(submit);
      if (!result?.success) {
        throw new Error(result?.error?.code || "fill failed (step " + (step + 2) + ")");
      }
    }

    console.error(`worker fill ok page=${pageUrl} request=${requestId}`);
    return { pageUrl, tabId };
  } finally {
    await browser.close();
  }
}

if (import.meta.url === `file://${process.argv[1]}`) {
  (async () => {
    await workerFill({
      itemId: ITEM_ID,
      requestId: REQUEST_ID,
      pageMatch: PAGE_MATCH,
    });
    if (process.env.VEIL_VERIFY_FIELDS === "1") {
      const st = await verifyLoginFields({ pageMatch: PAGE_MATCH });
      emitResult({
        verify: {
          url: st.url,
          user_len: st.userLen,
          pass_len: st.passwordLen,
          has_user: st.hasUser,
          has_password: st.hasPassword,
        },
      });
    }
  })().catch((err) => {
    console.error(err);
    process.exit(1);
  });
}
