/** Non-secret broker defaults when Railway/escrow omit them (see docs/scale.md, docs/billing.md). */
export const brokerEnvDefaults = {
  VEIL_PG_MAX_CONNS: "20",
  VEIL_PG_AUDIT_CONNS: "2",
  VEIL_VORTEX_USAGE_EVENT: "credential_use",
  VEIL_APNS_KEY_PATH: "",
};

/** Service-scoped vars allowed to adopt as empty string. */
export const brokerOptionalEmptyVarNames = new Set(["VEIL_APNS_KEY_PATH"]);
