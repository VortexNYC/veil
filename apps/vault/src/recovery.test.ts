import { describe, expect, it } from "vitest";
import nacl from "tweetnacl";
import {
  b64,
  emergencyKitHTML,
  generateRecoveryKeypair,
  hex,
  recoveryKitText,
} from "./recovery";

describe("generateRecoveryKeypair", () => {
  it("mints a 32-byte X25519 pair whose pub re-derives from priv", () => {
    const { pub, priv } = generateRecoveryKeypair();
    expect(pub.length).toBe(32);
    expect(priv.length).toBe(32);
    // The CLI derives pub via curve25519.X25519(priv, basepoint); the kit
    // must agree so a restored priv still matches the enrolled share.
    expect(nacl.box.keyPair.fromSecretKey(priv).publicKey).toEqual(pub);
    const again = generateRecoveryKeypair();
    expect(again.priv).not.toEqual(priv);
  });
});

describe("recoveryKitText", () => {
  it("carries the hex priv and base64 pub — never a vault secret", () => {
    const { pub, priv } = generateRecoveryKeypair();
    const text = recoveryKitText("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", pub, priv);
    expect(text).toContain(hex(priv));
    expect(text).toContain(b64(pub));
    expect(text).toContain("aaaaaaaa");
    // The org master is 32 raw bytes — the kit only ever holds the share
    // private key. No field other than the two keys should decode to 32B.
    expect(text.split("\n").filter((l) => /^[0-9a-f]{64}$/.test(l)).length).toBe(1);
  });
});

describe("emergencyKitHTML", () => {
  it("carries exactly the priv hex + pubkey and the printable context — never master material", () => {
    const { pub, priv } = generateRecoveryKeypair();
    const html = emergencyKitHTML(
      "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
      "ada@example.com",
      pub,
      priv,
    );
    // The only 64-hex secret material in the page is the recovery key.
    expect(html.match(/[0-9a-f]{64}/g)).toEqual([hex(priv)]);
    expect(html).toContain(b64(pub));
    expect(html).toContain("ada@example.com");
    expect(html).toContain("app.veil.nyc");
    expect(html).toContain("aaaaaaaa");
    expect(html).toContain("@media print");
    expect(html).toContain("veil"); // product, not org master or item secrets
    expect(html.toLowerCase()).not.toContain("master");
  });

  it("escapes the member email and org id in markup", () => {
    const { pub, priv } = generateRecoveryKeypair();
    const html = emergencyKitHTML(
      'aa"><script>alert(1)</script>',
      'a<b>@x.test',
      pub,
      priv,
    );
    expect(html).not.toContain("<script>alert(1)");
    expect(html).toContain("a&lt;b&gt;@x.test");
  });
});
