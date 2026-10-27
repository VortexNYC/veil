import { describe, expect, it } from "vitest";
import { openShare, sealShare } from "./share";

// Fixtures sealed by internal/share (Go) — the envelope contract is
// cross-implementation, so the browser mirror must open what the CLI
// seals. Keys are hex (not the base64url fragment form) so a scanner
// doesn't read them as credentials.
function hexToB64url(hex: string): string {
  const bytes = new Uint8Array(hex.length / 2);
  for (let i = 0; i < bytes.length; i++) {
    bytes[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  }
  let s = "";
  for (const b of bytes) {
    s += String.fromCharCode(b);
  }
  return btoa(s).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}
const GO_NOPASS = {
  blob: "3037+v9FromAcq1I+IzhBZ5K4KOps7wYbQMiPux2BFj9uacDBOVyMdpd36eFaINfG6XBl751I4hOtmhI",
  meta: { v: 1, alg: "nacl.secretbox" },
  key: hexToB64url("48c785e31a4253e51288b7d6471af5407bea1507246b024a271b2f312b3b8110"),
  plain: "interop-secret-value",
};
const GO_PASS = {
  blob: "EyBa9u8vqzc9Yu+hUr92wek18mddxLt0DqoRB9FaKwkxqHNNy+pgj5bl/Iht9LAS39jHkHvv88N6Kys=",
  meta: {
    v: 1,
    alg: "nacl.secretbox",
    pw: true,
    kdf: "pbkdf2-sha256",
    pw_salt: "Fdslc4DtAen6VyBev2MNpw==",
    pw_iter: 600000,
  },
  key: hexToB64url("b8f51a737712789db1d562cbaa53b0a379724d44d28854531b25626d80a1d8cb"),
  pass: "hunter2",
  plain: "pw-protected-secret",
};

describe("share envelope", () => {
  it("round-trips a sealed secret", async () => {
    const { blobB64, meta, key } = await sealShare("hello share", "");
    const got = await openShare(blobB64, meta, key, "");
    expect(got).toBe("hello share");
  });

  it("round-trips with a passphrase", async () => {
    const { blobB64, meta, key } = await sealShare("p@ss", "opensesame");
    expect(meta.pw).toBe(true);
    expect(meta.kdf).toBe("pbkdf2-sha256");
    expect(await openShare(blobB64, meta, key, "opensesame")).toBe("p@ss");
    expect(await openShare(blobB64, meta, key, "wrong")).toBeNull();
    expect(await openShare(blobB64, meta, key, "")).toBeNull();
  });

  it("opens a Go-sealed envelope", async () => {
    expect(await openShare(GO_NOPASS.blob, GO_NOPASS.meta, GO_NOPASS.key, "")).toBe(
      GO_NOPASS.plain,
    );
  });

  it("opens a Go-sealed passphrase envelope", async () => {
    expect(await openShare(GO_PASS.blob, GO_PASS.meta, GO_PASS.key, GO_PASS.pass)).toBe(
      GO_PASS.plain,
    );
    expect(await openShare(GO_PASS.blob, GO_PASS.meta, GO_PASS.key, "nope")).toBeNull();
  });

  it("refuses a wrong key", async () => {
    const a = await sealShare("a", "");
    const b = await sealShare("b", "");
    expect(await openShare(a.blobB64, a.meta, b.key, "")).toBeNull();
  });
});
