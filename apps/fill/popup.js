const root = document.getElementById("root");

// Inject the shared token/component stylesheet — same source as the in-page
// chooser, so the popup and the inline menu read as one surface.
(function () {
  const s = document.createElement("style");
  s.textContent = veilUI.css;
  document.head.appendChild(s);
})();

function show(html) {
  root.innerHTML = html;
}

function hostOf(url) {
  try {
    return new URL(url).host;
  } catch {
    return "";
  }
}

function send(msg) {
  return new Promise(function (resolve) {
    try {
      chrome.runtime.sendMessage(msg, function (res) {
        if (chrome.runtime.lastError) {
          resolve(null);
          return;
        }
        resolve(res || null);
      });
    } catch {
      resolve(null);
    }
  });
}

// Rendered rows in the visible order — keyboard nav and hover share one
// highlight, Enter activates whichever row it sits on.
const state = {
  site: null, // popup-list reply (matched entries + save/generate context)
  catalog: [], // popup-catalog reply (every fillable item, for search)
  list: null, // .v-list container
  rows: [],
  active: -1,
};

function highlight(i) {
  state.active = i;
  veilUI.setActive(state.rows, i);
  const row = state.rows[i];
  if (row && typeof row.scrollIntoView === "function") {
    row.scrollIntoView({ block: "nearest" });
  }
}

// A row that participates in keyboard nav: hover takes the highlight so
// mouse and arrows never disagree about which row is active.
function track(b) {
  const i = state.rows.length;
  b.addEventListener("mouseenter", function () {
    highlight(i);
  });
  state.rows.push(b);
  state.list.appendChild(b);
}

function clearList() {
  while (state.list.firstChild) {
    state.list.removeChild(state.list.firstChild);
  }
  state.rows = [];
  state.active = -1;
}

function pick(e, fromSearch) {
  const url = (state.site && state.site.url) || "";
  chrome.tabs.query({}, function (tabs) {
    const byURL = url ? globalThis.veilTab.tabByURL(tabs, url) : null;
    const tabId = (state.site && state.site.tabId) || (byURL && byURL.id);
    chrome.runtime.sendMessage(
      { type: "popup-fill", uuid: e.uuid, url: url, tabId: tabId, fromSearch: !!fromSearch },
      function (res) {
        if (res && res.ok) {
          window.close();
          return;
        }
        show('<div class="v-err">Fill canceled or failed.</div>');
      },
    );
  });
}

function copyText(val, btn, label) {
  if (!val || !navigator.clipboard) {
    return;
  }
  navigator.clipboard.writeText(val).then(function () {
    if (btn) {
      btn.textContent = "Copied";
      setTimeout(function () {
        btn.textContent = label;
      }, 1200);
    }
  });
}

function actionBtn(label, title, onClick) {
  const btn = veilUI.el("button", "v-btn", label);
  btn.type = "button";
  if (title) {
    btn.title = title;
  }
  btn.addEventListener("click", function (ev) {
    ev.stopPropagation();
    onClick(btn);
  });
  return btn;
}

function entryButton(e, fromSearch) {
  const b = veilUI.entryRow(
    {
      name: e.name || e.uuid || "item",
      sub: e.login || (e.kind && e.kind !== "login" ? e.kind : ""),
      kind: e.kind,
      icon: e.icon,
    },
    "div",
  );
  b.addEventListener("click", function () {
    pick(e, fromSearch);
  });
  const actions = veilUI.el("div", "v-actions");
  if (e.login) {
    actions.appendChild(
      actionBtn("User", "Copy username / email", function (btn) {
        copyText(e.login, btn, "User");
      }),
    );
  }
  if (e.kind !== "passkey") {
    actions.appendChild(
      actionBtn("Pass", "Copy password (Touch ID)", function (btn) {
        btn.textContent = "…";
        send({ type: "popup-reveal", uuid: e.uuid, mintTotp: false }).then(function (res) {
          if (res && res.ok && res.entry && res.entry.password) {
            copyText(res.entry.password, btn, "Pass");
          } else {
            btn.textContent = "Err";
            setTimeout(function () {
              btn.textContent = "Pass";
            }, 1200);
          }
        });
      }),
    );
  }
  if (e.totp === "*") {
    actions.appendChild(
      actionBtn("2FA", "Copy 6-digit 2FA code", function (btn) {
        btn.textContent = "…";
        send({ type: "popup-reveal", uuid: e.uuid, mintTotp: true }).then(function (res) {
          if (res && res.ok && res.entry && res.entry.totp) {
            copyText(res.entry.totp, btn, "2FA");
          } else {
            btn.textContent = "Err";
            setTimeout(function () {
              btn.textContent = "2FA";
            }, 1200);
          }
        });
      }),
    );
  }
  const firstURI = (e.uris && e.uris[0]) || "";
  if (firstURI && (firstURI.indexOf("http://") === 0 || firstURI.indexOf("https://") === 0)) {
    actions.appendChild(
      actionBtn("Open", "Open website", function () {
        chrome.tabs.create({ url: firstURI });
        window.close();
      }),
    );
  }
  b.appendChild(actions);
  return b;
}

function suggestButton(got) {
  const b = veilUI.entryRow({ name: "Suggest a password", sub: "generate and save", kind: "generate" }, "button");
  b.addEventListener("click", function () {
    chrome.runtime.sendMessage(
      {
        type: "popup-generate",
        tabId: got.tabId,
        url: got.url,
        login: got.login || "",
        passwordRules: got.passwordRules || "",
      },
      function (res) {
        if (res && res.ok) {
          window.close();
          return;
        }
        show('<div class="v-err">Generate canceled or failed.</div>');
      },
    );
  });
  return b;
}

function saveButton(got) {
  const isUpdate = !!(got && got.updateUUID);
  const b = veilUI.entryRow(
    {
      name: isUpdate ? "Update saved password" : "Save this sign-in",
      sub: isUpdate ? got.updateName : "",
      kind: isUpdate ? "update" : "save",
    },
    "button",
  );
  b.addEventListener("click", function () {
    chrome.runtime.sendMessage(
      {
        type: "popup-save",
        tabId: got.tabId,
        url: got.url,
        uuid: got.updateUUID || "",
      },
      function (res) {
        if (res && res.ok) {
          window.close();
          return;
        }
        show('<div class="v-err">Save canceled or failed.</div>');
      },
    );
  });
  return b;
}

// Empty search box: the site view — matched entries plus any save/generate
// offer, exactly like the popup has always drawn.
function renderSite() {
  clearList();
  const got = state.site || {};
  const entries = got.entries || [];
  const offerSave = !!got.canSave;
  if (!entries.length && !got.canGenerate && !offerSave) {
    state.list.appendChild(veilUI.el("div", "v-empty", "Nothing saved for this site — type above to search vault."));
    return;
  }
  if (offerSave) {
    track(saveButton(got));
  }
  entries.forEach(function (e) {
    track(entryButton(e, false));
  });
  if (got.canGenerate) {
    track(suggestButton(got));
  }
}

// Typed query: the whole vault, not just this site — name, login, saved-for
// host, and every URI match case-insensitively.
function renderSearch(q) {
  clearList();
  const terms = q
    .toLowerCase()
    .split(/\s+/)
    .filter(function (t) {
      return !!t;
    });
  const hits = state.catalog.filter(function (e) {
    if (!e) {
      return false;
    }
    const hay = [
      e.name || "",
      e.login || "",
      e.savedFor || "",
      e.kind || "",
      e.totp === "*" ? "2fa totp" : "",
      (e.uris || []).join(" "),
    ]
      .join(" ")
      .toLowerCase();
    for (let i = 0; i < terms.length; i++) {
      if (hay.indexOf(terms[i]) === -1) {
        return false;
      }
    }
    return true;
  });
  if (!hits.length) {
    state.list.appendChild(veilUI.el("div", "v-empty", "No matches in vault."));
    return;
  }
  hits.forEach(function (e) {
    track(entryButton(e, true));
  });
  // A query's top hit is the Enter target — arrows start from there.
  highlight(0);
}

Promise.all([send({ type: "popup-catalog" }), send({ type: "popup-list" })]).then(function (replies) {
  const catalog = replies[0];
  const got = replies[1];
  if (!got) {
    show('<div class="v-err">Host is not running. veil fill install</div>');
    return;
  }
  state.catalog = (catalog && catalog.entries) || [];
  state.site = got;
  root.textContent = "";
  const host = hostOf(got.url || "");
  const head = document.createElement("div");
  head.className = "v-head";
  const mark = document.createElement("div");
  mark.className = "v-mark";
  mark.innerHTML = veilUI.glyphs.veil + "<span>Veil</span>";
  head.appendChild(mark);
  const headRight = veilUI.el("div", "v-head-right");
  if (host) {
    const where = document.createElement("div");
    where.className = "v-where";
    where.textContent = host;
    headRight.appendChild(where);
  }
  headRight.appendChild(
    actionBtn("Vault ↗", "Open Veil Vault", function () {
      chrome.tabs.create({ url: "https://app.veil.nyc/items" });
      window.close();
    }),
  );
  head.appendChild(headRight);
  root.appendChild(head);

  const wrap = document.createElement("div");
  wrap.className = "v-search-wrap";
  const search = document.createElement("input");
  search.className = "v-search";
  search.type = "search";
  search.placeholder = "Search vault…";
  search.autocomplete = "off";
  search.spellcheck = false;
  wrap.appendChild(search);
  root.appendChild(wrap);

  const list = document.createElement("div");
  list.className = "v-list";
  state.list = list;
  root.appendChild(list);

  function rerender() {
    const q = String(search.value || "").trim();
    if (q) {
      renderSearch(q);
    } else {
      renderSite();
    }
  }

  search.addEventListener("input", rerender);
  search.addEventListener("keydown", function (ev) {
    if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
      ev.preventDefault();
      if (!state.rows.length) {
        return;
      }
      let next = state.active + (ev.key === "ArrowDown" ? 1 : -1);
      next = ((next % state.rows.length) + state.rows.length) % state.rows.length;
      highlight(next);
      return;
    }
    if (ev.key === "Enter") {
      ev.preventDefault();
      const i = state.active >= 0 ? state.active : 0;
      if (state.rows[i]) {
        state.rows[i].click();
      }
      return;
    }
    if (ev.key === "Escape" && search.value) {
      search.value = "";
      rerender();
    }
  });

  renderSite();
  search.focus();
});
