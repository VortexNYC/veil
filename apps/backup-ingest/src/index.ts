interface Env {
	BUCKET: R2Bucket;
	INGEST_TOKEN: string;
}

// Names are deliberately narrow: db dumps are <db>-YYYY-MM-DD-HHMM.dump,
// the audit archive is audit-YYYYMMDD-HHMMSS-<firstId>-<lastId>.jsonl,
// base backups are base-YYYY-MM-DD-HHMM.tar.gz, and WAL archiving lands
// wal-<24-hex-segment> (plus .partial in-flight, .<lsn-offset>.backup
// labels, wal-<8-hex>.history timelines). Nothing else lands in the bucket,
// and a crafted name can't write outside the prefixes.
const NAME = /^([a-z0-9]+-\d{4}-\d{2}-\d{2}-\d{4}\.dump|audit-\d{8}-\d{6}-\d+-\d+\.jsonl|base-\d{4}-\d{2}-\d{2}-\d{4}\.tar\.gz|wal-[0-9A-F]{24}(?:\.partial|\.[0-9A-F]{8}\.backup)?|wal-[0-9A-F]{8}\.history)$/;

function authed(req: Request, env: Env): boolean {
	return req.headers.get("Authorization") === `Bearer ${env.INGEST_TOKEN}`;
}

export default {
	async fetch(req: Request, env: Env): Promise<Response> {
		const url = new URL(req.url);
		// GET /v1/?prefix=wal- lists key names (+ sizes) so a restore can
		// enumerate the archive without guessing. Names only — bodies still
		// go through GET /v1/<name>. Bearer-gated like everything else.
		if (url.pathname === "/v1/" || url.pathname === "/v1") {
			if (req.method !== "GET") {
				return new Response("method not allowed", { status: 405 });
			}
			if (!authed(req, env)) {
				return new Response("unauthorized", { status: 401 });
			}
			const prefix = url.searchParams.get("prefix") ?? "";
			if (!/^[A-Za-z0-9._-]{0,64}$/.test(prefix)) {
				return new Response("bad prefix", { status: 400 });
			}
			const cursor = url.searchParams.get("cursor") ?? undefined;
			const page = await env.BUCKET.list({ prefix, cursor, limit: 1000 });
			return new Response(
				JSON.stringify({
					keys: page.objects.map((o) => ({ key: o.key, size: o.size })),
					cursor: page.truncated ? page.cursor : null,
				}),
				{ headers: { "Content-Type": "application/json" } },
			);
		}
		const m = url.pathname.match(/^\/v1\/([A-Za-z0-9._-]+)$/);
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
