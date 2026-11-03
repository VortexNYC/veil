import { createHash } from "node:crypto";

export const sha256 = (s) => createHash("sha256").update(s, "utf8").digest("hex");

/** Digests that are literals in Railway env, not secret material. */
const LITERAL_DIGESTS = new Set([
  "header",
  "Authorization",
  "api_key",
  "unset",
  "Lax",
  "Strict",
  "true",
  "false",
  "4433",
  "4434",
  "4444",
  "4456",
  "4461",
  "disable",
  "require",
]);

/**
 * Store sha256 in git for secret values; keep short non-secret literals for plan stability.
 */
export function digestForGit(value) {
  if (typeof value !== "string" || value.length === 0) return value;
  if (/^[a-f0-9]{64}$/i.test(value)) return value;
  if (LITERAL_DIGESTS.has(value)) return value;
  if (/^\d+$/.test(value) && value.length <= 5) return value;
  if (/^https?:\/\/[^@\s]+$/.test(value)) return value;
  if (/^http:\/\/[a-z0-9.-]+(:\d+)?(\/[^@\s]*)?$/i.test(value)) return value;
  if (/^postgres(\+[a-z]+)?:\/\//i.test(value)) return sha256(value);
  if (/^Bearer /i.test(value)) return sha256(value);
  if (/^Authorization=Bearer /i.test(value)) return sha256(value);
  if (/^whsec_/i.test(value)) return sha256(value);
  if (/re_[A-Za-z0-9_]+/.test(value)) return sha256(value);
  if (/^smtps?:\/\//i.test(value)) return sha256(value);
  if (/BEGIN (?:RSA )?PRIVATE KEY/i.test(value)) return sha256(value);
  if (/^\{\s*"type"\s*:\s*"service_account"/.test(value.trim())) return sha256(value);
  if (value.length >= 24) return sha256(value);
  return value;
}
