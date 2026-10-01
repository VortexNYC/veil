import * as Crypto from "expo-crypto";
import * as SecureStore from "expo-secure-store";
import * as WebBrowser from "expo-web-browser";

export const issuer = "https://id.veil.nyc";
export const clientID = "veil";
export const originAPI = "https://veil.nyc";
export const redirectURI = "veil://oidc/callback";

const tokenKey = "veil.id_token";

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
  if (!raw) return null;
  if (expired(raw)) {
    await SecureStore.deleteItemAsync(tokenKey);
    return null;
  }
  return raw;
}

/**
 * Sign-in is the same PKCE authorize flow the SPA runs — the browser page is
 * Ory's own UI at login.veil.nyc, so the app never handles the password or
 * TOTP. It only receives the code on the veil:// callback.
 */
export async function signIn(): Promise<void> {
  const verifier = randomB64(32);
  const state = randomB64(16);
  const digest = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, verifier, {
    encoding: Crypto.CryptoEncoding.BASE64,
  });
  const challenge = digest.replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
  const u = new URL("/oauth2/auth", issuer);
  u.searchParams.set("client_id", clientID);
  u.searchParams.set("response_type", "code");
  u.searchParams.set("scope", "openid");
  u.searchParams.set("redirect_uri", redirectURI);
  u.searchParams.set("state", state);
  u.searchParams.set("code_challenge", challenge);
  u.searchParams.set("code_challenge_method", "S256");
  u.searchParams.set("max_age", "0");
  u.searchParams.set("prompt", "login");

  const result = await WebBrowser.openAuthSessionAsync(u.toString(), redirectURI);
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
  if (!amrOf(idToken).includes("totp")) throw new Error("aal2 required — complete TOTP");
  await SecureStore.setItemAsync(tokenKey, idToken);
}

export async function signOut(): Promise<void> {
  const { clearAutofill } = await import("../../modules/identity-sync");
  await clearAutofill().catch(() => {});
  await SecureStore.deleteItemAsync(tokenKey);
}
