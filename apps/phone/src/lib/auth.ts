import * as Crypto from "expo-crypto";
import * as SecureStore from "expo-secure-store";
import * as WebBrowser from "expo-web-browser";

export const issuer = "https://id.veil.nyc";
export const clientID = "veil";
export const originAPI = "https://veil.nyc";
export const redirectURI = "veil://oidc/callback";

const tokenKey = "veil.id_token";
const refreshKey = "veil.refresh_token";

function b64url(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) {
    s += String.fromCharCode(b);
  }
  return btoa(s).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
}

function randomB64(n: number): string {
  return b64url(Crypto.getRandomBytes(n));
}

function claimsOf(raw: string): Record<string, unknown> {
  const parts = raw.split(".");
  if (parts.length < 2) return {};
  const pad = parts[1].padEnd(parts[1].length + ((4 - (parts[1].length % 4)) % 4), "=");
  try {
    const json = atob(pad.replaceAll("-", "+").replaceAll("_", "/"));
    const parsed: unknown = JSON.parse(json);
    return typeof parsed === "object" && parsed !== null ? (parsed as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

function amrOf(raw: string): string[] {
  const amr = claimsOf(raw).amr;
  return Array.isArray(amr) ? amr.filter((v): v is string => typeof v === "string") : [];
}

function expired(raw: string): boolean {
  const exp = claimsOf(raw).exp;
  return typeof exp !== "number" || exp * 1000 <= Date.now() + 15_000;
}

export async function storedToken(): Promise<string | null> {
  const raw = await SecureStore.getItemAsync(tokenKey);
  if (raw && !expired(raw)) return raw;
  const minted = await refreshSession();
  if (!minted) await SecureStore.deleteItemAsync(tokenKey);
  return minted;
}

export async function storedRefresh(): Promise<string> {
  return (await SecureStore.getItemAsync(refreshKey)) ?? "";
}

let refreshing: Promise<string | null> | null = null;

/**
 * id_token expired — remint from the refresh token instead of dropping the
 * user to sign-in. The AutoFill appex can refresh first and write its new
 * pair back into the shared handoff, so on an invalid_grant we retry once
 * with the handoff's newer copy before giving up.
 */
export function refreshSession(): Promise<string | null> {
  refreshing ??= refreshSessionOnce().finally(() => {
    refreshing = null;
  });
  return refreshing;
}

async function refreshSessionOnce(): Promise<string | null> {
  const mod = await import("../../modules/identity-sync");
  for (const rt of await refreshCandidates()) {
    const pair = await refreshGrant(rt);
    if (!pair) continue;
    await SecureStore.setItemAsync(tokenKey, pair.id);
    await SecureStore.setItemAsync(refreshKey, pair.refresh);
    await mod.refreshAutofill(pair.id, pair.refresh).catch(() => {});
    return pair.id;
  }
  await SecureStore.deleteItemAsync(refreshKey);
  return null;
}

async function refreshCandidates(): Promise<string[]> {
  const mod = await import("../../modules/identity-sync");
  const own = await SecureStore.getItemAsync(refreshKey);
  const theirs = await mod.autofillAuth().catch(() => null);
  return [own, theirs?.refresh].filter(
    (v): v is string => typeof v === "string" && v !== "",
  );
}

async function refreshGrant(
  rt: string,
): Promise<{ id: string; refresh: string } | null> {
  const res = await fetch(new URL("/oauth2/token", issuer).toString(), {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "refresh_token",
      client_id: clientID,
      refresh_token: rt,
      scope: "openid offline_access",
    }),
  }).catch(() => null);
  if (!res || res.status === 400 || res.status === 401) return null;
  if (!res.ok) return null;
  const parsed: unknown = await res.json().catch(() => null);
  if (typeof parsed !== "object" || parsed === null) return null;
  const id = "id_token" in parsed ? parsed.id_token : null;
  const refresh = "refresh_token" in parsed ? parsed.refresh_token : null;
  if (typeof id !== "string" || id === "" || typeof refresh !== "string" || refresh === "") {
    return null;
  }
  return { id, refresh };
}

/**
 * Sign-in is the same PKCE authorize flow the SPA runs — the browser page is
 * Ory's own UI at login.veil.nyc, so the app never handles the password or
 * TOTP. It only receives the code on the veil:// callback.
 *
 * Do not force prompt=login/max_age here: a live session skips the form and the
 * aal2 assertion below is the real gate. Forcing re-auth makes Kratos build a
 * refresh login flow that can render zero methods — a dead-end for users.
 */
export async function signIn(): Promise<void> {
  const first = await runAuth(false);
  if (!amrOf(first.id).includes("totp")) {
    // aal1 session got reused; redo the dance forcing fresh factors
    const fresh = await runAuth(true);
    if (!amrOf(fresh.id).includes("totp")) throw new Error("aal2 required — complete TOTP");
    await storeTokens(fresh);
    return;
  }
  await storeTokens(first);
}

async function storeTokens(pair: { id: string; refresh: string | null }): Promise<void> {
  await SecureStore.setItemAsync(tokenKey, pair.id);
  if (pair.refresh) await SecureStore.setItemAsync(refreshKey, pair.refresh);
}

async function runAuth(forceLogin: boolean): Promise<{ id: string; refresh: string | null }> {
  const verifier = randomB64(32);
  const state = randomB64(16);
  const digest = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, verifier, {
    encoding: Crypto.CryptoEncoding.BASE64,
  });
  const challenge = digest.replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
  const u = new URL("/oauth2/auth", issuer);
  u.searchParams.set("client_id", clientID);
  u.searchParams.set("response_type", "code");
  u.searchParams.set("scope", "openid offline_access");
  u.searchParams.set("redirect_uri", redirectURI);
  u.searchParams.set("state", state);
  u.searchParams.set("code_challenge", challenge);
  u.searchParams.set("code_challenge_method", "S256");
  if (forceLogin) {
    u.searchParams.set("max_age", "0");
    u.searchParams.set("prompt", "login");
  }

  const result = await WebBrowser.openAuthSessionAsync(u.toString(), redirectURI, {
    // Ephemeral jar: no shared Safari cookies/cache — every sign-in is a clean
    // dance and a stale session can never wedge the login UI.
    preferEphemeralSession: true,
  });
  if (result.type !== "success" || !result.url) {
    throw new Error("sign-in canceled");
  }
  const cb = new URL(result.url);
  const err = cb.searchParams.get("error");
  if (err) throw new Error(err);
  if (cb.searchParams.get("state") !== state) throw new Error("state mismatch");
  const code = cb.searchParams.get("code");
  if (!code) throw new Error("no code");

  const body = new URLSearchParams({
    grant_type: "authorization_code",
    client_id: clientID,
    code,
    redirect_uri: redirectURI,
    code_verifier: verifier,
  });
  const res = await fetch(new URL("/oauth2/token", issuer).toString(), {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: body.toString(),
  });
  if (!res.ok) throw new Error(`token exchange ${res.status}: ${(await res.text()).slice(0, 200)}`);
  const parsed: unknown = await res.json();
  const idToken =
    typeof parsed === "object" && parsed !== null && "id_token" in parsed ? parsed.id_token : null;
  if (typeof idToken !== "string" || idToken === "") throw new Error("token exchange failed");
  const refresh =
    typeof parsed === "object" && parsed !== null && "refresh_token" in parsed
      ? parsed.refresh_token
      : null;
  return { id: idToken, refresh: typeof refresh === "string" ? refresh : null };
}

export async function signOut(): Promise<void> {
  const { clearAutofill } = await import("../../modules/identity-sync");
  await clearAutofill().catch(() => {});
  await SecureStore.deleteItemAsync(tokenKey);
  await SecureStore.deleteItemAsync(refreshKey);
}
