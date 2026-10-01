import { defineRailway, image, github, postgres, preserve, project, service, volume } from "railway/iac";

// Full production plane: kratos, keto, glue, hydra, veil, Postgres.
// Omitting a service here and applying deletes it. Secrets stay preserve().
// Do not apply unless `railway config plan` is the change you intend.

export default defineRailway(() => {
  const Postgres = postgres("Postgres", { region: "sfo" });
  Postgres.networking = { privateNetworkEndpoint: "postgres" };
  const postgresVolume = volume("postgres-volume", { alerts: { usage: { "100": {}, "80": {}, "95": {} } }, allowOnlineResize: true, region: "sfo", sizeMB: 5000 });
  const kratos = service("kratos", {
    source: github("VortexNYC/veil", { branch: "main" }),
    build: { buildEnvironment: "V3", builder: "DOCKERFILE", dockerfilePath: "identity/kratos/Dockerfile" },
    start: "kratos serve -c /etc/config/kratos/kratos.yml --sqa-opt-out --watch-courier",
    healthcheck: "/health/ready",
    preDeploy: "kratos -c /etc/config/kratos/kratos.yml migrate sql -e --yes",
    replicas: { "sfo": 1 },
    domains: [{ domain: "accounts.veil.nyc", port: 4433 }],
    env: { COURIER_HTTP_REQUEST_CONFIG_AUTH_CONFIG_IN: preserve(), COURIER_HTTP_REQUEST_CONFIG_AUTH_CONFIG_NAME: preserve(), COURIER_HTTP_REQUEST_CONFIG_AUTH_CONFIG_VALUE: preserve(), COURIER_HTTP_REQUEST_CONFIG_AUTH_TYPE: preserve(), COURIER_SMTP_CONNECTION_URI: preserve(), DSN: preserve(), PORT: preserve(), RESEND_API_KEY: preserve(), RESEND_AUTHORIZATION: preserve(), SECRETS_CIPHER: preserve(), SECRETS_COOKIE: preserve(), SERVE_PUBLIC_PORT: preserve() },
  });
  const keto = service("keto", {
    source: github("VortexNYC/veil", { branch: "main" }),
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
  // Audit spool needs real persistence: on store failure the auditor
  // fsyncs events here (internal/audit/spool.go). Ephemeral fs would lose
  // them on host eviction mid-outage — the exact scenario the spool exists
  // for. Tiny by design; the relay drains it the moment Postgres returns.
  const veilSpool = volume("veil-spool", { region: "sfo", sizeMB: 500, allowOnlineResize: true });
  // Replica budget (VEIL-5, docs/scale.md): each origin replica holds
  // VEIL_PG_MAX_CONNS (default 20) + VEIL_PG_AUDIT_CONNS (default 2) backend
  // connections against the shared Postgres. At the stock
  // max_connections=100 that fits ~4 replicas with headroom for migrations
  // and ops verbs. Raising replicas past that ceiling requires PgBouncer
  // first — do not bump this number without checking the pool math.
  const veil = service("veil", {
    source: github("VortexNYC/veil", { branch: "main" }),
    start: "/veil mcp",
    healthcheck: "/health",
    healthcheckTimeout: 300,
    replicas: { "sfo": 1 },
    domains: [{ domain: "veil.nyc", port: 4461 }],
    volumeMounts: { "/spool": veilSpool },
    env: { OTEL_EXPORTER_OTLP_TRACES_ENDPOINT: preserve(), OTEL_EXPORTER_OTLP_TRACES_HEADERS: preserve(), OTEL_EXPORTER_OTLP_TRACES_PROTOCOL: preserve(), OTEL_RESOURCE_ATTRIBUTES: preserve(), OTEL_SERVICE_NAME: preserve(), PORT: preserve(), VEIL_AUDIT_SPOOL_DIR: "/spool", VEIL_HOME: preserve(), VEIL_HYDRA_ADMIN: preserve(), VEIL_HYDRA_CLIENT_ID: preserve(), VEIL_HYDRA_ISSUER: preserve(), VEIL_KEK: preserve(), VEIL_KETO_READ: preserve(), VEIL_KETO_WRITE: preserve(), VEIL_KRATOS_ADMIN: preserve(), VEIL_KRATOS_PUBLIC: preserve(), VEIL_MAIL_TOKEN: preserve(), VEIL_MAIL_URL: preserve(), VEIL_MCP_URL: preserve(), VEIL_MASTER_KEY: preserve(), VEIL_POSTGRES_DSN: "postgresql://${{Postgres.PGUSER}}:${{Postgres.PGPASSWORD}}@${{Postgres.PGHOST}}:${{Postgres.PGPORT}}/veil", VEIL_PG_MAX_CONNS: preserve(), VEIL_PG_AUDIT_CONNS: preserve(), VEIL_FREE_USE_CAP: preserve(), VEIL_VORTEX_API_URL: preserve(), VEIL_VORTEX_API_KEY: preserve(), VEIL_VORTEX_MERCHANT_ID: preserve(), VEIL_VORTEX_ENV: preserve(), VEIL_VORTEX_METER_ID: preserve(), VEIL_VORTEX_USAGE_EVENT: preserve(), VEIL_VORTEX_PRICE_ID: preserve(), VEIL_BILLING_WEBHOOK_SECRET: preserve(), VEIL_CHECKOUT_SUCCESS_URL: preserve(), VEIL_CHECKOUT_CANCEL_URL: preserve() },
  });
  const veilMigrate = service("veil-migrate", {
    source: github("VortexNYC/veil", { branch: "main" }),
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
    source: github("VortexNYC/veil", { branch: "main" }),
    build: { buildEnvironment: "V3", builder: "DOCKERFILE", dockerfilePath: "Dockerfile" },
    start: "/veil sweep --keep 24h",
    deploy: { restartPolicyType: "NEVER", cronSchedule: "0 * * * *" },
    replicas: { "sfo": 1 },
    env: {
      VEIL_POSTGRES_DSN: "postgresql://${{Postgres.PGUSER}}:${{Postgres.PGPASSWORD}}@${{Postgres.PGHOST}}:${{Postgres.PGPORT}}/veil",
    },
  });
  // Daily pg_dump of every real database to a dedicated volume — the same
  // format the restore drill in docs/backup-restore.md proves. The identity
  // plane is three databases, not one: kratos (humans), keto (org tuples),
  // and `railway` (hydra — its DSN targets the default db). Losing them
  // orphans humans even with a perfect vault restore.
  // Custom-format (-Fc) dumps compress and restore selectively; 14-day
  // local retention. The trailing sleep leaves a daily window where the
  // container is alive for `railway ssh`/`railway volume files` pulls —
  // the SFTP bridge only works while the service is running. After the
  // local dumps, each fresh artifact PUTs to R2 via the backup-ingest
  // worker (backup-ingest.veil.nyc) — the offsite copy that survives a
  // Railway-account loss. The KEK is escrowed separately; a dump plus
  // its KEK is a plaintext export, keep them apart.
  const veilBackups = volume("veil-backups", { region: "sfo", sizeMB: 2000, allowOnlineResize: true });
  const veilBackup = service("veil-backup", {
    source: github("VortexNYC/veil", { branch: "main" }),
    build: { buildEnvironment: "V3", builder: "DOCKERFILE", dockerfilePath: "Dockerfile.backup" },
    start: "sh -c 'rc=0; SYSID=$(psql \"$PGDUMP_BASE/veil\" -Atc \"SELECT system_identifier FROM pg_control_system()\" | tr -cd 0-9); [ -n \"$SYSID\" ] || { echo \"no system_identifier — cannot namespace archive\"; rc=1; }; [ -n \"$OFFSITE_TOKEN\" ] || { echo \"OFFSITE_TOKEN unset — offsite copy is not optional\"; rc=1; }; for d in veil kratos keto railway; do pg_dump \"$PGDUMP_BASE/$d\" -Fc -f /backups/$d-$(date +%F-%H%M).dump || rc=1; done; if pg_basebackup -D /backups/base -Ft -z -X stream -d \"$PGDUMP_BASE/postgres?replication=database\"; then mv /backups/base/base.tar.gz /backups/base-$(date +%F-%H%M).tar.gz; rm -f /backups/base/pg_wal.tar.gz; else rc=1; fi; if [ $rc -eq 0 ]; then for f in /backups/*-$(date +%F)-*.dump /backups/base-$(date +%F)-*.tar.gz; do [ -f \"$f\" ] || continue; curl -fsS -X PUT -H \"Authorization: Bearer $OFFSITE_TOKEN\" --data-binary \"@$f\" \"https://backup-ingest.veil.nyc/v1/arc-$SYSID/$(basename \"$f\")\" || rc=1; done; fi; find /backups \\( -name \"*.dump\" -o -name \"base-*.tar.gz\" \\) -mtime +14 -delete; if [ $rc -eq 0 ]; then psql \"$PGDUMP_BASE/veil\" -qc \"CREATE TABLE IF NOT EXISTS ops_heartbeat(name text primary key, at timestamptz not null); INSERT INTO ops_heartbeat(name,at) VALUES('\"'\"'backup'\"'\"',now()) ON CONFLICT(name) DO UPDATE SET at=now();\" || rc=1; fi; sleep 600; exit $rc'",
    deploy: { restartPolicyType: "NEVER", cronSchedule: "17 5 * * *" },
    replicas: { "sfo": 1 },
    volumeMounts: { "/backups": veilBackups },
    env: {
      PGDUMP_BASE: "postgresql://${{Postgres.PGUSER}}:${{Postgres.PGPASSWORD}}@${{Postgres.PGHOST}}:${{Postgres.PGPORT}}",
      OFFSITE_TOKEN: preserve(),
    },
  });
  // Continuous WAL archive — the PITR half of the backup plane. Streams WAL
  // over the replication protocol into physical slot wal_archive (retains
  // WAL server-side while this service is down; max_slot_wal_keep_size caps
  // what a dead archiver can pin) and pushes every segment plus the
  // in-flight .partial to R2 every ~15s. With the daily base-*.tar.gz from
  // veil-backup this restores to any second — loss window is the push
  // interval, not the dump interval. Always-on, unlike the cron siblings.
  const veilWal = service("veil-wal", {
    source: github("VortexNYC/veil", { branch: "main" }),
    build: { buildEnvironment: "V3", builder: "DOCKERFILE", dockerfilePath: "Dockerfile.wal" },
    start: "sh /wal-archive.sh",
    replicas: { "sfo": 1 },
    env: {
      PGDUMP_BASE: "postgresql://${{Postgres.PGUSER}}:${{Postgres.PGPASSWORD}}@${{Postgres.PGHOST}}:${{Postgres.PGPORT}}",
      OFFSITE_TOKEN: preserve(),
    },
  });
  // Standby WAL archiver in a second region (iad). Separate service because a
  // replication slot is single-writer — this one owns wal_archive_dr. It
  // streams the identical WAL bytes into the same arc-<sysid>/ prefix; R2
  // last-write-wins is fenced server-side (a shorter .partial can never
  // overwrite a longer one), so duplicate pushes are harmless. Covers an
  // sfo-side archiver host failure, not region loss — if sfo Postgres dies
  // the archive is already offsite and this receiver dies too.
  const veilWalDr = service("veil-wal-dr", {
    source: github("VortexNYC/veil", { branch: "main" }),
    build: { buildEnvironment: "V3", builder: "DOCKERFILE", dockerfilePath: "Dockerfile.wal" },
    start: "sh /wal-archive.sh",
    replicas: { "iad": 1 },
    env: {
      PGDUMP_BASE: "postgresql://${{Postgres.PGUSER}}:${{Postgres.PGPASSWORD}}@${{Postgres.PGHOST}}:${{Postgres.PGPORT}}",
      OFFSITE_TOKEN: preserve(),
      WAL_SLOT: "wal_archive_dr",
    },
  });
  // Hourly audit archive: every audit row past the export cursor is shipped
  // through backup-ingest.veil.nyc into R2 as append-only JSONL. Postgres is
  // the store of record, but it is one blast radius — this is the copy that
  // survives it, and the feed a per-org SIEM export builds on later. The
  // beat is stamped only on a fully-drained run; monitor pages when it goes
  // stale. Same bearer as the backup push — one worker, one token.
  const veilAuditExport = service("veil-audit-export", {
    source: github("VortexNYC/veil", { branch: "main" }),
    build: { buildEnvironment: "V3", builder: "DOCKERFILE", dockerfilePath: "Dockerfile" },
    start: "/veil audit-export",
    deploy: { restartPolicyType: "NEVER", cronSchedule: "42 * * * *" },
    replicas: { "sfo": 1 },
    env: {
      VEIL_POSTGRES_DSN: "postgresql://${{Postgres.PGUSER}}:${{Postgres.PGPASSWORD}}@${{Postgres.PGHOST}}:${{Postgres.PGPORT}}/veil",
      VEIL_AUDIT_INGEST_TOKEN: preserve(),
    },
  });
  // Dead-man's switch: every 15 min, check the backup/sweep beats,
  // audit_outbox lag, and public /ready — email on findings. A cron that
  // dies silently is worse than no cron; this is the thing that notices.
  const veilMonitor = service("veil-monitor", {
    source: github("VortexNYC/veil", { branch: "main" }),
    build: { buildEnvironment: "V3", builder: "DOCKERFILE", dockerfilePath: "Dockerfile" },
    start: "/veil monitor",
    deploy: { restartPolicyType: "NEVER", cronSchedule: "*/15 * * * *" },
    replicas: { "sfo": 1 },
    env: {
      VEIL_POSTGRES_DSN: "postgresql://${{Postgres.PGUSER}}:${{Postgres.PGPASSWORD}}@${{Postgres.PGHOST}}:${{Postgres.PGPORT}}/veil",
      VEIL_MAIL_URL: "${{veil.VEIL_MAIL_URL}}",
      VEIL_MAIL_TOKEN: "${{veil.VEIL_MAIL_TOKEN}}",
      VEIL_ALERT_TO: preserve(),
      VEIL_READY_URL: "https://veil.nyc/ready",
    },
  });
  const glue = service("glue", {
    source: github("VortexNYC/veil", { branch: "main" }),
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
    resources: [kratos, keto, veil, Postgres, glue, hydra, postgresVolume, veilVolume, veilSpool, veilMigrate, veilSweep, veilBackup, veilBackups, veilMonitor, veilAuditExport, veilWal, veilWalDr],
  });
});
