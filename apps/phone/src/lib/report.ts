// report.ts — $exception capture to PostHog for the RN layer.
// The same shape as apps/fill/report.js: a plain capture POST, no SDK,
// no native module — JS-side errors only. Native crashes (the appex,
// a Swift fault) need the full posthog-react-native build later.
// distinct_id is an anonymous install id in AsyncStorage-free memory —
// regenerated per launch; grouping matters, not identity.

const HOST = "https://us.i.posthog.com/capture/";
const KEY = "phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8";

let installId = `phone-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;

function stackOf(err: unknown): string {
  const s = err instanceof Error ? err.stack ?? err.message : String(err);
  return s.length > 1200 ? s.slice(0, 1200) : s;
}

function send(type: string, message: string, stack: string, extra?: Record<string, unknown>) {
  try {
    fetch(HOST, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        api_key: KEY,
        event: "$exception",
        properties: {
          distinct_id: installId,
          $lib: "veil-phone",
          $exception_type: type,
          $exception_message: message,
          $exception_stack_trace_raw: stack,
          surface: "phone",
          ...extra,
        },
      }),
    }).catch(() => {});
  } catch {}
}

export const report = {
  error(err: unknown, extra?: Record<string, unknown>) {
    send(
      "error",
      err instanceof Error ? err.message : String(err),
      stackOf(err),
      extra
    );
  },
  fatal(err: unknown) {
    send("fatal", err instanceof Error ? err.message : String(err), stackOf(err));
  },
};

let armed = false;

// Hook the global JS error handler once — any uncaught render/fetch/logic
// fault becomes an $exception. The previous handler still runs so RN's
// red-box keeps working in dev.
export function armErrorReporting() {
  if (armed) return;
  armed = true;
  const g = globalThis as unknown as {
    ErrorUtils?: {
      getGlobalHandler?: () => (err: Error, isFatal?: boolean) => void;
      setGlobalHandler?: (fn: (err: Error, isFatal?: boolean) => void) => void;
    };
  };
  const eu = g.ErrorUtils;
  if (!eu?.getGlobalHandler || !eu.setGlobalHandler) return;
  const prev = eu.getGlobalHandler();
  eu.setGlobalHandler((err, isFatal) => {
    if (isFatal) {
      report.fatal(err);
    } else {
      report.error(err);
    }
    if (typeof prev === "function") prev(err, isFatal);
  });
}
