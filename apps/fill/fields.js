// Field pick + write. WHATWG autocomplete first. No site catalog.
(function (root) {
  function fieldToken(s) {
    const skip = {
      on: true,
      off: true,
      shipping: true,
      billing: true,
      home: true,
      work: true,
      mobile: true,
      fax: true,
      pager: true,
    };
    const parts = String(s || "")
      .toLowerCase()
      .trim()
      .split(/\s+/);
    for (let i = parts.length - 1; i >= 0; i--) {
      const p = parts[i];
      if (!p || skip[p] || p.indexOf("section-") === 0) {
        continue;
      }
      return p;
    }
    return "";
  }
  function auto(el) {
    if (!el) {
      return "";
    }
    const tokens = [el.autocomplete];
    if (typeof el.getAttribute === "function") {
      tokens.push(el.getAttribute("autocomplete"), el.getAttribute("data-autocomplete"));
    }
    for (let i = 0; i < tokens.length; i++) {
      const s = fieldToken(tokens[i]);
      if (s) {
        return s;
      }
    }
    return "";
  }
  function typ(el) {
    return String(el.type || "text").toLowerCase();
  }
  function ident(el) {
    return String(el.name || el.id || "");
  }
  function visible(el) {
    if (el.hidden || el.disabled) {
      return false;
    }
    if (typeof el.getBoundingClientRect === "function") {
      const r = el.getBoundingClientRect();
      if (r.width < 16 || r.height < 8) {
        if (auto(el).indexOf("cc-") !== 0) {
          return false;
        }
      }
    } else if (typeof el.offsetParent !== "undefined" && el.offsetParent === null && typ(el) !== "password") {
      return false;
    }
    const view = el.ownerDocument && el.ownerDocument.defaultView;
    if (view && view.getComputedStyle) {
      const cs = view.getComputedStyle(el);
      if (cs && (cs.display === "none" || cs.visibility === "hidden" || cs.opacity === "0")) {
        return false;
      }
    }
    return true;
  }

  function pickFields(els) {
    const live = (els || []).filter(visible);
    const byAuto = function (names) {
      const hits = live.filter(function (el) {
        return names.indexOf(auto(el)) !== -1;
      });
      if (!hits.length) {
        return undefined;
      }
      hits.sort(function (a, b) {
        const ra = typeof a.getBoundingClientRect === "function" ? a.getBoundingClientRect() : { width: 0, height: 0 };
        const rb = typeof b.getBoundingClientRect === "function" ? b.getBoundingClientRect() : { width: 0, height: 0 };
        return rb.width * rb.height - ra.width * ra.height;
      });
      return hits[0];
    };
    const username =
      byAuto(["username", "email"]) ||
      live.find(function (el) {
        return typ(el) === "email";
      }) ||
      live.find(function (el) {
        return /user|email|login/i.test(ident(el)) && typ(el) !== "password";
      }) ||
      null;
    const current =
      byAuto(["current-password"]) ||
      live.find(function (el) {
        return typ(el) === "password" && auto(el) !== "new-password";
      }) ||
      null;
    const newPassword = live.filter(function (el) {
      return auto(el) === "new-password";
    });
    const totp =
      byAuto(["one-time-code"]) ||
      live.find(function (el) {
        return /otp|totp|one-time|2fa/i.test(ident(el) + " " + auto(el));
      }) ||
      null;
    const number =
      byAuto(["cc-number"]) ||
      live.find(function (el) {
        return /card.?number|cc-num/i.test(ident(el) + " " + auto(el));
      }) ||
      null;
    const expMonth = byAuto(["cc-exp-month"]);
    const expYear = byAuto(["cc-exp-year"]);
    const exp = byAuto(["cc-exp"]);
    const cvv = byAuto(["cc-csc", "cc-cvc"]);
    const given = byAuto(["given-name", "cc-given-name"]);
    const family = byAuto(["family-name", "cc-family-name"]);
    const name = byAuto(["cc-name", "name"]);
    const address = byAuto(["street-address", "address-line1"]);
    const city = byAuto(["address-level2"]);
    const region = byAuto(["address-level1"]);
    const postal = byAuto(["postal-code"]);
    const country = byAuto(["country", "country-name"]);
    const phone = byAuto(["tel", "tel-national"]);
    const email = byAuto(["email"]);
    return {
      username: username || null,
      password: current || null,
      newPassword: newPassword,
      totp: totp || null,
      number: number || null,
      expMonth: expMonth || null,
      expYear: expYear || null,
      exp: exp || null,
      cvv: cvv || null,
      given: given || null,
      family: family || null,
      name: name || null,
      address: address || null,
      city: city || null,
      region: region || null,
      postal: postal || null,
      country: country || null,
      phone: phone || null,
      email: email || null,
    };
  }

  function nativeSet(el, value) {
    const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    const desc = Object.getOwnPropertyDescriptor(proto, "value");
    if (desc && desc.set) {
      desc.set.call(el, value);
    }
    el.value = value;
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
  }

  function writeField(el, value) {
    if (!el || value == null || value === "") {
      return;
    }
    nativeSet(el, value);
  }

  function writeLogin(doc, entry) {
    const fields = pickFields(Array.prototype.slice.call(doc.querySelectorAll("input, textarea")));
    writeField(fields.username, entry.login);
    if (fields.newPassword && fields.newPassword.length) {
      fields.newPassword.forEach(function (el) {
        writeField(el, entry.password);
      });
    } else {
      writeField(fields.password, entry.password);
    }
    writeField(fields.totp, entry.totp);
  }

  function writeCard(doc, entry) {
    const fields = pickFields(Array.prototype.slice.call(doc.querySelectorAll("input, textarea")));
    let number = fields.number;
    if (!number && doc.querySelector) {
      number = doc.querySelector('input[autocomplete="cc-number"]');
    }
    writeField(number, entry.number);
    writeField(fields.expMonth, entry.expMonth);
    writeField(fields.expYear, entry.expYear);
    if (fields.exp && entry.expMonth && entry.expYear) {
      writeField(fields.exp, entry.expMonth + "/" + String(entry.expYear).slice(-2));
    }
    writeField(fields.cvv, entry.cvv);
    writeField(fields.given, entry.givenName);
    writeField(fields.name, entry.givenName);
    return !!(number && number.value);
  }

  function writeIdentity(doc, entry) {
    const fields = pickFields(Array.prototype.slice.call(doc.querySelectorAll("input, textarea")));
    writeField(fields.given, entry.givenName);
    writeField(fields.family, entry.familyName);
    writeField(fields.address, entry.address);
    writeField(fields.city, entry.city);
    writeField(fields.region, entry.region);
    writeField(fields.postal, entry.postal);
    writeField(fields.country, entry.country);
    writeField(fields.phone, entry.phone);
    writeField(fields.email, entry.email);
    if (entry.givenName && entry.familyName) {
      writeField(fields.name, entry.givenName + " " + entry.familyName);
    }
  }

  function writeEntry(doc, entry) {
    if (!entry) {
      return false;
    }
    if (entry.kind === "card") {
      return writeCard(doc, entry);
    }
    if (entry.kind === "identity") {
      writeIdentity(doc, entry);
      return true;
    }
    writeLogin(doc, entry);
    return true;
  }

  function isFillTarget(el) {
    if (!el || (el.tagName !== "INPUT" && el.tagName !== "TEXTAREA")) {
      return false;
    }
    if (auto(el) === "new-password") {
      return true;
    }
    const t = typ(el);
    if (t === "password" || t === "email") {
      return true;
    }
    const a = auto(el);
    return (
      a === "username" ||
      a === "email" ||
      a === "current-password" ||
      a === "one-time-code" ||
      a.indexOf("cc-") === 0 ||
      a === "given-name" ||
      a === "family-name" ||
      a === "street-address" ||
      a === "address-line1" ||
      a === "address-level1" ||
      a === "address-level2" ||
      a === "postal-code" ||
      a === "country" ||
      a === "country-name" ||
      a === "tel" ||
      a === "tel-national" ||
      a === "name"
    );
  }

  // What the focused form can take: a login form only wants logins — offering
  // cards/identities on a sign-in page is noise that blocks the field.
  function fillContext(el, fields) {
    const a = auto(el);
    const idf = (ident(el) + " " + a).toLowerCase();
    if (a.indexOf("cc-") === 0 || /card.?num|cc-num/.test(idf)) {
      return "card";
    }
    if (
      /^(street|address|postal|country|tel|given|family)/.test(a) ||
      /address|postal|zip|country|phone/.test(idf)
    ) {
      return "identity";
    }
    if (typ(el) === "password" || a === "username" || a === "email" || typ(el) === "email") {
      return "login";
    }
    if (fields) {
      if (fields.number || fields.cvv || fields.exp || fields.expMonth || fields.expYear) {
        return "card";
      }
      if (fields.password || fields.username) {
        return "login";
      }
      if (fields.address || fields.postal || fields.city || fields.region || fields.country) {
        return "identity";
      }
    }
    return "generic";
  }

  function isTopWindow() {
    try {
      return typeof window === "undefined" || window === window.top;
    } catch {
      return false;
    }
  }

  function canSave(fields, el) {
    if (!isTopWindow()) {
      return false;
    }
    if (!fields || !fields.password) {
      return false;
    }
    if (fields.newPassword && fields.newPassword.length) {
      return false;
    }
    const target = el || fields.password;
    const a = auto(target);
    if (a.indexOf("cc-") === 0) {
      return false;
    }
    if (fields.number && target === fields.number) {
      return false;
    }
    if (a === "new-password") {
      return false;
    }
    const pw = String(fields.password.value || "");
    if (!pw) {
      return false;
    }
    return a === "current-password" || typ(fields.password) === "password";
  }

  function findOTPAuth(root) {
    if (!isTopWindow()) {
      return "";
    }
    if (!root || typeof root.querySelector !== "function") {
      return "";
    }
    const link = root.querySelector('a[href^="otpauth://totp"]');
    if (link && link.href && String(link.href).indexOf("otpauth://totp") === 0) {
      return String(link.href);
    }
    const nodes =
      typeof root.querySelectorAll === "function"
        ? root.querySelectorAll("input, textarea, code, pre, [data-otpauth]")
        : [];
    for (let i = 0; i < nodes.length; i++) {
      const el = nodes[i];
      const bits = [el.value, el.textContent];
      if (typeof el.getAttribute === "function") {
        bits.push(el.getAttribute("data-otpauth"));
      }
      for (let j = 0; j < bits.length; j++) {
        const s = String(bits[j] || "").trim();
        const idx = s.indexOf("otpauth://totp");
        if (idx === -1) {
          continue;
        }
        const rest = s.slice(idx).split(/\s/)[0];
        if (rest.indexOf("otpauth://totp") === 0) {
          return rest;
        }
      }
    }
    return "";
  }

  root.veilFields = {
    auto: auto,
    pickFields: pickFields,
    writeField: writeField,
    writeLogin: writeLogin,
    writeCard: writeCard,
    writeIdentity: writeIdentity,
    writeEntry: writeEntry,
    isFillTarget: isFillTarget,
    fillContext: fillContext,
    canSave: canSave,
    findOTPAuth: findOTPAuth,
  };
  if (typeof module !== "undefined" && module.exports) {
    module.exports = root.veilFields;
  }
})(typeof globalThis !== "undefined" ? globalThis : this);
