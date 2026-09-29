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
  const b = veilUI.entryRow({ name: "Save this sign-in", kind: "generate" }, "button");
  b.addEventListener("click", function () {
    chrome.runtime.sendMessage(
      {
        type: "popup-save",
        tabId: got.tabId,
        url: got.url,
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

chrome.runtime.sendMessage({ type: "popup-list" }, function (got) {
  if (chrome.runtime.lastError) {
    show('<div class="v-err">Host is not running. veil fill install</div>');
    return;
  }
  const entries = (got && got.entries) || [];
  const logins = entries.filter(function (e) {
    return e.kind === "login";
  });
  root.textContent = "";
  const host = hostOf((got && got.url) || "");
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
  const offerSave = !!(got && got.canSave && !logins.length);
  if (!entries.length && !(got && got.canGenerate) && !offerSave) {
    const empty = document.createElement("div");
    empty.className = "v-empty";
    empty.textContent = "Nothing saved for this site.";
    root.appendChild(empty);
    return;
  }
  if (offerSave) {
    root.appendChild(saveButton(got));
  }
  entries.forEach(function (e) {
    const b = veilUI.entryRow(
      { name: e.name || e.uuid || "item", sub: e.login || (e.kind && e.kind !== "login" ? e.kind : ""), kind: e.kind },
      "button",
    );
    b.addEventListener("click", function () {
      chrome.tabs.query({}, function (tabs) {
        const tab = globalThis.veilTab.tabByURL(tabs, got.url);
        chrome.runtime.sendMessage(
          { type: "popup-fill", uuid: e.uuid, url: got.url, tabId: tab && tab.id },
          function (res) {
            if (res && res.ok) {
              window.close();
              return;
            }
            show('<div class="v-err">Fill canceled or failed.</div>');
          },
        );
      });
    });
    root.appendChild(b);
  });
  if (got && got.canGenerate) {
    root.appendChild(suggestButton(got));
  }
});
