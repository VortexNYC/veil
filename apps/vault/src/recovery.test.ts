import { describe, expect, it } from "vitest";
import nacl from "tweetnacl";
import { b64, generateRecoveryKeypair, hex, recoveryKitText } from "./recovery";

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
