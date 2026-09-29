const { test } = require("node:test");
const assert = require("node:assert/strict");
const { pickFields, canSave, findOTPAuth } = require("./fields.js");

function el(partial) {
  const node = Object.assign({ autocomplete: "", type: "text", name: "", id: "", hidden: false, disabled: false }, partial);
  node.getAttribute = function (name) {
    if (name === "autocomplete") {
      return this.autocomplete || "";
    }
    if (name === "data-autocomplete") {
      return this["data-autocomplete"] || "";
    }
    return "";
  };
  return node;
}

test("username and current-password from autocomplete", () => {
  const got = pickFields([
    el({ autocomplete: "username", name: "u" }),
    el({ autocomplete: "current-password", type: "password", name: "p" }),
  ]);
  assert.equal(got.username.name, "u");
  assert.equal(got.password.name, "p");
  assert.equal(got.totp, null);
});

test("data-autocomplete new-password is generate", () => {
  const np = el({ autocomplete: "", "data-autocomplete": "new-password", type: "password", name: "np" });
  const got = pickFields([np]);
  assert.equal(got.password, null);
  assert.equal(got.newPassword.length, 1);
  assert.equal(got.newPassword[0].name, "np");
});

test("new-password is generate, not current-password fill", () => {
  const np = el({ autocomplete: "new-password", type: "password", name: "np" });
  const got = pickFields([el({ autocomplete: "username" }), np]);
  assert.equal(got.password, null);
  assert.equal(got.newPassword.length, 1);
  assert.equal(got.newPassword[0].name, "np");
});

test("two new-password fields are one generated value", () => {
  const got = pickFields([
    el({ autocomplete: "new-password", type: "password", name: "p1" }),
    el({ autocomplete: "new-password", type: "password", name: "p2" }),
  ]);
  assert.equal(got.newPassword.length, 2);
});

test("email type and password type without autocomplete", () => {
  const got = pickFields([el({ type: "email", name: "e" }), el({ type: "password", name: "p" })]);
  assert.equal(got.username.name, "e");
  assert.equal(got.password.name, "p");
});

test("one-time-code", () => {
  const got = pickFields([el({ autocomplete: "one-time-code", name: "otp" })]);
  assert.equal(got.totp.name, "otp");
});

test("empty is honest", () => {
  const got = pickFields([]);
  assert.equal(got.username, null);
  assert.equal(got.password, null);
  assert.equal(got.totp, null);
  assert.deepEqual(got.newPassword, []);
  assert.equal(got.number, null);
  assert.equal(got.cvv, null);
});

test("cc-number and cc-csc", () => {
  const got = pickFields([
    el({ autocomplete: "cc-number", name: "num" }),
    el({ autocomplete: "cc-csc", name: "cvc" }),
    el({ autocomplete: "cc-exp-month", name: "em" }),
    el({ autocomplete: "cc-exp-year", name: "ey" }),
  ]);
  assert.equal(got.number.name, "num");
  assert.equal(got.cvv.name, "cvc");
  assert.equal(got.expMonth.name, "em");
  assert.equal(got.expYear.name, "ey");
});

test("identity autocomplete", () => {
  const { pickFields, isFillTarget } = require("./fields.js");
  const got = pickFields([
    el({ autocomplete: "given-name", name: "fn" }),
    el({ autocomplete: "family-name", name: "ln" }),
    el({ autocomplete: "street-address", name: "addr" }),
    el({ autocomplete: "tel", name: "ph" }),
  ]);
  assert.equal(got.given.name, "fn");
  assert.equal(got.family.name, "ln");
  assert.equal(got.address.name, "addr");
  assert.equal(got.phone.name, "ph");
  assert.equal(got.number, null);
  assert.equal(
    isFillTarget(el({ tagName: "INPUT", autocomplete: "cc-number" })),
    true,
  );
  assert.equal(
    isFillTarget(el({ tagName: "INPUT", autocomplete: "postal-code" })),
    true,
  );
});

test("billing cc-number is cc-number", () => {
  const got = pickFields([el({ autocomplete: "billing cc-number", name: "num" })]);
  assert.equal(got.number.name, "num");
});

test("tiny cc-number loses to the real field", () => {
  const tiny = el({ autocomplete: "cc-number", name: "honeypot" });
  tiny.getBoundingClientRect = function () {
    return { width: 1, height: 1 };
  };
  const real = el({ autocomplete: "cc-number", name: "num" });
  real.getBoundingClientRect = function () {
    return { width: 400, height: 40 };
  };
  const got = pickFields([tiny, real]);
  assert.equal(got.number.name, "num");
});

test("writeEntry card is false without a number field", () => {
  const { writeEntry } = require("./fields.js");
  const doc = {
    querySelectorAll: function () {
      return [];
    },
    querySelector: function () {
      return null;
    },
  };
  assert.equal(writeEntry(doc, { kind: "card", number: "4111111111111111" }), false);
});

test("writeCard fills hidden cc-number via querySelector", () => {
  if (typeof HTMLInputElement === "undefined") {
    global.HTMLInputElement = function HTMLInputElement() {};
    HTMLInputElement.prototype = {};
  }
  if (typeof HTMLTextAreaElement === "undefined") {
    global.HTMLTextAreaElement = function HTMLTextAreaElement() {};
    HTMLTextAreaElement.prototype = {};
  }
  const { writeEntry } = require("./fields.js");
  const number = el({ autocomplete: "cc-number", name: "num", hidden: true });
  number.value = "";
  number.dispatchEvent = function () {};
  const month = el({ autocomplete: "cc-exp-month", name: "em", hidden: true });
  month.value = "";
  month.dispatchEvent = function () {};
  const year = el({ autocomplete: "cc-exp-year", name: "ey", hidden: true });
  year.value = "";
  year.dispatchEvent = function () {};
  const cvc = el({ autocomplete: "cc-csc", name: "cvc", hidden: true });
  cvc.value = "";
  cvc.dispatchEvent = function () {};
  const doc = {
    querySelectorAll: function () {
      return [number, month, year, cvc];
    },
    querySelector: function (sel) {
      return sel === 'input[autocomplete="cc-number"]' ? number : null;
    },
  };
  assert.equal(
    writeEntry(doc, { kind: "card", number: "4111111111111111", expMonth: "12", expYear: "2030", cvv: "123" }),
    true,
  );
  assert.equal(number.value.length, 16);
});

test("canSave current-password with a value", () => {
  const pw = el({ autocomplete: "current-password", type: "password", name: "p", value: "hunter2" });
  const fields = pickFields([el({ autocomplete: "username", name: "u" }), pw]);
  assert.equal(canSave(fields, fields.password), true);
});

test("canSave empty current-password is false", () => {
  const pw = el({ autocomplete: "current-password", type: "password", name: "p", value: "" });
  const fields = pickFields([pw]);
  assert.equal(canSave(fields, fields.password), false);
});

test("canSave new-password is generate, not typed-save", () => {
  const np = el({ autocomplete: "new-password", type: "password", name: "np", value: "generated" });
  const fields = pickFields([np]);
  assert.equal(canSave(fields, fields.password), false);
});

test("canSave cc-number is not a login", () => {
  const num = el({ autocomplete: "cc-number", name: "num", value: "4111111111111111" });
  const fields = pickFields([num]);
  assert.equal(canSave(fields, fields.number), false);
});

test("findOTPAuth from totp link", () => {
  const href = "otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP";
  const root = {
    querySelector: function (sel) {
      return sel === 'a[href^="otpauth://totp"]' ? { href: href } : null;
    },
    querySelectorAll: function () {
      return [];
    },
  };
  assert.equal(findOTPAuth(root), href);
});

function writable(partial) {
  if (typeof HTMLInputElement === "undefined") {
    global.HTMLInputElement = function HTMLInputElement() {};
    HTMLInputElement.prototype = {};
  }
  if (typeof HTMLTextAreaElement === "undefined") {
    global.HTMLTextAreaElement = function HTMLTextAreaElement() {};
    HTMLTextAreaElement.prototype = {};
  }
  const node = el(partial);
  node.value = "";
  node.dispatchEvent = function () {};
  return node;
}

function docOf(inputs) {
  return {
    querySelectorAll: function () {
      return inputs;
    },
    querySelector: function () {
      return null;
    },
  };
}

test("picked login fills current-password, never new-password", () => {
  const { writeLogin } = require("./fields.js");
  const current = writable({ autocomplete: "current-password", type: "password", name: "cur" });
  const fresh = writable({ autocomplete: "new-password", type: "password", name: "np1" });
  const confirm = writable({ autocomplete: "new-password", type: "password", name: "np2" });
  writeLogin(docOf([writable({ autocomplete: "username", name: "u" }), current, fresh, confirm]), {
    kind: "login",
    login: "ada",
    password: "old_pw",
  });
  assert.equal(current.value, "old_pw");
  assert.equal(fresh.value, "");
  assert.equal(confirm.value, "");
});

test("generated entry fills every new-password field", () => {
  const { writeLogin } = require("./fields.js");
  const current = writable({ autocomplete: "current-password", type: "password", name: "cur" });
  const fresh = writable({ autocomplete: "new-password", type: "password", name: "np1" });
  const confirm = writable({ autocomplete: "new-password", type: "password", name: "np2" });
  writeLogin(docOf([writable({ autocomplete: "username", name: "u" }), current, fresh, confirm]), {
    kind: "login",
    login: "ada",
    password: "minted_pw",
    generated: true,
  });
  assert.equal(current.value, "");
  assert.equal(fresh.value, "minted_pw");
  assert.equal(confirm.value, "minted_pw");
});

test("picked login on a signup form still lands in new-password", () => {
  const { writeLogin } = require("./fields.js");
  const fresh = writable({ autocomplete: "new-password", type: "password", name: "np1" });
  const confirm = writable({ autocomplete: "new-password", type: "password", name: "np2" });
  writeLogin(docOf([writable({ autocomplete: "username", name: "u" }), fresh, confirm]), {
    kind: "login",
    login: "ada",
    password: "old_pw",
  });
  assert.equal(fresh.value, "old_pw");
  assert.equal(confirm.value, "old_pw");
});

test("findOTPAuth empty in iframe", () => {
  const prev = global.window;
  global.window = { top: {} };
  try {
    const href = "otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP";
    const root = {
      querySelector: function () {
        return { href: href };
      },
      querySelectorAll: function () {
        return [];
      },
    };
    assert.equal(findOTPAuth(root), "");
  } finally {
    global.window = prev;
  }
});
