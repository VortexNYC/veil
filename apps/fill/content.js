(function () {
  // Safari can inject a content script twice into one document — both
  // instances would answer "suggest" and stack menus. The DOM is shared
  // across worlds, so a marker on the root lets the second instance bail.
  if (document.documentElement.dataset.veilContent) {
    return;
  }
  document.documentElement.dataset.veilContent = "1";

  let lastOTPAuth = "";
  let focusEl = null;
  let menu = null;
  let icon = null;
  let keepalive = null;

  // Safari event pages suspend ~30s after load and can silently stop waking
  // on sendMessage — the field then looks dead. An open port pins the
  // background context resident while a fill session is possible on the page.
  function holdBackground() {
    if (keepalive) {
      return;
    }
    try {
      keepalive = chrome.runtime.connect({ name: "veil-field" });
      keepalive.onDisconnect.addListener(function () {
        keepalive = null;
      });
    } catch (e) {
      keepalive = null;
    }
  }

  // The same ask focusin sends — extracted so the field icon can re-trigger
  // a suggestion for an already-focused field (no second focus event fires).
  function sendFocus(el) {
    focusEl = el;
    holdBackground();
    const ctx = probe(el);
    chrome.runtime.sendMessage({
      type: "trusted-focus",
      generate: ctx.generate,
      login: ctx.login,
      passwordRules: ctx.passwordRules,
      context: ctx.context,
    });
  }

  // The in-field glyph: 1Password's affordance — once a field has something
  // to offer, a quiet mark inside its right edge stays as the way back in.
  // mousedown+preventDefault keeps focus on the field; a click toggles the
  // menu. No offers, no icon — the silence rule stands.
  function hideIcon() {
    if (icon && icon.el && icon.el.parentNode) {
      icon.el.parentNode.removeChild(icon.el);
    }
    icon = null;
  }

  function placeIcon() {
    if (!icon) {
      return;
    }
    const el = icon.forEl;
    if (!document.contains(el)) {
      hideIcon();
      return;
    }
    const r = el.getBoundingClientRect();
    icon.el.style.top = r.top + (window.scrollY || 0) + Math.max(0, (r.height - 22) / 2) + "px";
    icon.el.style.left = r.right + (window.scrollX || 0) - 27 + "px";
  }

  function showIconFor(el) {
    hideIcon();
    const host = document.createElement("div");
    host.style.cssText = "position:absolute;z-index:2147483646;";
    const shade = host.attachShadow({ mode: "open" });
    const style = document.createElement("style");
    style.textContent = veilUI.css;
    shade.appendChild(style);
    const btn = veilUI.el("button", "v-field-icon");
    btn.type = "button";
    btn.title = "Veil";
    btn.innerHTML = veilUI.glyphs.veil;
    btn.addEventListener("mousedown", function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      if (menu && menu.forEl === el) {
        hideMenu();
        return;
      }
      if (document.activeElement === el) {
        sendFocus(el);
      } else {
        // The trusted focusin carries the ask — do not send a second one.
        el.focus();
      }
    });
    shade.appendChild(btn);
    (document.body || document.documentElement).appendChild(host);
    icon = { el: host, forEl: el };
    placeIcon();
  }

  // Inline suggestion list under the focused field. Page DOM, not the toolbar
  // popover — the popover steals keyboard focus and the field never gets it
  // back. The menu never takes focus: picks happen on mousedown+preventDefault,
  // keys stay on the field (arrows/Enter/Escape handled in capture). The visual
  // tree lives in an open shadow root so page CSS can neither restyle it nor
  // be restyled by it.
  function hideMenu() {
    if (menu && menu.el && menu.el.parentNode) {
      menu.el.parentNode.removeChild(menu.el);
    }
    menu = null;
  }

  function placeMenu(el) {
    if (!menu || !menu.el) {
      return;
    }
    const r = el.getBoundingClientRect();
    // absolute + scroll offsets — Safari resolves this identically to fixed on
    // an unscrolled page but keeps working when fixed glitches on early rects.
    // 2px off the field and its exact width, like the native autofill line.
    const top = r.bottom + (window.scrollY || 0) + 2;
    const left = Math.max(8, r.left + (window.scrollX || 0));
    menu.el.style.left = left + "px";
    // Flip above the field when the menu would overflow the viewport bottom.
    const menuH = menu.el.offsetHeight || 0;
    const bottom = top - (window.scrollY || 0) + menuH;
    if (bottom > (window.innerHeight || 0) - 8 && r.top - menuH - 4 > 0) {
      menu.el.style.top = r.top + (window.scrollY || 0) - menuH - 2 + "px";
    } else {
      menu.el.style.top = top + "px";
    }
    menu.el.style.width = Math.max(220, r.width) + "px";
    menu.el.style.maxWidth = "400px";
  }

  function pickEntry(e) {
    hideMenu();
    chrome.runtime.sendMessage({ type: "suggest-pick", uuid: e.uuid, url: location.href });
  }

  function menuRow(opts, onPick) {
    const row = veilUI.entryRow(opts);
    row.addEventListener("mousedown", function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      onPick();
    });
    row.addEventListener("mouseenter", function () {
      if (menu) {
        highlight(menu.rows.indexOf(row));
      }
    });
    return row;
  }

  function highlight(i) {
    if (!menu) {
      return;
    }
    menu.active = i;
    veilUI.setActive(menu.rows, i);
  }

  function showMenu(el, entries, ctx) {
    hideMenu();
    const host = document.createElement("div");
    host.style.cssText = "position:absolute;z-index:2147483647;";
    const shade = host.attachShadow({ mode: "open" });
    const style = document.createElement("style");
    style.textContent = veilUI.css;
    shade.appendChild(style);
    const box = document.createElement("div");
    box.className = "v-menu v-field";
    box.style.cssText = "max-height:264px;overflow-y:auto;";
    box.setAttribute("role", "listbox");
    shade.appendChild(box);
    const rows = [];
    const primary = (entries || []).filter(function (e) {
      return !e.affiliated;
    });
    const related = (entries || []).filter(function (e) {
      return !!e.affiliated;
    });
    const addEntry = function (e, sec) {
      const badges = [];
      if (e.hasTotp) {
        badges.push("totp");
      }
      if (e.hasPasskey) {
        badges.push("passkey");
      }
      const row = menuRow(
        { name: e.name || "item", sub: e.login || (e.kind !== "login" ? e.kind : ""), kind: e.kind, slim: true, badges: badges },
        function () {
          pickEntry(e);
        },
      );
      if (sec) {
        row.dataset.sec = sec;
      }
      rows.push(row);
      box.appendChild(row);
    };
    primary.forEach(function (e) {
      addEntry(e, "");
    });
    if (related.length) {
      if (primary.length) {
        const sec = veilUI.el("div", "v-sec", "Related sites");
        sec.dataset.sec = "related";
        box.appendChild(sec);
      }
      related.forEach(function (e) {
        addEntry(e, "related");
      });
    }
    if (ctx && ctx.generate) {
      const rotates = ctx.rotates || [];
      const genRow = function (uuid, name, sub) {
        return menuRow({ name: name, sub: sub, kind: "generate", slim: true }, function () {
          hideMenu();
          chrome.runtime.sendMessage({
            type: "suggest-generate",
            url: location.href,
            login: ctx.login || "",
            passwordRules: ctx.passwordRules || "",
            uuid: uuid || "",
          });
        });
      };
      if (rotates.length) {
        rotates.forEach(function (e) {
          const r = genRow(e.uuid, "New password", "replaces " + (e.login || e.name || "this login"));
          rows.push(r);
          box.appendChild(r);
        });
      } else {
        const r = genRow("", "Suggest a password", "generate and save");
        rows.push(r);
        box.appendChild(r);
      }
    }
    if (!rows.length) {
      return;
    }
    rows.forEach(function (r) {
      r.dataset.q = r.textContent.toLowerCase();
    });
    menu = { el: host, rows: rows, active: -1, forEl: el };
    (document.body || document.documentElement).appendChild(host);
    placeMenu(el);
    // The focused field can report a pre-layout rect when focus is restored
    // during load — re-place once the frame settles instead of trusting it.
    requestAnimationFrame(function () {
      if (menu && menu.forEl === el) {
        placeMenu(el);
      }
    });
    setTimeout(function () {
      if (menu && menu.forEl === el) {
        placeMenu(el);
      }
    }, 250);
    highlight(0);
  }

  // In-page save offer — action.openPopup does not exist on Safari (and can
  // be refused at runtime on Chromium), so the offer draws under the
  // password field in the same inline language as the fill menu. Not
  // auto-highlighted: an Enter meant for the page must never save.
  function showSavePrompt(msg) {
    const fields = veilFields.pickFields(Array.prototype.slice.call(document.querySelectorAll("input, textarea")));
    const el = fields.password || (fields.newPassword && fields.newPassword[0]) || focusEl;
    if (!el || !document.contains(el)) {
      return;
    }
    hideMenu();
    const host = document.createElement("div");
    host.style.cssText = "position:absolute;z-index:2147483647;";
    const shade = host.attachShadow({ mode: "open" });
    const style = document.createElement("style");
    style.textContent = veilUI.css;
    shade.appendChild(style);
    const box = document.createElement("div");
    box.className = "v-menu v-field";
    box.setAttribute("role", "listbox");
    shade.appendChild(box);
    const name = msg.update ? "Update password" : "Save to Veil";
    const sub = (msg.name ? msg.name + " — " : "") + (msg.login || el.name || "this site");
    const rows = [
      menuRow({ name: name, sub: sub, kind: msg.update ? "update" : "save", slim: true }, function () {
        hideMenu();
        chrome.runtime.sendMessage({ type: "save-pick" });
      }),
      menuRow({ name: "Not now", sub: "", kind: "dismiss", slim: true }, function () {
        hideMenu();
        chrome.runtime.sendMessage({ type: "save-dismiss" });
      }),
    ];
    rows.forEach(function (r) {
      r.dataset.q = r.textContent.toLowerCase();
      box.appendChild(r);
    });
    menu = { el: host, rows: rows, active: -1, forEl: el };
    (document.body || document.documentElement).appendChild(host);
    placeMenu(el);
    requestAnimationFrame(function () {
      if (menu && menu.forEl === el) {
        placeMenu(el);
      }
    });
  }

  document.addEventListener(
    "keydown",
    function (ev) {
      if (!menu) {
        return;
      }
      if (ev.key === "Escape") {
        ev.preventDefault();
        ev.stopPropagation();
        hideMenu();
        return;
      }
      if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
        ev.preventDefault();
        ev.stopPropagation();
        const n = menu.rows.length;
        // Skip rows the type-filter hid — a filtered-out credential must
        // never be the Enter pick.
        let next = menu.active;
        for (let i = 0; i < n; i++) {
          next = (((next + (ev.key === "ArrowDown" ? 1 : -1)) % n) + n) % n;
          if (menu.rows[next].style.display !== "none") {
            break;
          }
        }
        highlight(next);
        return;
      }
      if (ev.key === "Enter" && menu.active >= 0 && menu.rows[menu.active].style.display !== "none") {
        ev.preventDefault();
        ev.stopPropagation();
        menu.rows[menu.active].dispatchEvent(new MouseEvent("mousedown", { bubbles: false }));
      }
    },
    true,
  );

  document.addEventListener(
    "focusout",
    function () {
      // The menu never holds focus, so any real blur means the user left.
      setTimeout(hideMenu, 0);
    },
    true,
  );
  document.addEventListener(
    "mousedown",
    function (ev) {
      if (menu && menu.el && !menu.el.contains(ev.target)) {
        hideMenu();
      }
    },
    true,
  );
  window.addEventListener(
    "scroll",
    function () {
      if (menu && menu.forEl) {
        placeMenu(menu.forEl);
      }
      placeIcon();
    },
    true,
  );
  window.addEventListener("resize", function () {
    hideMenu();
    placeIcon();
  });

  function probe(target) {
    const fields = veilFields.pickFields(Array.prototype.slice.call(document.querySelectorAll("input, textarea")));
    const el = target || document.activeElement;
    const login = fields.username && fields.username.value ? String(fields.username.value) : "";
    const rules =
      (el && el.getAttribute && (el.getAttribute("passwordrules") || el.getAttribute("passwordRules"))) || "";
    return {
      generate: !!(el && veilFields.auto(el) === "new-password"),
      canGenerate: !!(fields.newPassword && fields.newPassword.length),
      canSave: veilFields.canSave(fields, fields.password),
      login: login,
      passwordRules: rules,
      context: veilFields.fillContext(el, fields),
    };
  }

  function typed() {
    const fields = veilFields.pickFields(Array.prototype.slice.call(document.querySelectorAll("input, textarea")));
    if (!veilFields.canSave(fields, fields.password)) {
      return { login: "", password: "" };
    }
    return {
      login: fields.username && fields.username.value ? String(fields.username.value) : "",
      password: String(fields.password.value),
    };
  }

  function reportOTPAuth(uri) {
    if (!uri || uri === lastOTPAuth) {
      return;
    }
    lastOTPAuth = uri;
    chrome.runtime.sendMessage({ type: "found-otpauth", otpauth: uri });
  }

  function scanOTPAuth(root) {
    const uri = veilFields.findOTPAuth(root || document);
    if (uri) {
      reportOTPAuth(uri);
    }
  }

  async function scanQR() {
    if (typeof BarcodeDetector === "undefined") {
      return;
    }
    let det;
    try {
      det = new BarcodeDetector({ formats: ["qr_code"] });
    } catch {
      return;
    }
    const imgs = document.images || [];
    for (let i = 0; i < imgs.length; i++) {
      try {
        const codes = await det.detect(imgs[i]);
        for (let j = 0; j < codes.length; j++) {
          const v = codes[j] && codes[j].rawValue ? String(codes[j].rawValue) : "";
          if (v.indexOf("otpauth://totp") === 0) {
            reportOTPAuth(v);
            return;
          }
        }
      } catch {
        /* image not readable */
      }
    }
  }

  chrome.runtime.onMessage.addListener(function (msg, _sender, sendResponse) {
    if (!msg || !msg.type) {
      return;
    }
    if (msg.type === "write") {
      try {
        sendResponse({ ok: !!veilFields.writeEntry(document, msg.entry || msg) });
      } catch {
        sendResponse({ ok: false });
      }
      return true;
    }
    if (msg.type === "probe") {
      sendResponse(probe());
      return true;
    }
    if (msg.type === "typed") {
      sendResponse(typed());
      return true;
    }
    if (msg.type === "otpauth") {
      sendResponse({ otpauth: lastOTPAuth || veilFields.findOTPAuth(document) || "" });
      return true;
    }
    if (msg.type === "suggest") {
      const el = focusEl;
      // Only render while the field still holds focus — a late reply after a
      // blur must not pop a menu over a page the human already left.
      if (el && document.contains(el) && document.activeElement === el && !(msg.entries || []).length && !msg.generate) {
        hideMenu();
        if (icon && icon.forEl === el) {
          hideIcon();
        }
      } else if (el && document.contains(el) && document.activeElement === el) {
        showMenu(el, msg.entries, { generate: !!msg.generate, rotates: msg.rotates || [], login: msg.login, passwordRules: msg.passwordRules });
        showIconFor(el);
      }
      sendResponse({ ok: true });
      return true;
    }
    if (msg.type === "suggest-hide") {
      hideMenu();
      hideIcon();
      sendResponse({ ok: true });
      return true;
    }
    if (msg.type === "save-prompt") {
      showSavePrompt(msg);
      sendResponse({ ok: true });
      return true;
    }
    if (msg.type === "veil-fill") {
      // Cmd-\: aim the flow at the field that owns the ask — the one with
      // focus, else the first field we can fill — and let focusin carry it.
      const el = pickTarget();
      if (el) {
        if (document.activeElement === el) {
          sendFocus(el);
        } else {
          el.focus();
        }
      }
      sendResponse({ ok: !!el });
      return true;
    }
  });

  function pickTarget() {
    if (focusEl && document.contains(focusEl) && veilFields.isFillTarget(focusEl)) {
      return focusEl;
    }
    const active = document.activeElement;
    if (veilFields.isFillTarget(active)) {
      return active;
    }
    const fields = veilFields.pickFields(Array.prototype.slice.call(document.querySelectorAll("input, textarea")));
    return fields.password || (fields.newPassword && fields.newPassword[0]) || fields.username || fields.number || null;
  }

  document.addEventListener(
    "focusin",
    function (ev) {
      if (!ev.isTrusted || !veilFields.isFillTarget(ev.target)) {
        return;
      }
      sendFocus(ev.target);
    },
    true,
  );

  // Typing is an answer too: filter the rows live, and when nothing matches
  // anymore the menu backs off — the human is typing, not choosing.
  document.addEventListener(
    "input",
    function (ev) {
      if (!menu || ev.target !== menu.forEl) {
        return;
      }
      const q = String(menu.forEl.value || "").toLowerCase();
      let visible = 0;
      menu.rows.forEach(function (row) {
        const show = !q || (row.dataset.q || "").indexOf(q) !== -1;
        row.style.display = show ? "" : "none";
        if (show) {
          visible++;
        }
      });
      // A section header with no visible rows under it goes with them.
      const secs = menu.el.querySelectorAll(".v-sec");
      secs.forEach(function (sec) {
        const anyVisible = menu.rows.some(function (row) {
          return row.dataset.sec === sec.dataset.sec && row.style.display !== "none";
        });
        sec.style.display = anyVisible ? "" : "none";
      });
      if (q && !visible) {
        hideMenu();
      }
    },
    true,
  );

  document.addEventListener(
    "submit",
    function (ev) {
      if (!ev.isTrusted) {
        return;
      }
      const got = typed();
      if (!got.password) {
        return;
      }
      chrome.runtime.sendMessage({ type: "offer-save", login: got.login, password: got.password });
    },
    true,
  );

  scanOTPAuth(document);
  scanQR();
  if (typeof MutationObserver === "function") {
    const obs = new MutationObserver(function (muts) {
      for (let i = 0; i < muts.length; i++) {
        const nodes = muts[i].addedNodes || [];
        for (let j = 0; j < nodes.length; j++) {
          if (nodes[j].nodeType === 1) {
            scanOTPAuth(nodes[j]);
          }
        }
      }
    });
    obs.observe(document.documentElement, { childList: true, subtree: true });
  }
})();
