(function () {
  let lastOTPAuth = "";
  let focusEl = null;
  let menu = null;

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
    (entries || []).forEach(function (e) {
      rows.push(
        menuRow(
          { name: e.name || "item", sub: e.login || (e.kind !== "login" ? e.kind : ""), kind: e.kind, slim: true },
          function () {
            pickEntry(e);
          },
        ),
      );
    });
    if (ctx && ctx.generate) {
      rows.push(
        menuRow({ name: "Suggest a password", sub: "generate and save", kind: "generate", slim: true }, function () {
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
