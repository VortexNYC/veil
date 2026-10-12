// report.ts — $exception capture to PostHog for worker faults.
// Same shape as the extension/phone reporters: one capture POST, no
// SDK, no secrets — path + surface only. ctx.waitUntil keeps the send
// off the request path; a reporting failure must never fail the worker.
const HOST = "https://us.i.posthog.com/capture/";
const KEY = "phc_v5HaYTE3N9siA53wtYmkFHSm28mBATJJsdDjeaobHxC8";

export function reportWorkerError(
	surface: string,
	err: unknown,
	ctx: { waitUntil(p: Promise<unknown>): void },
	extra?: Record<string, unknown>,
): void {
	try {
		const msg = err instanceof Error ? err.message : String(err);
		const stack = err instanceof Error ? (err.stack ?? "") : "";
		const body = JSON.stringify({
			api_key: KEY,
			event: "$exception",
			properties: {
				distinct_id: "worker-" + surface,
				$lib: "veil-worker",
				$exception_type: "worker_error",
				$exception_message: msg,
				$exception_stack_trace_raw: stack.slice(0, 1200),
				surface,
				...extra,
			},
		});
		ctx.waitUntil(
			fetch(HOST, {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body,
			}).catch(() => {}),
		);
	} catch {}
}
