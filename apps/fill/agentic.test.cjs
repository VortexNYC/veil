"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");

test("veilAgentic exposes Browserbase-shaped namespaces", function () {
  globalThis.hostSend = function () {
    return Promise.resolve({ version: "1" });
  };
  globalThis.usable = function () {
    return true;
  };
  globalThis.needLogin = function () {
    return false;
  };
  globalThis.primeWrite = async function () {};
  globalThis.writeTab = async function () {
    return true;
  };
  globalThis.chrome = {
    tabs: {
      get: async function () {
        return { id: 1, url: "https://example.com/login" };
      },
    },
  };
  require("./agentic.js");
  assert.ok(globalThis.veilAgentic);
  assert.ok(globalThis.veilAgentic.v1.initialization.whenSettled);
  assert.ok(globalThis.veilAgentic.v1.agenticMode.enable);
  assert.ok(globalThis.veilAgentic.v1.autofill.fillCredential);
});
