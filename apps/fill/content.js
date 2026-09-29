(function () {
  let lastOTPAuth = "";
  let focusEl = null;
  let menu = null;

  // Inline suggestion list under the focused field. Page DOM, not the toolbar
  // popover — the popover steals keyboard focus and the field never gets it
  // back. The menu never takes focus: picks happen on mousedown+preventDefault,
  // keys stay on the field (arrows/Enter/Escape handled in capture).
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
    const top = r.bottom + (window.scrollY || 0) + 4;
    const left = Math.max(8, r.left + (window.scrollX || 0));
    menu.el.style.left = left + "px";
    // Flip above the field when the menu would overflow the viewport bottom.
    const menuH = menu.el.offsetHeight || 0;
    const bottom = top - (window.scrollY || 0) + menuH;
    if (bottom > (window.innerHeight || 0) - 8 && r.top - menuH - 4 > 0) {
      menu.el.style.top = r.top + (window.scrollY || 0) - menuH - 4 + "px";
    } else {
      menu.el.style.top = top + "px";
    }
    menu.el.style.minWidth = Math.max(220, r.width) + "px";
  }

  function pickEntry(e) {
    hideMenu();
    chrome.runtime.sendMessage({ type: "suggest-pick", uuid: e.uuid, url: location.href });
  }

  function menuRow(label, sub, onPick) {
    const row = document.createElement("div");
    row.style.cssText =
      "padding:8px 12px;cursor:pointer;border-radius:8px;" +
      "font:13px/-1.3 -apple-system,system-ui,sans-serif;";
    const name = document.createElement("div");
    name.style.cssText = "color:#f0ede8;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;";
    name.textContent = label;
    row.appendChild(name);
    if (sub) {
      const s = document.createElement("div");
      s.style.cssText = "color:#a09a90;font-size:12px;margin-top:2px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;";
      s.textContent = sub;
      row.appendChild(s);
    }
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
    menu.rows.forEach(function (row, j) {
      row.style.background = j === i ? "#35312a" : "transparent";
    });
  }

  function showMenu(el, entries, ctx) {
    hideMenu();
    const box = document.createElement("div");
    box.style.cssText =
      "position:absolute;z-index:2147483647;background:#1c1a17;border:1px solid #35312a;" +
      "border-radius:10px;padding:4px;box-shadow:0 8px 28px rgba(0,0,0,0.55);" +
      "max-height:260px;overflow-y:auto;";
    const rows = [];
    (entries || []).forEach(function (e) {
      rows.push(menuRow(e.name || "item", e.login || (e.kind !== "login" ? e.kind : ""), function () {
        pickEntry(e);
      }));
    });
    if (ctx && ctx.generate) {
      rows.push(
        menuRow("Suggest a password", "generate and save", function () {
          hideMenu();
          chrome.runtime.sendMessage({
            type: "suggest-generate",
            url: location.href,
            login: ctx.login || "",
            passwordRules: ctx.passwordRules || "",
          });
        }),
      );
    }
    if (!rows.length) {
      return;
    }
    rows.forEach(function (r) {
      box.appendChild(r);
    });
    menu = { el: box, rows: rows, active: -1, forEl: el };
    (document.body || document.documentElement).appendChild(box);
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
        highlight(((menu.active + (ev.key === "ArrowDown" ? 1 : -1)) % n + n) % n);
        return;
      }
      if (ev.key === "Enter" && menu.active >= 0) {
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
    },
    true,
  );
  window.addEventListener("resize", hideMenu);

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
      } else if (el && document.contains(el) && document.activeElement === el) {
        showMenu(el, msg.entries, { generate: !!msg.generate, login: msg.login, passwordRules: msg.passwordRules });
      }
      sendResponse({ ok: true });
      return true;
    }
    if (msg.type === "suggest-hide") {
      hideMenu();
      sendResponse({ ok: true });
      return true;
    }
  });

  document.addEventListener(
    "focusin",
    function (ev) {
      if (!ev.isTrusted || !veilFields.isFillTarget(ev.target)) {
        return;
      }
      focusEl = ev.target;
      const ctx = probe(ev.target);
      chrome.runtime.sendMessage({
        type: "trusted-focus",
        generate: ctx.generate,
        login: ctx.login,
        passwordRules: ctx.passwordRules,
        context: ctx.context,
      });
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
