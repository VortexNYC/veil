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

function entryButton(e, fromSearch) {
  const b = veilUI.entryRow(
    { name: e.name || e.uuid || "item", sub: e.login || (e.kind && e.kind !== "login" ? e.kind : ""), kind: e.kind, icon: e.icon },
    "button",
  );
  b.addEventListener("click", function () {
    pick(e, fromSearch);
  });
  // Item metadata (login/URIs/tags) edits in place — the pencil beside the
  // row opens the form; the secret never leaves the rotate path.
  const wrap = document.createElement("div");
  wrap.className = "v-editrow";
  wrap.appendChild(b);
  const edit = document.createElement("button");
  edit.className = "v-edit";
  edit.type = "button";
  edit.title = "Edit " + (e.name || "item");
  edit.textContent = "✎";
  edit.addEventListener("click", function (ev) {
    ev.stopPropagation();
    openEdit(e);
  });
  wrap.appendChild(edit);
  return wrap;
}

// The edit form covers metadata only — name is the vault's lookup key and
// the secret rotates through generate/save, never through this surface.
function openEdit(e) {
  clearList();
  const form = document.createElement("form");
  form.className = "v-editform";
  const nameWrap = document.createElement("div");
  nameWrap.appendChild(veilUI.el("div", "v-label", "Name"));
  nameWrap.appendChild(veilUI.el("div", "v-name", e.name || e.uuid || "item"));
  form.appendChild(nameWrap);
  function field(label, id, value) {
    const w = document.createElement("div");
    const l = veilUI.el("div", "v-label", label);
    const inp = document.createElement("input");
    inp.className = "v-inp";
    inp.name = id;
    inp.value = value || "";
    inp.autocomplete = "off";
    w.appendChild(l);
    w.appendChild(inp);
    return w;
  }
  form.appendChild(field("Username", "login", e.login));
  form.appendChild(field("URIs (comma-separated)", "uris", (e.uris || []).join(", ")));
  form.appendChild(field("Tags (comma-separated)", "tags", (e.tags || []).join(", ")));
  const actions = document.createElement("div");
  actions.className = "v-actions";
  const cancel = document.createElement("button");
  cancel.className = "v-btn";
  cancel.type = "button";
  cancel.textContent = "Cancel";
  cancel.addEventListener("click", function () {
    const s = document.querySelector(".v-search");
    if (s && s.value.trim()) {
      renderSearch(s.value.trim());
    } else {
      renderSite();
    }
  });
  const save = document.createElement("button");
  save.className = "v-btn v-primary";
  save.type = "submit";
  save.textContent = "Save";
  actions.appendChild(cancel);
  actions.appendChild(save);
  form.appendChild(actions);
  form.addEventListener("submit", function (ev) {
    ev.preventDefault();
    const list = function (v) {
      return String(v || "")
        .split(",")
        .map(function (s) {
          return s.trim();
        })
        .filter(Boolean);
    };
    save.disabled = true;
    chrome.runtime.sendMessage(
      {
        type: "popup-update",
        uuid: e.uuid,
        login: form.elements.login.value.trim(),
        uris: list(form.elements.uris.value),
        tags: list(form.elements.tags.value),
      },
      function (res) {
        if (res && res.ok) {
          window.close();
          return;
        }
        save.disabled = false;
        show('<div class="v-err">Edit canceled or failed.</div>');
      },
    );
  });
  state.list.appendChild(form);
  const first = form.elements.login;
  if (first && typeof first.focus === "function") {
    first.focus();
  }
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
  const ql = q.toLowerCase();
  const hits = state.catalog.filter(function (e) {
    if (!e) {
      return false;
    }
    if (String(e.name || "").toLowerCase().indexOf(ql) !== -1) {
      return true;
    }
    if (String(e.login || "").toLowerCase().indexOf(ql) !== -1) {
      return true;
    }
    if (String(e.savedFor || "").toLowerCase().indexOf(ql) !== -1) {
      return true;
    }
    const uris = e.uris || [];
    for (let i = 0; i < uris.length; i++) {
      if (String(uris[i] || "").toLowerCase().indexOf(ql) !== -1) {
        return true;
      }
    }
    return false;
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
  if (host) {
    const where = document.createElement("div");
    where.className = "v-where";
    where.textContent = host;
    head.appendChild(where);
  }
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
