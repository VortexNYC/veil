interface Env {
	BUCKET: R2Bucket;
	INGEST_TOKEN: string;
}

// Names are deliberately narrow: db dumps are <db>-YYYY-MM-DD-HHMM.dump and
// the audit archive is audit-YYYYMMDD-HHMMSS-<firstId>-<lastId>.jsonl.
// Nothing else lands in the bucket, and a crafted name can't write outside
// the prefixes.
const NAME = /^([a-z0-9]+-\d{4}-\d{2}-\d{2}-\d{4}\.dump|audit-\d{8}-\d{6}-\d+-\d+\.jsonl)$/;

function authed(req: Request, env: Env): boolean {
	return req.headers.get("Authorization") === `Bearer ${env.INGEST_TOKEN}`;
}

export default {
	async fetch(req: Request, env: Env): Promise<Response> {
		const url = new URL(req.url);
		const m = url.pathname.match(/^\/v1\/([a-z0-9.-]+\.(?:dump|jsonl))$/);
		if (!m || !NAME.test(m[1])) {
			return new Response("not found", { status: 404 });
		}
		if (!authed(req, env)) {
			return new Response("unauthorized", { status: 401 });
		}
		const key = m[1];
		switch (req.method) {
			case "PUT": {
				if (!req.body) {
					return new Response("empty body", { status: 400 });
				}
				await env.BUCKET.put(key, req.body, {
					customMetadata: { uploaded: new Date().toISOString() },
				});
				return new Response(JSON.stringify({ stored: key }), {
					headers: { "Content-Type": "application/json" },
				});
			}
			case "GET": {
				const obj = await env.BUCKET.get(key);
				if (!obj) {
					return new Response("not found", { status: 404 });
				}
				return new Response(obj.body, {
					headers: { "Content-Type": "application/octet-stream" },
				});
			}
			case "HEAD": {
				const head = await env.BUCKET.head(key);
				if (!head) {
					return new Response("not found", { status: 404 });
				}
				return new Response(null, {
					headers: { "Content-Length": String(head.size) },
				});
			}
			default:
				return new Response("method not allowed", { status: 405 });
		}
	},
} satisfies ExportedHandler<Env>;
