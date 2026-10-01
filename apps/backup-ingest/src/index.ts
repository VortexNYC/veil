// backup-ingest — minimal R2 ingest for offsite Postgres dumps/WAL.
// Auth: Bearer OFFSITE_TOKEN. Routes:
//   PUT    /v1/<name>   store object (namespace arc-<sysid>/...)
//   DELETE /v1/<name>   remove object  (Bearer DELETE_TOKEN — separate secret;
//                        the upload bearer must not be able to erase archives)
//   GET    /v1/<name>   fetch object   (RESTORE_TOKEN or OFFSITE_TOKEN)
//   HEAD   /v1/<name>   object metadata
//   GET    /v1?prefix=  list keys under a prefix (restore flow discovery)
// Names are restricted to a narrow grammar — no traversal, no arbitrary keys.

interface Env {
	BUCKET: R2Bucket;
	OFFSITE_TOKEN: string;
	RESTORE_TOKEN?: string;
	DELETE_TOKEN?: string;
}

// Flat wal-*/base-*/<db>-*.dump names are grandfathered for GET/HEAD/DELETE
// (pre-namespace objects still readable); new writes must be namespaced so a
// rebuilt cluster can never overwrite the archive of the cluster it replaced.
const NAME = /^(arc-\d{1,20}\/)?([a-z0-9]+-\d{4}-\d{2}-\d{2}-\d{4}\.dump|audit-\d{8}-\d{6}-\d+-\d+\.jsonl|base-\d{4}-\d{2}-\d{2}-\d{4}\.tar\.gz|wal-[0-9A-F]{24}(?:\.partial|\.[0-9A-F]{8}\.backup)?|wal-[0-9A-F]{8}\.history)$/;

function authorized(req: Request, token: string | undefined): boolean {
	if (!token) return false;
	const auth = req.headers.get("Authorization") ?? "";
	if (!auth.startsWith("Bearer ")) return false;
	const got = auth.slice(7);
	if (got.length !== token.length) return false;
	let diff = 0;
	for (let i = 0; i < got.length; i++) diff |= got.charCodeAt(i) ^ token.charCodeAt(i);
	return diff === 0;
}

// fencedPartialPut enforces the WAL monotonic rule — a .partial must never
// shrink the archive — under CONCURRENT writers. HEAD-then-PUT has a TOCTOU
// gap (two writers both pass the size check, the shorter lands last); R2
// conditional writes close it: put-if-etag-unchanged, retry on the etag
// having moved. 409 = a longer copy already archived; 503 = keep losing the
// race, shipper will retry.
async function fencedPartialPut(bucket: R2Bucket, key: string, body: ArrayBuffer): Promise<Response> {
	for (let i = 0; i < 4; i++) {
		const cur = await bucket.head(key);
		if (cur && cur.size > body.byteLength) {
			return new Response("partial regression refused: " + cur.size + " > " + body.byteLength, { status: 409 });
		}
		const opts = cur
			? { onlyIf: { etagMatches: cur.etag } }
			: { onlyIf: { etagDoesNotMatch: "*" } };
		const r = await bucket.put(key, body, opts);
		if (r) return new Response("ok");
	}
	return new Response("partial write contended", { status: 503, headers: { "Retry-After": "1" } });
}

export default {
	async fetch(req: Request, env: Env): Promise<Response> {
		const url = new URL(req.url);
		if (req.method === "GET" && (url.pathname === "/v1" || url.pathname === "/v1/")) {
			if (!authorized(req, env.OFFSITE_TOKEN)) {
				return new Response("unauthorized", { status: 401 });
			}
			const prefix = url.searchParams.get("prefix") ?? "";
			if (prefix.includes("..")) {
				return new Response("bad prefix", { status: 400 });
			}
			// One page per request — the caller loops on `cursor`. Draining
			// every page into memory here lets an authed client OOM the
			// worker on a big bucket.
			const cursor = url.searchParams.get("cursor") ?? undefined;
			const page = await env.BUCKET.list({ prefix, cursor, limit: 1000 });
			return Response.json({
				keys: page.objects.map((o) => ({ key: o.key, size: o.size })),
				cursor: page.truncated ? page.cursor : undefined,
			});
		}
		const m = url.pathname.match(/^\/v1\/(.+)$/);
		if (!m || !NAME.test(m[1])) {
			return new Response("not found", { status: 404 });
		}
		const name = m[1];
		if (req.method === "PUT") {
			if (!authorized(req, env.OFFSITE_TOKEN)) {
				return new Response("unauthorized", { status: 401 });
			}
			if (!name.startsWith("arc-")) {
				return new Response("PUT requires the arc-<sysid>/ namespace", { status: 400 });
			}
			if (name.endsWith(".partial")) {
				// The size fence needs the whole body in memory — bound it.
				// A .partial is a prefix of a 16MB WAL segment; anything
				// bigger is hostile or corrupt, and a stolen bearer can't
				// OOM the worker with an unbounded upload.
				const max = 16 * 1024 * 1024;
				const cl = Number(req.headers.get("Content-Length") ?? NaN);
				if (!Number.isFinite(cl) || cl < 0 || cl > max) {
					return new Response("bad content-length", { status: 400 });
				}
				const body = await req.arrayBuffer();
				if (body.byteLength > max) {
					return new Response("partial exceeds 16MB segment", { status: 413 });
				}
				return fencedPartialPut(env.BUCKET, name, body);
			}
			if (!req.body) {
				return new Response("empty body", { status: 400 });
			}
			// Non-partial objects are immutable-by-content (completed WAL,
			// dated dumps): put-if-absent only, so a stolen upload bearer
			// cannot clobber a finished artifact. A same-size re-push is an
			// idempotent retry → 200; a different-size collision → 409.
			const len = Number(req.headers.get("Content-Length") ?? -1);
			const r = await env.BUCKET.put(name, req.body, {
				onlyIf: { etagDoesNotMatch: "*" },
			});
			if (r) return new Response("ok");
			const cur = await env.BUCKET.head(name);
			if (cur && cur.size === len) return new Response("ok");
			return new Response("conflict: object exists", { status: 409 });
		}
		if (req.method === "DELETE") {
			if (!authorized(req, env.DELETE_TOKEN)) {
				return new Response("unauthorized", { status: 401 });
			}
			const obj = await env.BUCKET.head(name);
			if (!obj) {
				return new Response("not found", { status: 404 });
			}
			await env.BUCKET.delete(name);
			return new Response("ok");
		}
		if (req.method === "GET" || req.method === "HEAD") {
			if (!authorized(req, env.RESTORE_TOKEN) && !authorized(req, env.OFFSITE_TOKEN)) {
				return new Response("unauthorized", { status: 401 });
			}
			const obj = await env.BUCKET.get(name);
			if (!obj) {
				return new Response("not found", { status: 404 });
			}
			if (req.method === "HEAD") {
				return new Response(null, {
					headers: { "content-length": String(obj.size), etag: obj.etag },
				});
			}
			return new Response(obj.body, {
				headers: { "content-length": String(obj.size), etag: obj.etag },
			});
		}
		return new Response("method not allowed", { status: 405 });
	},
} satisfies ExportedHandler<Env>;
