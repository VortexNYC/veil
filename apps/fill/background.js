// tab.js is imported by the service worker on Chromium; Firefox's event-page
// background loads it via the manifest scripts array instead — no
// importScripts there, so guard the call.
if (typeof importScripts === "function") importScripts("tab.js");

const HOST = "nyc.veil.fill";
const LOGIN = "https://login.veil.nyc";

let port = null;
const waiters = [];
const matches = new Map();
let openedLogin = false;
let lastPage = null;
let pendingSave = null;

function wipePending() {
  if (pendingSave) {
    pendingSave.login = "";
    pendingSave.password = "";
  }
  pendingSave = null;
}

function rememberTab(tab) {
  if (tab && tab.id != null && usable(tab.url)) {
    lastPage = { id: tab.id, url: tab.url };
    try {
      chrome.storage.session.set({ lastPage: lastPage });
    } catch {
      /* session storage optional */
    }
  }
}

async function loadLastPage() {
  if (lastPage) {
    return lastPage;
  }
  try {
    const got = await chrome.storage.session.get("lastPage");
    if (got && got.lastPage && got.lastPage.id != null) {
      lastPage = got.lastPage;
    }
  } catch {
    /* session storage optional */
  }
  return lastPage;
}

function usable(url) {
  return globalThis.veilTab.usable(url);
}

function pageOrigin(url) {
  try {
    return new URL(url).origin;
  } catch {
    return "";
  }
}

function connect() {
  if (port) {
    return;
  }
  port = chrome.runtime.connectNative(HOST);
  port.onMessage.addListener(function (msg) {
    noteLive(msg || {});
    const w = waiters.shift();
    if (w) {
      w.resolve(msg || {});
    }
  });
  port.onDisconnect.addListener(function () {
    port = null;
    const err = chrome.runtime.lastError && chrome.runtime.lastError.message;
    while (waiters.length) {
      waiters.shift().reject(new Error(err || "host gone"));
    }
  });
}

function hostSend(msg, timeoutMs) {
  connect();
  const ms = timeoutMs || 8000;
  return new Promise(function (resolve, reject) {
    if (!port) {
      reject(new Error("no host"));
      return;
    }
    let done = false;
    const finish = function (fn, v) {
      if (done) {
        return;
      }
      done = true;
      clearTimeout(timer);
      fn(v);
    };
    const wait = {
      resolve: function (v) {
        finish(resolve, v);
      },
      reject: function (e) {
        finish(reject, e);
      },
    };
    const timer = setTimeout(function () {
      const i = waiters.indexOf(wait);
      if (i >= 0) {
        waiters.splice(i, 1);
      }
      wait.reject(new Error("host timeout"));
    }, ms);
    waiters.push(wait);
    try {
      port.postMessage(msg);
    } catch (err) {
      waiters.pop();
      wait.reject(err);
    }
  });
}

function needLogin(msg) {
  return msg && msg.error === "need_login";
}

// Any healthy host answer means the token landed — allow the next
// need_login to open the sign-in tab again. The {auth_url} reply doesn't
// count: the human is still signing in and the flow is in-flight.
function noteLive(msg) {
  if (msg && !msg.auth_url && !needLogin(msg) && !(msg.error && msg.error !== "")) {
    openedLogin = false;
  }
}

// The host owns sign-in: {action:"login"} starts the PKCE flow and returns
// the authorize URL — completing it in the tab hands the token straight back.
// login.veil.nyc alone can't reach the host.
function openLogin() {
  if (openedLogin) {
    return;
  }
  openedLogin = true;
  hostSend({ action: "login" }, 15000)
    .then(function (msg) {
      if (msg && msg.auth_url) {
        chrome.tabs.create({ url: msg.auth_url });
        return;
      }
      chrome.tabs.create({ url: LOGIN });
    })
    .catch(function () {
      chrome.tabs.create({ url: LOGIN });
    });
}

async function ping() {
  const msg = await hostSend({ action: "ping" });
  if (needLogin(msg)) {
    openLogin();
  }
  return msg;
}

async function matchTab(tabId, url) {
  if (!usable(url)) {
    matches.delete(tabId);
    await badge(tabId, 0);
    return [];
  }
  const msg = await hostSend({ action: "match", url: url });
  const entries = (msg && msg.entries) || [];
  matches.set(tabId, { url: url, entries: entries });
  await badge(tabId, entries.length);
  return entries;
}

async function badge(tabId, n) {
  const text = n > 0 ? String(n) : "";
  try {
    await chrome.action.setBadgeText({ tabId: tabId, text: text });
  } catch {
    /* tab gone */
  }
}

function wipeEntry(entry) {
  if (!entry) {
    return;
  }
  entry.login = "";
  entry.password = "";
  entry.totp = "";
  entry.number = "";
  entry.cvv = "";
  entry.givenName = "";
  entry.familyName = "";
  entry.address = "";
  entry.phone = "";
}

async function injectWrite(tabId, copy, world) {
  const file = { target: { tabId: tabId }, files: ["fields.js"] };
  const run = {
    target: { tabId: tabId },
    args: [copy],
    func: function (got) {
      return { ok: globalThis.veilFields.writeEntry(document, got) };
    },
  };
  if (world) {
    file.world = world;
    run.world = world;
  }
  try {
    await chrome.scripting.executeScript(file);
    const inj = await chrome.scripting.executeScript(run);
    return !!(inj && inj[0] && inj[0].result && inj[0].result.ok);
  } catch {
    return false;
  }
}

async function primeWrite(tabId) {
  try {
    await chrome.scripting.executeScript({
      target: { tabId: tabId },
      files: ["fields.js", "content.js"],
    });
  } catch {
    /* tab gone */
  }
}

async function writeTab(tabId, entry) {
  const copy = JSON.parse(JSON.stringify(entry || {}));
  let wrote = false;
  try {
    try {
      const res = await chrome.tabs.sendMessage(tabId, { type: "write", entry: copy });
      wrote = !!(res && res.ok);
    } catch {
      wrote = false;
    }
    if (!wrote) {
      wrote = await injectWrite(tabId, copy);
    }
    if (!wrote && copy.kind === "card") {
      wrote = await injectWrite(tabId, copy, "MAIN");
    }
  } finally {
    wipeEntry(copy);
    wipeEntry(entry);
  }
  return wrote;
}

async function fillTab(tabId, url, uuid) {
  await primeWrite(tabId);
  const body = { action: "fill", url: url };
  if (uuid) {
    body.uuid = uuid;
  }
  const msg = await hostSend(body, 90000);
  if (needLogin(msg)) {
    openLogin();
    return { ok: false, error: "need_login" };
  }
  const entries = (msg && msg.entries) || [];
  if (!entries.length) {
    return { ok: false, error: "empty" };
  }
  const wrote = await writeTab(tabId, entries[0]);
  return { ok: wrote };
}

async function generateTab(tabId, url, login, passwordRules, uuid) {
  const body = { action: "generate", url: url };
  if (login) {
    body.login = login;
  }
  if (passwordRules) {
    body.passwordRules = passwordRules;
  }
  if (uuid) {
    body.uuid = uuid;
  }
  const msg = await hostSend(body, 90000);
  if (needLogin(msg)) {
    openLogin();
    return { ok: false, error: "need_login" };
  }
  if (msg && msg.error === "choose") {
    return { ok: false, error: "choose" };
  }
  if (!msg || !msg.password) {
    return { ok: false, error: (msg && msg.error) || "empty" };
  }
  await writeTab(tabId, { kind: "login", login: msg.login || login || "", password: msg.password, totp: "", generated: true });
  await matchTab(tabId, url);
  return { ok: true };
}

async function saveTab(tabId, url, login, password) {
  if (!password) {
    return { ok: false, error: "empty" };
  }
  const body = { action: "save", url: url, login: login || "", password: password };
  const msg = await hostSend(body, 90000);
  password = "";
  if (needLogin(msg)) {
    openLogin();
    return { ok: false, error: "need_login" };
  }
  if (msg && msg.error) {
    return { ok: false, error: msg.error };
  }
  if (!msg || !msg.uuid) {
    return { ok: false, error: "empty" };
  }
  await matchTab(tabId, url);
  return { ok: true };
}

async function enrollTotp(tabId, url, otpauth) {
  if (!otpauth) {
    return { ok: false, error: "empty" };
  }
  const msg = await hostSend({ action: "enrollTotp", url: url, otpauth: otpauth }, 90000);
  if (needLogin(msg)) {
    openLogin();
    return { ok: false, error: "need_login" };
  }
  if (msg && msg.error) {
    return { ok: false, error: msg.error };
  }
  if (!msg || !msg.uuid) {
    return { ok: false, error: "empty" };
  }
  await matchTab(tabId, url);
  return { ok: true };
}

async function executeTab(tabId, url, opts) {
  let hit = matches.get(tabId);
  if (!hit || hit.url !== url) {
    await matchTab(tabId, url);
    hit = matches.get(tabId);
  }
  const entries = (hit && hit.entries) || [];
  if (opts && opts.generate) {
    const logins = entries.filter(function (e) {
      return e.kind === "login";
    });
    if (logins.length) {
      return { ok: false, error: "choose", entries: entries };
    }
    return generateTab(tabId, url, opts.login, opts.passwordRules);
  }
  if (entries.length !== 1 || entries[0].kind !== "login" || entries[0].affiliated) {
    return { ok: false, error: "choose", entries: entries };
  }
  return fillTab(tabId, url, entries[0].uuid);
}

async function probeTab(tabId) {
  try {
    return await chrome.tabs.sendMessage(tabId, { type: "probe" });
  } catch {
    return { generate: false, canGenerate: false, canSave: false, login: "", passwordRules: "" };
  }
}

async function activeTab() {
  const focused = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  if (focused[0] && usable(focused[0].url)) {
    return focused[0];
  }
  const tabs = await chrome.tabs.query({ active: true, currentWindow: true });
  return tabs[0] || null;
}

async function tabByURL(url) {
  const tabs = await chrome.tabs.query({});
  return globalThis.veilTab.tabByURL(tabs, url);
}

async function fillTargetTab() {
  await loadLastPage();
  if (lastPage) {
    try {
      const tab = await chrome.tabs.get(lastPage.id);
      if (tab && usable(tab.url)) {
        return tab;
      }
    } catch {
      lastPage = null;
    }
  }
  const focused = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  if (focused[0] && usable(focused[0].url)) {
    rememberTab(focused[0]);
    return focused[0];
  }
  const tabs = await chrome.tabs.query({});
  for (let i = 0; i < tabs.length; i++) {
    if (usable(tabs[i].url) && tabs[i].active) {
      rememberTab(tabs[i]);
      return tabs[i];
    }
  }
  for (let i = 0; i < tabs.length; i++) {
    if (usable(tabs[i].url)) {
      rememberTab(tabs[i]);
      return tabs[i];
    }
  }
  return null;
}

chrome.runtime.onInstalled.addListener(function () {
  ping().catch(function () {});
});
chrome.runtime.onStartup.addListener(function () {
  ping().catch(function () {});
});

chrome.tabs.onUpdated.addListener(function (tabId, info, tab) {
  const url = tab && (tab.url || tab.pendingUrl);
  if (info.status !== "complete" || !usable(url)) {
    return;
  }
  if (tab.active) {
    rememberTab(tab);
  }
  matchTab(tabId, url).catch(function () {});
});

chrome.tabs.onActivated.addListener(function (info) {
  chrome.tabs.get(info.tabId, function (tab) {
    const url = tab && (tab.url || tab.pendingUrl);
    if (chrome.runtime.lastError || !usable(url)) {
      return;
    }
    rememberTab(tab);
    matchTab(tab.id, url).catch(function () {});
  });
});

chrome.tabs.onRemoved.addListener(function (tabId) {
  matches.delete(tabId);
});

chrome.commands.onCommand.addListener(function (command) {
  if (command !== "fill") {
    return;
  }
  // Cmd-\ routes through the page: content picks the field that owns the
  // ask, focuses it, and the normal trusted-focus → suggest → menu flow
  // runs — the chooser is inline, never the toolbar popover.
  activeTab().then(function (tab) {
    if (!tab || !usable(tab.url)) {
      return;
    }
    rememberTab(tab);
    chrome.tabs.sendMessage(tab.id, { type: "veil-fill" }).then(function (res) {
      if (res && res.ok) {
        return;
      }
      // No content script or no fillable field — fall back to the
      // field-detection write path for a lone match.
      probeTab(tab.id).then(function (ctx) {
        return executeTab(tab.id, tab.url, {
          generate: !!(ctx && ctx.generate),
          login: ctx && ctx.login,
          passwordRules: ctx && ctx.passwordRules,
        });
      });
    }).catch(function () {});
  });
});

chrome.runtime.onMessage.addListener(function (msg, sender, sendResponse) {
  if (!msg || !msg.type) {
    return;
  }
  if (msg.type === "trusted-focus") {
    const tab = sender.tab;
    const url = tab && (tab.url || tab.pendingUrl);
    if (!tab || !usable(url)) {
      return;
    }
    rememberTab(tab);
    // Field focus must never hand keyboard focus to the toolbar popover.
    // Data there: the in-page suggestion under the field, one row per
    // match — pick fills. No data: silence, like the extension isn't there.
    matchTab(tab.id, url)
      .then(function (entries) {
        const plan = globalThis.veilTab.focusPlan(entries, !!msg.generate, msg.context || "");
        if (plan.action === "menu") {
          chrome.tabs
            .sendMessage(tab.id, {
              type: "suggest",
              entries: plan.entries,
              generate: !!plan.generate,
              rotates: plan.rotates || [],
              login: msg.login || "",
              passwordRules: msg.passwordRules || "",
            })
            .catch(function () {});
          return;
        }
        chrome.tabs.sendMessage(tab.id, { type: "suggest-hide" }).catch(function () {});
      })
      .catch(function () {});
    return;
  }
  if (msg.type === "suggest-pick") {
    const tab = sender.tab;
    const url = tab && (tab.url || tab.pendingUrl);
    if (!tab || !usable(url) || !msg.uuid) {
      return;
    }
    fillTab(tab.id, url, msg.uuid).catch(function () {});
    return;
  }
  if (msg.type === "suggest-generate") {
    const tab = sender.tab;
    const url = tab && (tab.url || tab.pendingUrl);
    if (!tab || !usable(url)) {
      return;
    }
    generateTab(tab.id, url, msg.login || "", msg.passwordRules || "", msg.uuid || "").catch(function () {});
    return;
  }
  if (msg.type === "offer-save") {
    const tab = sender.tab;
    const url = tab && (tab.url || tab.pendingUrl);
    if (!tab || !usable(url) || !msg.password) {
      return;
    }
    wipePending();
    pendingSave = { tabId: tab.id, url: url, login: msg.login || "", password: msg.password };
    chrome.action.openPopup().catch(function () {});
    return;
  }
  if (msg.type === "found-otpauth") {
    const tab = sender.tab;
    const url = tab && (tab.url || tab.pendingUrl);
    if (!tab || !usable(url) || !msg.otpauth) {
      return;
    }
    enrollTotp(tab.id, url, msg.otpauth).catch(function () {});
    return;
  }
  if (msg.type === "popup-list") {
    const listed =
      msg.tabId != null
        ? chrome.tabs.get(msg.tabId).then(function (tab) {
            return tab && usable(tab.url) ? tab : fillTargetTab();
          }, function () {
            return fillTargetTab();
          })
        : fillTargetTab();
    listed.then(function (tab) {
      if (!tab || !usable(tab.url)) {
        sendResponse({ entries: [], url: "" });
        return;
      }
      matchTab(tab.id, tab.url).then(function (entries) {
        probeTab(tab.id).then(function (ctx) {
          const liveSave = !!(ctx && ctx.canSave);
          const pending = !!(pendingSave && pendingSave.tabId === tab.id);
          sendResponse({
            entries: entries,
            url: tab.url,
            tabId: tab.id,
            canGenerate: !!(ctx && ctx.canGenerate),
            canSave: liveSave || pending,
            login: (ctx && ctx.login) || (pendingSave && pendingSave.login) || "",
            passwordRules: (ctx && ctx.passwordRules) || "",
          });
        });
      });
    });
    return true;
  }
  if (msg.type === "popup-fill") {
    const listed = usable(msg.url) ? tabByURL(msg.url) : Promise.resolve(null);
    listed.then(function (byURL) {
      return byURL ? byURL : fillTargetTab();
    }).then(function (tab) {
      const url = usable(msg.url) ? msg.url : tab && tab.url;
      const id = tab && tab.id;
      if (!id || !usable(url)) {
        sendResponse({ ok: false, error: "empty" });
        return;
      }
      fillTab(id, url, msg.uuid).then(function (got) {
        sendResponse(got);
      }, function () {
        sendResponse({ ok: false, error: "host" });
      });
    });
    return true;
  }
  if (msg.type === "popup-generate") {
    generateTab(msg.tabId, msg.url, msg.login, msg.passwordRules).then(function (got) {
      sendResponse(got);
    });
    return true;
  }
  if (msg.type === "popup-save") {
    const tabId = msg.tabId;
    const url = msg.url;
    const fromPending = pendingSave && pendingSave.tabId === tabId;
    const creds = fromPending
      ? Promise.resolve({ login: pendingSave.login, password: pendingSave.password })
      : chrome.tabs.sendMessage(tabId, { type: "typed" }).catch(function () {
          return { login: "", password: "" };
        });
    creds.then(function (got) {
      const login = (got && got.login) || "";
      const password = (got && got.password) || "";
      return saveTab(tabId, url, login, password).finally(function () {
        wipePending();
      });
    }).then(function (got) {
      sendResponse(got);
    }, function () {
      wipePending();
      sendResponse({ ok: false, error: "host" });
    });
    return true;
  }
  if (msg.type === "passkeyCreate" || msg.type === "passkeyGet") {
    const tab = sender.tab;
    const url = tab && (tab.url || tab.pendingUrl);
    if (!tab || !usable(url) || msg.origin !== pageOrigin(url)) {
      sendResponse({ error: "failed" });
      return true;
    }
    const req = {
      action: msg.type,
      origin: msg.origin,
      publicKey: msg.publicKey,
    };
    if (msg.type === "passkeyCreate" && msg.relatedOrigins) {
      req.relatedOrigins = msg.relatedOrigins;
    }
    hostSend(req, 90000).then(
      function (got) {
        sendResponse(got || { error: "failed" });
      },
      function () {
        sendResponse({ error: "failed" });
      },
    );
    return true;
  }
});
