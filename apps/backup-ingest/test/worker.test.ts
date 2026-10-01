import assert from "node:assert/strict";
import { test } from "node:test";

import worker from "../src/index.ts";

// In-memory R2Bucket stub — the worker only uses put/get/head/list.
class MemBucket {
	objects = new Map<string, { body: Uint8Array; size: number }>();
	async put(key: string, body: ArrayBuffer | Uint8Array) {
		const bytes = body instanceof ArrayBuffer ? new Uint8Array(body) : body;
		this.objects.set(key, { body: bytes, size: bytes.byteLength });
	}
	async get(key: string) {
		const o = this.objects.get(key);
		return o ? { body: o.body, size: o.size } : null;
	}
	async head(key: string) {
		const o = this.objects.get(key);
		return o ? { size: o.size } : null;
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

const env = { BUCKET: new MemBucket(), INGEST_TOKEN: "test-token" };

function call(path: string, init: RequestInit = {}, authed = true) {
	const req = new Request(`https://x.test${path}`, init);
	if (authed) req.headers.set("Authorization", "Bearer test-token");
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
		const res = await call(`/v1/${SEG}`, init, false);
		assert.equal(res.status, 401);
	}
	const res = await call("/v1/?prefix=wal-", {}, false);
	assert.equal(res.status, 401);
});

test("accepts namespaced and unnamespaced archive names", async () => {
	for (const name of [
		`arc-7583066194673548362/${SEG}`,
		`arc-123/${SEG}.partial`,
		`arc-9/base-2026-10-01-0517.tar.gz`,
		`arc-9/veil-2026-10-01-0517.dump`,
		`arc-9/wal-00000001.history`,
		SEG, // legacy flat names stay valid
	]) {
		const res = await call(`/v1/${name}`, {
			method: "PUT",
			body: "payload",
		});
		assert.equal(res.status, 200, name);
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
	// as the flat wal name — valid, authed, no escape. Assert that result.
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
