// Chooser URL → tab. The action popup is its own Chrome window;
// currentWindow / lastFocusedWindow are not the checkout tab.
(function (root) {
  function usable(url) {
    return typeof url === "string" && (url.startsWith("https://") || url.startsWith("http://"));
  }
  function tabByURL(tabs, url) {
    if (!usable(url) || !tabs) {
      return null;
    }
    for (let i = 0; i < tabs.length; i++) {
      if (tabs[i] && tabs[i].url === url) {
        return tabs[i];
      }
    }
    return null;
  }
  // Trusted field focus: any match shows the inline suggestion under the
  // field — the human sees which credential is offered and picks it (click
  // or Enter) like the native autofill line. No match → silence. context
  // scopes offers to what the focused form can take — a sign-in page gets
  // logins, never a card pile.
  function focusPlan(entries, generate, context) {
    let list = entries || [];
    if (context === "login" || context === "card" || context === "identity") {
      list = list.filter(function (e) {
        return e && e.kind === context;
      });
    }
    if (generate) {
      // Change-password: each matched login is a "suggest new" rotate target —
      // generate on an existing uuid replaces that password, never a create.
      const rotates = list.filter(function (e) {
        return e && e.kind === "login" && !e.affiliated;
      });
      return { action: "menu", entries: list, generate: true, rotates: rotates };
    }
    if (list.length) {
      return { action: "menu", entries: list };
    }
    return { action: "quiet" };
  }
  root.veilTab = { usable: usable, tabByURL: tabByURL, focusPlan: focusPlan };
  if (typeof module !== "undefined" && module.exports) {
    module.exports = root.veilTab;
  }
})(typeof globalThis !== "undefined" ? globalThis : this);
