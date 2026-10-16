export const issuer = "https://id.veil.nyc"
export const clientID = "veil"
export const originAPI = "https://veil.nyc"

const tokenKey = "veil.id_token"
const verifierKey = "veil.pkce"
const stateKey = "veil.state"

export type AuthorizeInput = {
  issuer: string
  clientID: string
  redirectURI: string
  state: string
  challenge: string
  force?: boolean
}

export function buildAuthorizeURL(input: AuthorizeInput): string {
  const u = new URL("/oauth2/auth", input.issuer)
  u.searchParams.set("client_id", input.clientID)
  u.searchParams.set("code_challenge", input.challenge)
  u.searchParams.set("code_challenge_method", "S256")
  if (input.force) {
    u.searchParams.set("max_age", "0")
    u.searchParams.set("prompt", "login")
  }
  u.searchParams.set("redirect_uri", input.redirectURI)
  u.searchParams.set("response_type", "code")
  u.searchParams.set("scope", "openid")
  u.searchParams.set("state", input.state)
  return u.toString()
}

export function redirectURI(): string {
  return `${window.location.origin}/oidc/callback`
}

function b64url(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf)
  let s = ""
  for (const b of bytes) {
    s += String.fromCharCode(b)
  }
  return btoa(s).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "")
}

async function sha256(raw: string): Promise<string> {
  const data = new TextEncoder().encode(raw)
  const digest = await crypto.subtle.digest("SHA-256", data)
  return b64url(digest)
}

function random(n: number): string {
  const bytes = new Uint8Array(n)
  crypto.getRandomValues(bytes)
  return b64url(bytes.buffer)
}

export function token(): string | null {
  const raw = sessionStorage.getItem(tokenKey)
  if (!raw) {
    return null
  }
  if (expired(raw)) {
    sessionStorage.removeItem(tokenKey)
    return null
  }
  return raw
}

export function signedIn(): boolean {
  return token() !== null
}

function expired(raw: string): boolean {
  const claims = claimsOf(raw)
  const exp = claims.exp
  return typeof exp !== "number" || exp * 1000 <= Date.now() + 15_000
}

export function claimsOf(raw: string): Record<string, unknown> {
  const parts = raw.split(".")
  if (parts.length < 2) {
    return {}
  }
  const pad = parts[1].padEnd(parts[1].length + ((4 - (parts[1].length % 4)) % 4), "=")
  try {
    const json = atob(pad.replaceAll("-", "+").replaceAll("_", "/"))
    const parsed: unknown = JSON.parse(json)
    if (typeof parsed === "object" && parsed !== null) {
      return parsed as Record<string, unknown>
    }
  } catch {
    return {}
  }
  return {}
}

export function amrOf(raw: string): string[] {
  const amr = claimsOf(raw).amr
  if (!Array.isArray(amr)) {
    return []
  }
  return amr.filter((v): v is string => typeof v === "string")
}

export async function beginLogin(force = false): Promise<string> {
  const verifier = random(32)
  const state = random(16)
  const challenge = await sha256(verifier)
  sessionStorage.setItem(verifierKey, verifier)
  sessionStorage.setItem(stateKey, state)
  return buildAuthorizeURL({
    issuer,
    clientID,
    redirectURI: redirectURI(),
    state,
    challenge,
    force,
  })
}

export async function finishLogin(href: string): Promise<void> {
  const u = new URL(href)
  const err = u.searchParams.get("error")
  if (err) {
    throw new Error(err)
  }
  const code = u.searchParams.get("code")
  const state = u.searchParams.get("state")
  const expect = sessionStorage.getItem(stateKey)
  const verifier = sessionStorage.getItem(verifierKey)
  sessionStorage.removeItem(stateKey)
  sessionStorage.removeItem(verifierKey)
  if (!code || !state || !expect || state !== expect || !verifier) {
    throw new Error("state")
  }
  const body = new URLSearchParams({
    grant_type: "authorization_code",
    client_id: clientID,
    code,
    redirect_uri: redirectURI(),
    code_verifier: verifier,
  })
  const res = await fetch(new URL("/oauth2/token", issuer), {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body,
  })
  if (!res.ok) {
    throw new Error("token")
  }
  const parsed: unknown = await res.json()
  if (typeof parsed !== "object" || parsed === null || !("id_token" in parsed)) {
    throw new Error("token")
  }
  const idToken = parsed.id_token
  if (typeof idToken !== "string" || idToken === "") {
    throw new Error("token")
  }
  if (!amrOf(idToken).includes("totp")) {
    throw new Error("aal2")
  }
  sessionStorage.setItem(tokenKey, idToken)
}

export function signOut(): void {
  sessionStorage.removeItem(tokenKey)
  sessionStorage.removeItem(verifierKey)
  sessionStorage.removeItem(stateKey)
}
