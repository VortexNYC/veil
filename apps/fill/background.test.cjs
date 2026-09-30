// hostSend reply correlation: the port is strictly request/response, but a
// reply can arrive after its browser-side timeout — the waiter is already
// gone and a FIFO shift would hand that stale reply to the next request.
// Replies must match by reqId; a reply naming a dead waiter is dropped.
const { test } = require("node:test");
const assert = require("node:assert/strict");

const posted = [];
const port = {
  onMessage: { addListener(fn) { port.onMsg = fn; } },
  onDisconnect: { addListener(fn) { port.onGone = fn; } },
  postMessage(m) { posted.push(m); },
};

global.chrome = {
  runtime: {
    connectNative: () => port,
    onInstalled: { addListener() {} },
    onStartup: { addListener() {} },
    onConnect: { addListener() {} },
    onMessage: { addListener() {} },
  },
  tabs: {
    onUpdated: { addListener() {} },
    onActivated: { addListener() {} },
    onRemoved: { addListener() {} },
  },
  commands: { onCommand: { addListener() {} } },
};
globalThis.veilTab = { usable: () => true };

const bg = require("./background.js");

const tick = () => new Promise((r) => setTimeout(r, 0));

test("requests carry a unique reqId", async () => {
  posted.length = 0;
  const a = bg.hostSend({ action: "ping" }, 500);
  const b = bg.hostSend({ action: "match", url: "https://x" }, 500);
  await tick();
  assert.equal(posted.length, 2);
  assert.ok(posted[0].reqId);
  assert.ok(posted[1].reqId);
  assert.notEqual(posted[0].reqId, posted[1].reqId);
  port.onMsg({ reqId: posted[1].reqId, entries: [] });
  port.onMsg({ reqId: posted[0].reqId, version: "1" });
  assert.deepEqual(await a, { reqId: posted[0].reqId, version: "1" });
  assert.deepEqual(await b, { reqId: posted[1].reqId, entries: [] });
});

test("out-of-order replies resolve their own waiters", async () => {
  posted.length = 0;
  const a = bg.hostSend({ action: "passkeyGet" }, 1000);
  const b = bg.hostSend({ action: "passkeyCreate" }, 1000);
  await tick();
  // The create reply lands first — FIFO would wrongly resolve the get waiter.
  port.onMsg({ reqId: posted[1].reqId, response: { id: "c" } });
  const got = await b;
  assert.equal(got.response.id, "c");
  port.onMsg({ reqId: posted[0].reqId, response: { id: "g" } });
  assert.equal((await a).response.id, "g");
});

test("a reply naming a dead waiter is dropped, not shifted onto the next", async () => {
  posted.length = 0;
  // Times out before its reply arrives — simulates a slow confirm.
  const stale = bg.hostSend({ action: "passkeyGet" }, 20);
  const create = bg.hostSend({ action: "passkeyCreate" }, 1000);
  await assert.rejects(stale, /host timeout/);
  // The stale get reply arrives late — under FIFO it would steal create's
  // waiter and hand the page an assertion credential on a create call.
  port.onMsg({
    reqId: posted[0].reqId,
    response: { id: "stale-get", response: { clientDataJSON: "e30" } },
  });
  await tick();
  port.onMsg({ reqId: posted[1].reqId, response: { id: "create-ok" } });
  assert.equal((await create).response.id, "create-ok");
});

test("replies with no reqId still work FIFO (older host)", async () => {
  posted.length = 0;
  const a = bg.hostSend({ action: "ping" }, 1000);
  const b = bg.hostSend({ action: "ping" }, 1000);
  await tick();
  port.onMsg({ version: "first" });
  port.onMsg({ version: "second" });
  assert.equal((await a).version, "first");
  assert.equal((await b).version, "second");
});

test("disconnect rejects every waiter", async () => {
  posted.length = 0;
  const a = bg.hostSend({ action: "ping" }, 5000);
  const b = bg.hostSend({ action: "ping" }, 5000);
  await tick();
  port.onGone();
  await assert.rejects(a);
  await assert.rejects(b);
});
