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

// Multipart upload — one request body can never exceed the edge's cap
// (~500MB), and daily kratos/base artifacts crossed it in Oct 2026. Big
// objects therefore arrive as parts, reassembled server-side by R2:
//   POST /v1/<name>?mpu=create                       -> { uploadId }
//   PUT  /v1/<name>?partNumber=<n>   (X-Mpu-Id: id)  -> { etag }
//   POST /v1/<name>?mpu=complete     (X-Mpu-Id: id)     body {parts:[{partNumber,etag}]}
//   POST /v1/<name>?mpu=abort        (X-Mpu-Id: id)
// The uploadId travels in a header, not the query string: it is opaque
// and need not be URL-safe. The same NAME grammar and arc- namespace
// apply — a part can never write a key the flat PUT could not.
// Parts are capped at 128MB so a stolen bearer cannot OOM the worker
// with a giant buffered part, and every part PUT re-checks head(name):
// the finished-key-exists fence holds for the whole upload, so mpu
// keeps the same put-if-absent rule as single PUT. The residual race —
// a peer committing between the last part and complete() — is the same
// TOCTOU class fencedPartialPut narrows for .partial; R2 offers no
// conditional complete, so the fence is best-effort, documented not
// silent.
const MPU_MAX_PART = 128 * 1024 * 1024;

async function mpuCreate(bucket: R2Bucket, name: string): Promise<Response> {
	if (await bucket.head(name)) {
		return new Response("conflict: object exists", { status: 409 });
	}
	const mpu = await bucket.createMultipartUpload(name);
	return Response.json({ uploadId: mpu.uploadId });
}

async function mpuPart(
	req: Request,
	bucket: R2Bucket,
	name: string,
	uploadId: string,
	partRaw: string | null,
): Promise<Response> {
	const n = Number(partRaw);
	if (!Number.isInteger(n) || n < 1 || n > 10000) {
		return new Response("bad partNumber", { status: 400 });
	}
	const cl = Number(req.headers.get("Content-Length") ?? NaN);
	if (!Number.isFinite(cl) || cl <= 0 || cl > MPU_MAX_PART) {
		return new Response("bad content-length", { status: 400 });
	}
	if (!req.body) {
		return new Response("empty body", { status: 400 });
	}
	if (await bucket.head(name)) {
		return new Response("conflict: object exists", { status: 409 });
	}
	try {
		const mpu = bucket.resumeMultipartUpload(name, uploadId);
		const part = await mpu.uploadPart(n, req.body);
		return Response.json({ partNumber: part.partNumber, etag: part.etag });
	} catch {
		return new Response("unknown uploadId", { status: 404 });
	}
}

async function mpuFinish(
	req: Request,
	bucket: R2Bucket,
	name: string,
	uploadId: string,
	action: string,
): Promise<Response> {
	if (action !== "abort" && action !== "complete") {
		return new Response("bad mpu action", { status: 400 });
	}
	let mpu: R2MultipartUpload;
	try {
		mpu = bucket.resumeMultipartUpload(name, uploadId);
	} catch {
		return new Response("unknown uploadId", { status: 404 });
	}
	if (action === "abort") {
		try {
			await mpu.abort();
		} catch {
			return new Response("unknown uploadId", { status: 404 });
		}
		return new Response("ok");
	}
	let parts: { partNumber: number; etag: string }[];
	try {
		const body = (await req.json()) as { parts?: unknown };
		if (!Array.isArray(body.parts)) throw new Error("parts");
		parts = body.parts.map((p) => {
			const o = p as { partNumber?: unknown; etag?: unknown };
			if (!Number.isInteger(o.partNumber) || (o.partNumber as number) < 1 || (o.partNumber as number) > 10000)
				throw new Error("partNumber");
			if (typeof o.etag !== "string" || o.etag.length === 0 || o.etag.length > 256)
				throw new Error("etag");
			return { partNumber: o.partNumber as number, etag: o.etag };
		});
	} catch {
		return new Response("bad parts body", { status: 400 });
	}
	if (await bucket.head(name)) {
		return new Response("conflict: object exists", { status: 409 });
	}
	try {
		await mpu.complete(parts);
	} catch {
		return new Response("unknown uploadId or bad parts", { status: 404 });
	}
	return new Response("ok");
}

// Flat wal-*/base-*/<db>-*.dump names are grandfathered for GET/HEAD/DELETE
// (pre-namespace objects still readable); new writes must be namespaced so a
// rebuilt cluster can never overwrite the archive of the cluster it replaced.
import { reportWorkerError } from "./report.ts";

const NAME = /^(arc-\d{1,20}\/)?([a-z0-9]+-\d{4}-\d{2}-\d{2}-\d{4}\.dump|audit-\d{8}-\d{6}-\d+-\d+\.jsonl|base-\d{4}-\d{2}-\d{2}-\d{4}\.(tar\.gz|manifest)|wal-[0-9A-F]{24}(?:\.partial|\.[0-9A-F]{8}\.backup)?|wal-[0-9A-F]{8}\.history)$/;

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
	async fetch(req: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
		try {
			return await this.handle(req, env);
		} catch (err) {
			reportWorkerError("backup-ingest", err, ctx, { path: new URL(req.url).pathname });
			throw err;
		}
	},

	async handle(req: Request, env: Env): Promise<Response> {
		const url = new URL(req.url);
		// Unauthenticated liveness for deploy smoke + the monitor — proves
		// the route AND the bucket binding are alive (list() exercises the
		// R2 binding; a detached binding throws, not 404s).
		if (url.pathname === "/healthz") {
			try {
				await env.BUCKET.list({ limit: 1 });
				return new Response("ok");
			} catch {
				return new Response("bucket unreachable", { status: 503 });
			}
		}
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
		if (req.method === "POST") {
			if (!authorized(req, env.OFFSITE_TOKEN)) {
				return new Response("unauthorized", { status: 401 });
			}
			if (!name.startsWith("arc-")) {
				return new Response("POST requires the arc-<sysid>/ namespace", { status: 400 });
			}
			const action = url.searchParams.get("mpu");
			if (action === "create") {
				return mpuCreate(env.BUCKET, name);
			}
			const uploadId = req.headers.get("x-mpu-id");
			if (!uploadId) {
				return new Response("missing x-mpu-id", { status: 400 });
			}
			if (action === "complete" || action === "abort") {
				return mpuFinish(req, env.BUCKET, name, uploadId, action);
			}
			return new Response("bad mpu action", { status: 400 });
		}
		if (req.method === "PUT") {
			if (!authorized(req, env.OFFSITE_TOKEN)) {
				return new Response("unauthorized", { status: 401 });
			}
			if (!name.startsWith("arc-")) {
				return new Response("PUT requires the arc-<sysid>/ namespace", { status: 400 });
			}
			const mpuId = req.headers.get("x-mpu-id");
			if (mpuId !== null || url.searchParams.get("partNumber") !== null) {
				if (!mpuId) {
					return new Response("missing x-mpu-id", { status: 400 });
				}
				return mpuPart(req, env.BUCKET, name, mpuId, url.searchParams.get("partNumber"));
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
			if (req.method === "HEAD") {
				const meta = await env.BUCKET.head(name);
				if (!meta) {
					return new Response("not found", { status: 404 });
				}
				return new Response(null, {
					headers: { "content-length": String(meta.size), etag: meta.etag },
				});
			}
			const obj = await env.BUCKET.get(name);
			if (!obj) {
				return new Response("not found", { status: 404 });
			}
			return new Response(obj.body, {
				headers: { "content-length": String(obj.size), etag: obj.etag },
			});
		}
		return new Response("method not allowed", { status: 405 });
	},
} satisfies ExportedHandler<Env>;
