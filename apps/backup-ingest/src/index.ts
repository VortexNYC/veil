interface Env {
	BUCKET: R2Bucket;
	INGEST_TOKEN: string;
}

// Names are deliberately narrow: db dumps are <db>-YYYY-MM-DD-HHMM.dump,
// the audit archive is audit-YYYYMMDD-HHMMSS-<firstId>-<lastId>.jsonl,
// base backups are base-YYYY-MM-DD-HHMM.tar.gz, and WAL archiving lands
// wal-<24-hex-segment> (plus .partial in-flight, .<lsn-offset>.backup
// labels, wal-<8-hex>.history timelines). Per-cluster namespacing puts WAL
// and bases under arc-<pg system_identifier>/ so a rebuilt cluster (new
// sysid, timeline 1, LSN 0) can never overwrite the archive of the cluster
// it is replacing. Nothing else lands in the bucket, and a crafted name
// can't write outside the prefixes.
const NAME = /^(arc-\d{1,20}\/)?([a-z0-9]+-\d{4}-\d{2}-\d{2}-\d{4}\.dump|audit-\d{8}-\d{6}-\d+-\d+\.jsonl|base-\d{4}-\d{2}-\d{2}-\d{4}\.tar\.gz|wal-[0-9A-F]{24}(?:\.partial|\.[0-9A-F]{8}\.backup)?|wal-[0-9A-F]{8}\.history)$/;

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
			if (!/^[A-Za-z0-9._/-]{0,96}$/.test(prefix) || prefix.includes("..")) {
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
		const m = url.pathname.match(/^\/v1\/([A-Za-z0-9._/-]+)$/);
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
				const body = await req.arrayBuffer();
				// WAL .partial files are append-only — same bytes, only the
				// tail grows. With two archivers (primary + standby) a slower
				// writer must never overwrite a longer copy, so PUT is fenced:
				// a .partial smaller than what is already stored is rejected.
				// Complete segments are byte-identical regardless of which
				// receiver produced them, so they overwrite freely.
				if (key.endsWith(".partial")) {
					const head = await env.BUCKET.head(key);
					if (head && head.size > body.byteLength) {
						return new Response("conflict: stored partial is newer", {
							status: 409,
						});
					}
				}
				await env.BUCKET.put(key, body, {
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
			case "DELETE": {
				const head = await env.BUCKET.head(key);
				if (!head) {
					return new Response("not found", { status: 404 });
				}
				await env.BUCKET.delete(key);
				return new Response(JSON.stringify({ deleted: key }), {
					headers: { "Content-Type": "application/json" },
				});
			}
			default:
				return new Response("method not allowed", { status: 405 });
		}
	},
} satisfies ExportedHandler<Env>;
