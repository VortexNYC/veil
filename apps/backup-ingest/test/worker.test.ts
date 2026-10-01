import assert from "node:assert/strict";
import { test } from "node:test";

import worker from "../src/index.ts";

// In-memory R2Bucket stub — the worker uses put (with conditional onlyIf),
// get, head, list, delete. Etags model R2's conditional-write semantics: a
// put carrying onlyIf.etagMatches commits only if the stored etag is
// unchanged; etagDoesNotMatch:"*" commits only if the key is absent.
class MemBucket {
	objects = new Map<string, { body: Uint8Array; size: number; etag: string }>();
	seq = 0;
	// Test hook — runs inside put() before the commit, so a test can
	// interleave a competing write between the worker's head and its put.
	onPut: ((key: string) => void) | null = null;

	async put(
		key: string,
		body: ArrayBuffer | Uint8Array | ReadableStream,
		opts?: { onlyIf?: { etagMatches?: string; etagDoesNotMatch?: string } },
	) {
		// onPut fires BEFORE the precondition is evaluated — it models a
		// peer's commit landing between our head and our commit, which is
		// exactly when R2 evaluates the conditional.
		this.onPut?.(key);
		const cur = this.objects.get(key);
		const cond = opts?.onlyIf;
		if (cond?.etagMatches !== undefined && cur?.etag !== cond.etagMatches) {
			return null; // precondition failed
		}
		if (cond?.etagDoesNotMatch === "*" && cur) {
			return null;
		}
		const bytes =
			body instanceof ReadableStream
				? new Uint8Array(await new Response(body).arrayBuffer())
				: body instanceof ArrayBuffer
					? new Uint8Array(body)
					: body;
		const etag = `e${++this.seq}`;
		this.objects.set(key, { body: bytes, size: bytes.byteLength, etag });
		return { key, size: bytes.byteLength, etag };
	}
	async get(key: string) {
		const o = this.objects.get(key);
		return o ? { body: o.body, size: o.size, etag: o.etag } : null;
	}
	async head(key: string) {
		const o = this.objects.get(key);
		return o ? { size: o.size, etag: o.etag } : null;
	}
	async delete(key: string) {
		this.objects.delete(key);
	}
	async list(opts: { prefix?: string; cursor?: string; limit?: number }) {
		const keys = [...this.objects.keys()]
			.filter((k) => k.startsWith(opts.prefix ?? ""))
			.sort();
		return {
			objects: keys.map((key) => ({ key, size: this.objects.get(key)!.size })),
			truncated: false,
		};
	}
}

const env = {
	BUCKET: new MemBucket(),
	OFFSITE_TOKEN: "test-token",
	DELETE_TOKEN: "delete-token",
};

function call(
	path: string,
	init: RequestInit = {},
	authed = true,
	token = "test-token",
) {
	const req = new Request(`https://x.test${path}`, init);
	if (authed) req.headers.set("Authorization", `Bearer ${token}`);
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	return worker.fetch(req, env as any);
}

const SEG = "wal-00000001000000000000000F";

test("rejects unauthenticated PUT/GET/HEAD/LIST", async () => {
	for (const init of [
		{ method: "PUT", body: "x" },
		{ method: "GET" },
		{ method: "HEAD" },
	]) {
		const res = await call(`/v1/arc-1/${SEG}`, init, false);
		assert.equal(res.status, 401);
	}
	const res = await call("/v1/?prefix=wal-", {}, false);
	assert.equal(res.status, 401);
});

test("PUT requires the arc-<sysid>/ namespace", async () => {
	for (const name of [
		`arc-7583066194673548362/${SEG}`,
		`arc-123/${SEG}.partial`,
		`arc-9/base-2026-10-01-0517.tar.gz`,
		`arc-9/veil-2026-10-01-0517.dump`,
		`arc-9/audit-20261001-051700-1-100.jsonl`,
		`arc-9/wal-00000001.history`,
	]) {
		const res = await call(`/v1/${name}`, {
			method: "PUT",
			body: "payload",
		});
		assert.equal(res.status, 200, name);
	}
	// Flat legacy names stay readable but can no longer be written —
	// new data only ever lands inside a cluster namespace.
	for (const name of [SEG, "base-2026-10-01-0517.tar.gz"]) {
		const res = await call(`/v1/${name}`, { method: "PUT", body: "x" });
		assert.equal(res.status, 400, name);
	}
});

test("rejects malformed and traversal names", async () => {
	for (const name of [
		"arc-x/wal-00000001000000000000000F", // non-numeric sysid
		"arc-/wal-00000001000000000000000F",
		"../etc/passwd",
		"wal-00000001000000000000000F.exe",
		"random.bin",
	]) {
		const res = await call(`/v1/${name}`, { method: "PUT", body: "x" });
		assert.equal(res.status, 404, name);
	}
	// URL parsing normalizes ".." before the name regex runs, so this lands
	// as a namespaced wal name — valid, authed, no escape. Assert that.
	const res = await call(
		"/v1/arc-1/../arc-2/wal-000000010000000000000001",
		{ method: "PUT", body: "x" },
	);
	assert.equal(res.status, 200);
	assert.ok(env.BUCKET.objects.has("arc-2/wal-000000010000000000000001"));
});

test("partial fencing: smaller rewrite is 409, larger succeeds", async () => {
	const key = `arc-1/${SEG}.partial`;
	const put = (n: number) =>
		call(`/v1/${key}`, { method: "PUT", body: new Uint8Array(n) });
	assert.equal((await put(100)).status, 200);
	assert.equal((await put(50)).status, 409);
	assert.equal(env.BUCKET.objects.get(key)!.size, 100); // untouched
	assert.equal((await put(150)).status, 200);
	assert.equal(env.BUCKET.objects.get(key)!.size, 150);
});

test("partial fencing holds under a concurrent write (CAS retry)", async () => {
	// Simulate the TOCTOU interleaving: our head sees nothing, then a peer
	// commits a LONGER partial before our put — the conditional write must
	// fail, the retry must see the bigger object, and we must 409 without
	// ever shrinking it.
	const key = `arc-2/${SEG}.partial`;
	env.BUCKET.onPut = (k) => {
		if (k === key) {
			env.BUCKET.objects.set(k, {
				body: new Uint8Array(500),
				size: 500,
				etag: "peer",
			});
		}
	};
	const res = await call(`/v1/${key}`, {
		method: "PUT",
		body: new Uint8Array(100),
	});
	env.BUCKET.onPut = null;
	assert.equal(res.status, 409);
	assert.equal(env.BUCKET.objects.get(key)!.size, 500); // never shrank
});

test("partial fencing: sustained contention surfaces 503, not a silent shrink", async () => {
	// A rival that ALWAYS moves the etag between our head and put means we
	// never commit — the shipper gets a retryable 503, never a false ok.
	const key = `arc-3/${SEG}.partial`;
	env.BUCKET.objects.set(key, {
		body: new Uint8Array(100),
		size: 100,
		etag: "v1",
	});
	env.BUCKET.onPut = (k) => {
		if (k === key) {
			const o = env.BUCKET.objects.get(k)!;
			env.BUCKET.objects.set(k, { ...o, etag: o.etag + "x" });
		}
	};
	const res = await call(`/v1/${key}`, {
		method: "PUT",
		body: new Uint8Array(200),
	});
	env.BUCKET.onPut = null;
	assert.equal(res.status, 503);
	assert.equal(env.BUCKET.objects.get(key)!.size, 100);
});

test("completed objects are immutable: same-size re-push ok, different-size 409", async () => {
	const key = `arc-7/${SEG}`;
	const put = (body: string, headers: Record<string, string> = {}) =>
		call(`/v1/${key}`, { method: "PUT", body, headers });
	// Real shippers (curl --data-binary) always send Content-Length — the
	// idempotent-retry path needs it to compare sizes.
	assert.equal((await put("abcd", { "Content-Length": "4" })).status, 200);
	// Idempotent retry — same artifact arriving twice (shipper restart).
	assert.equal((await put("wxyz", { "Content-Length": "4" })).status, 200);
	// A different-size write to a finished name is a collision, never an
	// overwrite — the stored bytes survive.
	assert.equal((await put("ab", { "Content-Length": "2" })).status, 409);
	// No Content-Length → size unverifiable → refuse rather than guess.
	assert.equal((await put("wxyz")).status, 409);
	assert.equal(new TextDecoder().decode(env.BUCKET.objects.get(key)!.body), "abcd");
});

test("two namespaces isolate identical wal names", async () => {
	await call(`/v1/arc-111/${SEG}`, { method: "PUT", body: "aaaa" });
	await call(`/v1/arc-222/${SEG}`, { method: "PUT", body: "bb" });
	const a = await env.BUCKET.get(`arc-111/${SEG}`);
	const b = await env.BUCKET.get(`arc-222/${SEG}`);
	assert.equal(a!.size, 4);
	assert.equal(b!.size, 2);
});

test("LIST accepts namespace prefixes, rejects bad prefixes", async () => {
	const res = await call("/v1/?prefix=arc-111/");
	assert.equal(res.status, 200);
	const { keys } = (await res.json()) as { keys: { key: string }[] };
	assert.deepEqual(
		keys.map((o) => o.key),
		[`arc-111/${SEG}`],
	);
	const bad = await call("/v1/?prefix=" + encodeURIComponent("arc-1/../"));
	assert.equal(bad.status, 400);
});

test("GET and HEAD serve namespaced objects", async () => {
	const get = await call(`/v1/arc-111/${SEG}`);
	assert.equal(get.status, 200);
	const head = await call(`/v1/arc-111/${SEG}`, { method: "HEAD" });
	assert.equal(head.headers.get("Content-Length"), "4");
});

test("DELETE requires the separate delete token, not the upload bearer", async () => {
	const key = `arc-1/${SEG}`;
	await call(`/v1/${key}`, { method: "PUT", body: "x" });
	// Upload bearer must NOT be able to erase the archive.
	assert.equal(
		(await call(`/v1/${key}`, { method: "DELETE" })).status,
		401,
	);
	assert.equal(
		(await call(`/v1/${key}`, { method: "DELETE" }, true, "delete-token"))
			.status,
		200,
	);
	assert.equal(env.BUCKET.objects.has(key), false);
	assert.equal(
		(await call(`/v1/${key}`, { method: "DELETE" }, true, "delete-token"))
			.status,
		404,
	);
});
