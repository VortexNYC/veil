// Controlled upstream for /v1/use load tests. The origin fetches this
// server-side with the item's credential injected — we return a fixed 200
// and deliberately do NOT echo request headers so injected secrets can never
// reflect back into a response body or a log line.
export default {
	async fetch(): Promise<Response> {
		return Response.json({ ok: true });
	},
} satisfies ExportedHandler;
