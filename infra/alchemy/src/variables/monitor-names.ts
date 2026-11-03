/** Shared monitor env vars (migrate from veil-monitor service preserve()). */
export const monitorSharedVarNames = [
  "VEIL_ALERT_TO",
  "VEIL_NTFY_TOPIC",
  "VEIL_APNS_KEY",
  "VEIL_APNS_KEY_PATH",
  "VEIL_APNS_KEY_ID",
  "VEIL_APNS_TEAM_ID",
  "VEIL_APNS_BUNDLE",
  "VEIL_APNS_PROD",
  "POSTHOG_PROJECT_KEY",
] as const;

export type MonitorSharedVarName = (typeof monitorSharedVarNames)[number];

export const monitorLogicalId = (name: MonitorSharedVarName) =>
  `Monitor${name.replace(/[^A-Za-z0-9]/g, "")}`;
