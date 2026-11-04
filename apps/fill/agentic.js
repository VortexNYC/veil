// Partner browser-automation API (Browserbase / Playwright CDP shape).
// Callable only from the extension service worker via page.evaluate on the
// worker target — never exposed to web pages.
(function () {
  const root = typeof globalThis !== "undefined" ? globalThis : self;

  function ok(result) {
    return { success: true, result: result };
  }

  function fail(code) {
    return { success: false, error: { code: code || "internal" } };
  }

  async function whenSettled() {
    if (typeof hostSend !== "function") {
      return "internal";
    }
    try {
      const msg = await hostSend({ action: "ping" }, 8000);
      if (msg && msg.error === "need_login") {
        return "need_login";
      }
      return "ready";
    } catch {
      return "internal";
    }
  }

  async function enableAgenticMode(opts) {
    const tabId = opts && opts.tabId;
    if (tabId != null) {
      agenticTabs.add(tabId);
    } else {
      agenticAll = true;
    }
    return ok({});
  }

  async function disableAgenticMode(opts) {
    const tabId = opts && opts.tabId;
    if (tabId != null) {
      agenticTabs.delete(tabId);
    } else {
      agenticAll = false;
      agenticTabs.clear();
    }
    return ok({});
  }

  let agenticAll = false;
  const agenticTabs = new Set();

  function agenticModeOn(tabId) {
    return agenticAll || agenticTabs.has(tabId);
  }

  async function checkRequestStatus(args) {
    const requestId = args && args.requestId;
    if (!requestId) {
      return fail("invalidArgument");
    }
    try {
      const msg = await hostSend({ action: "agenticStatus", request_id: requestId }, 15000);
      if (msg && msg.error) {
        return fail(msg.error);
      }
      return ok({
        state: msg.status || "pending",
        itemId: msg.item_id || "",
      });
    } catch {
      return fail("networkError");
    }
  }

  async function fillCredential(args) {
    const tabId = args && args.tabId;
    const requestId = args && args.requestId;
    const itemId = args && (args.itemId || args.entryId);
    const submit = !!(args && args.submit);
    if (tabId == null || !requestId || !itemId) {
      return fail("invalidArgument");
    }
    if (!agenticModeOn(tabId)) {
      return fail("agenticModeNotEnabled");
    }
    let tab;
    try {
      tab = await chrome.tabs.get(tabId);
    } catch {
      return fail("tabNotFound");
    }
    const url = tab && tab.url;
    if (!url || !usable(url)) {
      return fail("invalidArgument");
    }
    let msg;
    try {
      msg = await hostSend(
        { action: "agenticFill", url: url, uuid: itemId, request_id: requestId },
        120000,
      );
    } catch {
      return fail("networkError");
    }
    if (needLogin(msg)) {
      return fail("need_login");
    }
    const entries = (msg && msg.entries) || [];
    if (!entries.length) {
      return fail("denied");
    }
    await primeWrite(tabId);
    const wrote = await writeTab(tabId, entries[0]);
    if (!wrote) {
      return fail("fillFailed");
    }
    if (submit) {
      await chrome.tabs.sendMessage(tabId, { type: "agentic-submit" }).catch(function () {});
    }
    return ok({ filled: true });
  }

  root.veilAgentic = {
    v1: {
      initialization: {
        whenSettled: whenSettled,
      },
      agenticMode: {
        enable: enableAgenticMode,
        disable: disableAgenticMode,
      },
      autofill: {
        checkRequestStatus: checkRequestStatus,
        fillCredential: fillCredential,
      },
    },
  };
})();
