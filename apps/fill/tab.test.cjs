const { test } = require("node:test");
const assert = require("node:assert/strict");
const { usable, tabByURL, focusPlan } = require("./tab.js");

test("usable is http(s) only", () => {
  assert.equal(usable("http://127.0.0.1:8765/stripe-test-checkout.html"), true);
  assert.equal(usable("https://veil.nyc"), true);
  assert.equal(usable("chrome://extensions"), false);
  assert.equal(usable("chrome-extension://abc/popup.html"), false);
  assert.equal(usable(""), false);
});

test("tabByURL matches the chooser URL, not the popup or a search tab", () => {
  const checkout = "http://127.0.0.1:8765/stripe-test-checkout.html";
  const tabs = [
    { id: 1, url: "chrome-extension://abc/popup.html" },
    { id: 2, url: checkout },
    { id: 3, url: "https://www.google.com/search?q=" + checkout },
  ];
  const hit = tabByURL(tabs, checkout);
  assert.equal(hit && hit.id, 2);
  assert.equal(tabByURL(tabs, ""), null);
  assert.equal(tabByURL(tabs, "chrome://extensions"), null);
});

test("focusPlan shows the suggestion line for a single login", () => {
  // Even one unambiguous match is offered, not fired — the human sees which
  // credential it is (like 1Password's inline line) and picks to fill.
  const p = focusPlan([{ kind: "login", uuid: "u1" }], false);
  assert.equal(p.action, "menu");
  assert.equal(p.entries[0].uuid, "u1");
});

test("focusPlan opens the menu for multiple or non-login entries", () => {
  assert.equal(focusPlan([{ kind: "login", uuid: "a" }, { kind: "login", uuid: "b" }], false).action, "menu");
  assert.equal(focusPlan([{ kind: "card", uuid: "c" }], false).action, "menu");
  assert.equal(focusPlan([{ kind: "login", uuid: "a", affiliated: true }], false).action, "menu");
});

test("focusPlan is quiet with no data and menus generate fields", () => {
  assert.equal(focusPlan([], false).action, "quiet");
  assert.equal(focusPlan(null, false).action, "quiet");
  const g = focusPlan([], true);
  assert.equal(g.action, "menu");
  assert.equal(g.generate, true);
});

test("focusPlan generate rotates per matched login", () => {
  // change-password: existing logins are rotate targets, not a create. The
  // chooser offers "suggest new" per login so it never mints a second item.
  const entries = [
    { kind: "login", uuid: "l1", name: "stripe", login: "ada" },
    { kind: "card", uuid: "c1", name: "amex" },
    { kind: "login", uuid: "l2", name: "stripe", login: "work" },
  ];
  const p = focusPlan(entries, true);
  assert.equal(p.action, "menu");
  assert.equal(p.generate, true);
  assert.deepEqual(
    p.rotates.map(function (e) {
      return e.uuid;
    }),
    ["l1", "l2"],
  );
  // signup: nothing matched → generate creates, no rotate rows
  const signup = focusPlan([{ kind: "card", uuid: "c1" }], true);
  assert.equal(signup.generate, true);
  assert.deepEqual(signup.rotates, []);
  // a no-match focus stays quiet only when generate is off
  assert.equal(focusPlan([], false).action, "quiet");
});

test("focusPlan filters entries to the focused form context", () => {
  const entries = [
    { kind: "card", uuid: "c1" },
    { kind: "login", uuid: "l1" },
    { kind: "identity", uuid: "i1" },
  ];
  const login = focusPlan(entries, false, "login");
  assert.equal(login.action, "menu");
  assert.equal(login.entries[0].uuid, "l1");
  assert.equal(focusPlan(entries, false, "card").action, "menu");
  assert.equal(focusPlan(entries, false, "card").entries.length, 1);
  assert.equal(focusPlan(entries, false, "identity").entries[0].uuid, "i1");
  // a login form with no matching logins stays quiet — no card spam
  assert.equal(focusPlan([{ kind: "card" }], false, "login").action, "quiet");
});
