import { describe, expect, it } from "vitest"
import { amrOf, buildAuthorizeURL, claimsOf } from "./auth"

describe("buildAuthorizeURL", () => {
  it("lets a live session skip the prompt; force still re-auths for aal2", () => {
    const url = buildAuthorizeURL({
      issuer: "https://id.veil.nyc",
      clientID: "veil",
      redirectURI: "https://app.veil.nyc/oidc/callback",
      state: "st",
      challenge: "ch",
    })
    const u = new URL(url)
    expect(u.origin).toBe("https://id.veil.nyc")
    expect(u.searchParams.get("prompt")).toBeNull()
    expect(u.searchParams.get("max_age")).toBeNull()
    const forced = new URL(buildAuthorizeURL({
      issuer: "https://id.veil.nyc",
      clientID: "veil",
      redirectURI: "https://app.veil.nyc/oidc/callback",
      state: "st",
      challenge: "ch",
      force: true,
    }))
    expect(forced.searchParams.get("prompt")).toBe("login")
    expect(forced.searchParams.get("max_age")).toBe("0")
    expect(u.searchParams.get("code_challenge_method")).toBe("S256")
    expect(u.searchParams.get("scope")).toBe("openid")
    expect([...u.searchParams.keys()]).not.toEqual(
      expect.arrayContaining(["secret", "password", "token"]),
    )
  })
})

describe("claimsOf", () => {
  it("reads amr without treating other fields as material", () => {
    const payload = btoa(JSON.stringify({ amr: ["password", "totp"], exp: 1 }))
      .replaceAll("+", "-")
      .replaceAll("/", "_")
      .replaceAll("=", "")
    const raw = `hdr.${payload}.sig`
    expect(amrOf(raw)).toEqual(["password", "totp"])
    expect(claimsOf(raw).exp).toBe(1)
  })
})
