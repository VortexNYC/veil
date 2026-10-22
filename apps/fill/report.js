// veilReport — $exception capture to PostHog for every extension surface
// (background worker, content scripts, popup). Zero-dep: a single POST to
// the public ingest host. The project key is a write-only capture key —
// it ships in the client by design. No secrets, no item data, no URLs
// beyond the page hostname; a crash report must never leak a vault.
(function () {
  "use strict";
  var g = typeof self !== "undefined" ? self : globalThis;
  if (g.veilReport) {
    return;
  }
  var HOST = "https://us.i.posthog.com";
  var KEY = "phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8";
  var installId = null;

  function send(props) {
    try {
      var body = {
        api_key: KEY,
        event: "$exception",
        properties: Object.assign(
          { distinct_id: installId || "ext-anon", $lib: "veil-ext", $exception_type: props.type },
          props
        ),
      };
      fetch(HOST + "/capture/", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      }).catch(function () {});
    } catch {}
  }

  function stackOf(err) {
    var s = err && (err.stack || err.message);
    s = typeof s === "string" ? s : String(s);
    return s.length > 1200 ? s.slice(0, 1200) : s;
  }

  g.veilReport = {
    // A captured exception — message, first-stack, surface.
    error: function (surface, err, extra) {
      send(
        Object.assign(
          {
            $exception_message: (err && err.message) || String(err),
            $exception_stack_trace_raw: stackOf(err),
            surface: surface,
          },
          extra || {}
        )
      );
    },
    setInstallId: function (id) {
      installId = id;
    },
  };

  // The global traps — this is the whole point: uncaught errors in a
  // content script or event page are otherwise invisible.
  if (typeof g.addEventListener === "function") {
    g.addEventListener("error", function (ev) {
      g.veilReport.error("global", ev.error || ev.message);
    });
    g.addEventListener("unhandledrejection", function (ev) {
      g.veilReport.error("unhandled", ev.reason);
    });
  }
})();
