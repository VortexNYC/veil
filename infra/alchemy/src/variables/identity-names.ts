/** Service-scoped identity env (migrate from `.railway/railway.ts` preserve()). */
import { railwayPlainEnvKey } from "./railway-env-keys.ts";

export const identityServiceVarNames = {
  kratos: [
    "COURIER_HTTP_REQUEST_CONFIG_AUTH_CONFIG_IN",
    "COURIER_HTTP_REQUEST_CONFIG_AUTH_CONFIG_NAME",
    "COURIER_HTTP_REQUEST_CONFIG_AUTH_CONFIG_VALUE",
    "COURIER_HTTP_REQUEST_CONFIG_AUTH_TYPE",
    "COURIER_SMTP_CONNECTION_URI",
    "DSN",
    "PORT",
    "SECRETS_CIPHER",
    "SECRETS_COOKIE",
    "SERVE_PUBLIC_PORT",
  ],
  keto: ["DSN"],
  hydra: [
    "DSN",
    "HYDRA_SYSTEM_SECRET",
    "OIDC_SUBJECT_IDENTIFIERS_PAIRWISE_SALT",
    "OIDC_SUBJECT_IDENTIFIERS_SUPPORTED_TYPES",
    "PORT",
    "SECRETS_SYSTEM",
    "SERVE_ADMIN_HOST",
    "SERVE_ADMIN_PORT",
    "SERVE_COOKIES_SAME_SITE_MODE",
    "SERVE_PUBLIC_CORS_ALLOWED_ORIGINS",
    "SERVE_PUBLIC_CORS_ALLOW_CREDENTIALS",
    "SERVE_PUBLIC_CORS_ENABLED",
    "SERVE_PUBLIC_HOST",
    "SERVE_PUBLIC_PORT",
    "URLS_CONSENT",
    "URLS_LOGIN",
    "URLS_LOGOUT",
    "URLS_SELF_ISSUER",
  ],
  glue: [
    "BOOTSTRAP_EMAIL",
    "BOOTSTRAP_PASSWORD",
    "BROKER_REDIRECT_URL",
    "HYDRA_ADMIN_URL",
    "HYDRA_CLIENT_ID",
    "KETO_READ_URL",
    "KETO_WRITE_URL",
    "KRATOS_ADMIN",
    "KRATOS_PUBLIC",
    "PORT",
  ],
} as const;

export type IdentityRailwayService = keyof typeof identityServiceVarNames;

export const identityRailwayServices = Object.keys(
  identityServiceVarNames,
) as IdentityRailwayService[];

/** Alchemy LogicalId prefix per identity service. */
export const identityLogicalPrefix: Record<IdentityRailwayService, string> = {
  kratos: "Kratos",
  keto: "Keto",
  hydra: "Hydra",
  glue: "Glue",
};

/** Process env key for plan/deploy digests (`load-identity-env.mjs`). */
export const identityPlainEnvKey = (
  railwayService: IdentityRailwayService,
  varName: string,
) => railwayPlainEnvKey(railwayService, varName);
