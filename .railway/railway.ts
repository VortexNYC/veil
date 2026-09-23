import { defineRailway, image, postgres, preserve, project, service, volume } from "railway/iac";

// Full production plane: kratos, keto, glue, hydra, veil, Postgres.
// Omitting a service here and applying deletes it. Secrets stay preserve().
// Do not apply unless `railway config plan` is the change you intend.

export default defineRailway(() => {
  const Postgres = postgres("Postgres", { region: "sfo" });
  Postgres.networking = { privateNetworkEndpoint: "postgres" };
  const postgresVolume = volume("postgres-volume", { alerts: { usage: { "100": {}, "80": {}, "95": {} } }, allowOnlineResize: true, region: "sfo", sizeMB: 500 });
  const kratos = service("kratos", {
    build: { buildEnvironment: "V3", builder: "DOCKERFILE", dockerfilePath: "identity/kratos/Dockerfile" },
    start: "kratos serve -c /etc/config/kratos/kratos.yml --sqa-opt-out --watch-courier",
    healthcheck: "/health/ready",
    preDeploy: "kratos -c /etc/config/kratos/kratos.yml migrate sql -e --yes",
    replicas: { "sfo": 1 },
    domains: [{ domain: "accounts.veil.nyc", port: 4433 }],
    env: { COURIER_HTTP_REQUEST_CONFIG_AUTH_CONFIG_IN: preserve(), COURIER_HTTP_REQUEST_CONFIG_AUTH_CONFIG_NAME: preserve(), COURIER_HTTP_REQUEST_CONFIG_AUTH_CONFIG_VALUE: preserve(), COURIER_HTTP_REQUEST_CONFIG_AUTH_TYPE: preserve(), COURIER_SMTP_CONNECTION_URI: preserve(), DSN: preserve(), PORT: preserve(), RESEND_API_KEY: preserve(), RESEND_AUTHORIZATION: preserve(), SECRETS_CIPHER: preserve(), SECRETS_COOKIE: preserve(), SERVE_PUBLIC_PORT: preserve() },
  });
  const keto = service("keto", {
    build: { buildEnvironment: "V3", builder: "DOCKERFILE", dockerfilePath: "identity/keto/Dockerfile" },
    start: "keto serve -c /etc/config/keto/keto.yml",
    preDeploy: "keto -c /etc/config/keto/keto.yml migrate up -y",
    replicas: { "sfo": 1 },
    env: { DSN: preserve() },
  });
  // Postgres is the store of record (cutover done 2026-09-18). veil runs
  // stateless on VEIL_POSTGRES_DSN; the `veil` database lives in the shared
  // Postgres instance. veil-migrate keeps the sqlite volume mounted at /data
  // as the rollback path — redeploy it to re-run the idempotent sync.
  // Rollback: move the volumeMount back to veil, delete VEIL_POSTGRES_DSN,
  // redeploy. Drop pwm-volume only after the rollback window closes.
  // VEIL_MASTER_KEY MUST stay live on veil (preserve() cannot read sealed
  // variables — an apply will silently drop one). Local recovery copy:
  // ~/.config/vortex/veil-master-key and ~/.veil/wraps on the founder's Mac.
  // The Railway volume keeps its original name — the API has no volume
  // rename, and renaming the resource would provision an empty volume.
  const veilVolume = volume("pwm-volume", { region: "sfo", sizeMB: 500, allowOnlineResize: true });
  // Replica budget (VEIL-5, docs/scale.md): each origin replica holds
  // VEIL_PG_MAX_CONNS (default 20) + VEIL_PG_AUDIT_CONNS (default 2) backend
  // connections against the shared Postgres. At the stock
  // max_connections=100 that fits ~4 replicas with headroom for migrations
  // and ops verbs. Raising replicas past that ceiling requires PgBouncer
  // first — do not bump this number without checking the pool math.
  const veil = service("veil", {
    start: "/veil mcp",
    healthcheck: "/health",
    healthcheckTimeout: 300,
    replicas: { "sfo": 1 },
    domains: [{ domain: "veil.nyc", port: 4461 }],
    env: { OTEL_EXPORTER_OTLP_TRACES_ENDPOINT: preserve(), OTEL_EXPORTER_OTLP_TRACES_HEADERS: preserve(), OTEL_EXPORTER_OTLP_TRACES_PROTOCOL: preserve(), OTEL_RESOURCE_ATTRIBUTES: preserve(), OTEL_SERVICE_NAME: preserve(), PORT: preserve(), VEIL_HOME: preserve(), VEIL_HYDRA_ADMIN: preserve(), VEIL_HYDRA_CLIENT_ID: preserve(), VEIL_HYDRA_ISSUER: preserve(), VEIL_KEK: preserve(), VEIL_KETO_READ: preserve(), VEIL_KETO_WRITE: preserve(), VEIL_KRATOS_ADMIN: preserve(), VEIL_KRATOS_PUBLIC: preserve(), VEIL_MCP_URL: preserve(), VEIL_MASTER_KEY: preserve(), VEIL_POSTGRES_DSN: "postgresql://${{Postgres.PGUSER}}:${{Postgres.PGPASSWORD}}@${{Postgres.PGHOST}}:${{Postgres.PGPORT}}/veil", VEIL_PG_MAX_CONNS: preserve(), VEIL_PG_AUDIT_CONNS: preserve() },
  });
  const veilMigrate = service("veil-migrate", {
    build: { buildEnvironment: "V3", builder: "DOCKERFILE", dockerfilePath: "Dockerfile" },
    start: "/veil migrate",
    deploy: { restartPolicyType: "NEVER" },
    replicas: { "sfo": 1 },
    volumeMounts: { "/data": veilVolume },
    env: {
      VEIL_HOME: "/data",
      VEIL_POSTGRES_DSN: "postgresql://${{Postgres.PGUSER}}:${{Postgres.PGPASSWORD}}@${{Postgres.PGHOST}}:${{Postgres.PGPORT}}/veil",
    },
  });
  // Hourly cleanup of terminally-expired sessions, grants, and approvals
  // (24h keep window). Also creates audit month partitions ~3 months ahead
  // and detaches partitions older than --audit-keep (default 90d) into
  // standalone tables for archival — detach, never drop. Deletes only;
  // never decrypts, so no master key.
  const veilSweep = service("veil-sweep", {
    build: { buildEnvironment: "V3", builder: "DOCKERFILE", dockerfilePath: "Dockerfile" },
    start: "/veil sweep --keep 24h",
    deploy: { restartPolicyType: "NEVER", cronSchedule: "0 * * * *" },
    replicas: { "sfo": 1 },
    env: {
      VEIL_POSTGRES_DSN: "postgresql://${{Postgres.PGUSER}}:${{Postgres.PGPASSWORD}}@${{Postgres.PGHOST}}:${{Postgres.PGPORT}}/veil",
    },
  });
  // Daily pg_dump of the `veil` database to a dedicated volume — the same
  // format the restore drill in docs/backup-restore.md proves.
  // Custom-format (-Fc) dumps compress and restore selectively; 14-day
  // local retention. Pull a copy offsite with `railway files` — R2/offsite
  // replication is the post-alpha step, tracked in docs/backup-restore.md.
  const veilBackups = volume("veil-backups", { region: "sfo", sizeMB: 2000, allowOnlineResize: true });
  const veilBackup = service("veil-backup", {
    source: image("postgres:16-alpine"),
    start: "sh -c 'pg_dump \"$PGDUMP_DSN\" -Fc -f /backups/veil-$(date +%F-%H%M).dump && pg_dump \"$PGDUMP_IDENTITY_DSN\" -Fc -f /backups/identity-$(date +%F-%H%M).dump && find /backups -name \"*.dump\" -mtime +14 -delete'",
    deploy: { restartPolicyType: "NEVER", cronSchedule: "17 5 * * *" },
    replicas: { "sfo": 1 },
    volumeMounts: { "/backups": veilBackups },
    env: {
      PGDUMP_DSN: "postgresql://${{Postgres.PGUSER}}:${{Postgres.PGPASSWORD}}@${{Postgres.PGHOST}}:${{Postgres.PGPORT}}/veil",
      // Kratos/Keto/Hydra state — losing it orphans humans even with a
      // perfect vault restore (docs/backup-restore.md).
      PGDUMP_IDENTITY_DSN: "postgresql://${{Postgres.PGUSER}}:${{Postgres.PGPASSWORD}}@${{Postgres.PGHOST}}:${{Postgres.PGPORT}}/identity",
    },
  });
  const glue = service("glue", {
    build: { buildEnvironment: "V3", builder: "DOCKERFILE", dockerfilePath: "Dockerfile" },
    start: "/identity-glue",
    replicas: { "sfo": 1 },
    domains: [{ domain: "consent.veil.nyc", port: 4456 }],
    env: { BOOTSTRAP_EMAIL: preserve(), BOOTSTRAP_PASSWORD: preserve(), BROKER_REDIRECT_URL: preserve(), HYDRA_ADMIN_URL: preserve(), HYDRA_CLIENT_ID: preserve(), KETO_READ_URL: preserve(), KETO_WRITE_URL: preserve(), KRATOS_ADMIN: preserve(), KRATOS_PUBLIC: preserve(), PORT: preserve() },
  });
  const hydra = service("hydra", {
    source: image("oryd/hydra:v26.2.0"),
    start: "hydra serve all --sqa-opt-out",
    preDeploy: "hydra migrate sql -e --yes",
    replicas: { "sfo": 1 },
    domains: [{ domain: "id.veil.nyc", port: 4444 }],
    env: { DSN: preserve(), HYDRA_SYSTEM_SECRET: preserve(), OIDC_SUBJECT_IDENTIFIERS_PAIRWISE_SALT: preserve(), OIDC_SUBJECT_IDENTIFIERS_SUPPORTED_TYPES: preserve(), PORT: preserve(), SECRETS_SYSTEM: preserve(), SERVE_ADMIN_HOST: preserve(), SERVE_ADMIN_PORT: preserve(), SERVE_COOKIES_SAME_SITE_MODE: preserve(), SERVE_PUBLIC_CORS_ALLOWED_ORIGINS: preserve(), SERVE_PUBLIC_CORS_ALLOW_CREDENTIALS: preserve(), SERVE_PUBLIC_CORS_ENABLED: preserve(), SERVE_PUBLIC_HOST: preserve(), SERVE_PUBLIC_PORT: preserve(), URLS_CONSENT: preserve(), URLS_LOGIN: preserve(), URLS_LOGOUT: preserve(), URLS_SELF_ISSUER: preserve() },
  });

  return project("veil", {
    resources: [kratos, keto, veil, Postgres, glue, hydra, postgresVolume, veilVolume, veilMigrate, veilSweep, veilBackup, veilBackups],
  });
});
