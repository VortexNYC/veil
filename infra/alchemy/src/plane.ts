import * as Railway from "@alchemy.run/railway";
import type { Project } from "@alchemy.run/railway/Project";
import * as Effect from "effect/Effect";
import { pgDumpBase, pgVeilDsn } from "./railway-env.ts";
import { defineProductionNetwork } from "./network.ts";
import {
  defineProductionVariables,
  defineIdentityVariables,
  defineBrokerVariables,
  offsiteTokenRef,
  auditIngestTokenRef,
} from "./variables/index.ts";

const VEIL_REPO = "VortexNYC/veil";
const POSTGRES_IMAGE = "ghcr.io/railwayapp-templates/postgres-ssl:18";

/** GitHub branch: set `VEIL_RAILWAY_REPO_BRANCH=main` when OAuth has repo access (not account API tokens). */
const REPO = {
  repo: VEIL_REPO,
  ...(process.env.VEIL_RAILWAY_REPO_BRANCH?.trim()
    ? { branch: process.env.VEIL_RAILWAY_REPO_BRANCH.trim() }
    : {}),
} as const;

export const defineProductionPlane = (veilProject: Project) =>
  Effect.gen(function* () {
    const postgres = yield* Railway.Service("Postgres", {
      project: veilProject,
      name: "Postgres",
      image: POSTGRES_IMAGE,
      region: "sfo",
      publicDomain: false,
    });

    const network = yield* defineProductionNetwork(veilProject, postgres);

    const variables = yield* defineProductionVariables(veilProject);
    const offsiteShared = variables.offsite.shared;
    const auditIngestShared = variables.auditIngest.shared;
    const monitorRefs = variables.monitor.refs;

    const hydra = yield* Railway.Service("Hydra", {
      project: veilProject,
      name: "hydra",
      image: "oryd/hydra:v26.2.0",
      port: 4444,
      region: "sfo",
      startCommand: "hydra serve all --sqa-opt-out",
      preDeploy: { command: "hydra migrate sql -e --yes" },
      publicDomain: false,
    });

    const kratos = yield* Railway.Service("Kratos", {
      project: veilProject,
      name: "kratos",
      ...REPO,
      dockerfilePath: "identity/kratos/Dockerfile",
      region: "sfo",
      startCommand: "kratos serve -c /etc/config/kratos/kratos.yml --sqa-opt-out --watch-courier",
      healthcheck: "/health/ready",
      preDeploy: { command: "kratos -c /etc/config/kratos/kratos.yml migrate sql -e --yes" },
      publicDomain: false,
    });

    const keto = yield* Railway.Service("Keto", {
      project: veilProject,
      name: "keto",
      ...REPO,
      dockerfilePath: "identity/keto/Dockerfile",
      region: "sfo",
      startCommand: "keto serve -c /etc/config/keto/keto.yml",
      preDeploy: { command: "keto -c /etc/config/keto/keto.yml migrate up -y" },
      publicDomain: false,
    });

    const glue = yield* Railway.Service("Glue", {
      project: veilProject,
      name: "glue",
      ...REPO,
      dockerfilePath: "Dockerfile",
      region: "sfo",
      startCommand: "/identity-glue",
      port: 4456,
      publicDomain: false,
    });

    const identityVariables = yield* defineIdentityVariables(veilProject, {
      hydra,
      kratos,
      keto,
      glue,
    });

    const veilOrigin = yield* Railway.Service("VeilOrigin", {
      project: veilProject,
      name: "veil",
      ...REPO,
      port: 4461,
      region: "sfo",
      startCommand: "/veil mcp",
      healthcheck: "/health",
      healthcheckTimeout: 300,
      publicDomain: false,
      env: {
        VEIL_AUDIT_SPOOL_DIR: "/spool",
        VEIL_KEYSVC_URL: "https://keys.veil.nyc",
        VEIL_POSTGRES_DSN: pgVeilDsn(postgres),
      },
    });

    const brokerVariables = yield* defineBrokerVariables(veilProject, veilOrigin);

    const veilMigrate = yield* Railway.Service("VeilMigrate", {
      project: veilProject,
      name: "veil-migrate",
      ...REPO,
      dockerfilePath: "Dockerfile",
      region: "sfo",
      startCommand: "/veil migrate",
      restartPolicyType: "NEVER",
      publicDomain: false,
      env: {
        VEIL_HOME: "/data",
        VEIL_POSTGRES_DSN: pgVeilDsn(postgres),
      },
    });

    const veilSweep = yield* Railway.Service("VeilSweep", {
      project: veilProject,
      name: "veil-sweep",
      ...REPO,
      dockerfilePath: "Dockerfile",
      region: "sfo",
      startCommand: "/veil sweep --keep 24h",
      restartPolicyType: "NEVER",
      cronSchedule: "0 * * * *",
      publicDomain: false,
      env: {
        VEIL_POSTGRES_DSN: pgVeilDsn(postgres),
      },
    });

    const veilBackup = yield* Railway.Service("VeilBackup", {
      project: veilProject,
      name: "veil-backup",
      ...REPO,
      dockerfilePath: "Dockerfile.backup",
      region: "sfo",
      startCommand: "sh /backup.sh",
      restartPolicyType: "NEVER",
      cronSchedule: "17 5 * * *",
      publicDomain: false,
      env: {
        PGDUMP_BASE: pgDumpBase(postgres),
        PGSSLMODE: "disable",
        OFFSITE_TOKEN: offsiteTokenRef,
      },
    });

    const veilWal = yield* Railway.Service("VeilWal", {
      project: veilProject,
      name: "veil-wal",
      ...REPO,
      dockerfilePath: "Dockerfile.wal",
      region: "sfo",
      startCommand: "sh /wal-archive.sh",
      publicDomain: false,
      env: {
        PGDUMP_BASE: pgDumpBase(postgres),
        PGSSLMODE: "disable",
        OFFSITE_TOKEN: offsiteTokenRef,
      },
    });

    const veilWalDr = yield* Railway.Service("VeilWalDr", {
      project: veilProject,
      name: "veil-wal-dr",
      ...REPO,
      dockerfilePath: "Dockerfile.wal",
      region: "iad",
      startCommand: "sh /wal-archive.sh",
      publicDomain: false,
      env: {
        PGDUMP_BASE: pgDumpBase(postgres),
        PGSSLMODE: "disable",
        WAL_SLOT: "wal_archive_dr",
        OFFSITE_TOKEN: offsiteTokenRef,
      },
    });

    const veilAuditExport = yield* Railway.Service("VeilAuditExport", {
      project: veilProject,
      name: "veil-audit-export",
      ...REPO,
      dockerfilePath: "Dockerfile",
      region: "sfo",
      startCommand: "/veil audit-export",
      restartPolicyType: "NEVER",
      cronSchedule: "42 * * * *",
      publicDomain: false,
      env: {
        VEIL_POSTGRES_DSN: pgVeilDsn(postgres),
        VEIL_AUDIT_INGEST_TOKEN: auditIngestTokenRef,
      },
    });

    const veilMonitor = yield* Railway.Service("VeilMonitor", {
      project: veilProject,
      name: "veil-monitor",
      ...REPO,
      dockerfilePath: "Dockerfile",
      region: "sfo",
      startCommand: "/veil monitor",
      restartPolicyType: "NEVER",
      cronSchedule: "*/15 * * * *",
      publicDomain: false,
      env: {
        VEIL_POSTGRES_DSN: pgVeilDsn(postgres),
        VEIL_MAIL_URL: Railway.ref(veilOrigin, "VEIL_MAIL_URL"),
        VEIL_MAIL_TOKEN: Railway.ref(veilOrigin, "VEIL_MAIL_TOKEN"),
        VEIL_READY_URL: "https://veil.nyc/ready",
        VEIL_ALERT_TO: monitorRefs.VEIL_ALERT_TO,
        VEIL_NTFY_TOPIC: monitorRefs.VEIL_NTFY_TOPIC,
        VEIL_APNS_KEY: monitorRefs.VEIL_APNS_KEY,
        VEIL_APNS_KEY_PATH: monitorRefs.VEIL_APNS_KEY_PATH,
        VEIL_APNS_KEY_ID: monitorRefs.VEIL_APNS_KEY_ID,
        VEIL_APNS_TEAM_ID: monitorRefs.VEIL_APNS_TEAM_ID,
        VEIL_APNS_BUNDLE: monitorRefs.VEIL_APNS_BUNDLE,
        VEIL_APNS_PROD: monitorRefs.VEIL_APNS_PROD,
        POSTHOG_PROJECT_KEY: monitorRefs.POSTHOG_PROJECT_KEY,
      },
    });

    const domainVeil = yield* Railway.CustomDomain("DomainVeilNyc", {
      service: veilOrigin,
      environment: veilProject,
      domain: "veil.nyc",
      targetPort: 4461,
    });

    const domainHydra = yield* Railway.CustomDomain("DomainIdVeilNyc", {
      service: hydra,
      environment: veilProject,
      domain: "id.veil.nyc",
      targetPort: 4444,
    });

    const domainKratos = yield* Railway.CustomDomain("DomainAccountsVeilNyc", {
      service: kratos,
      environment: veilProject,
      domain: "accounts.veil.nyc",
      targetPort: 4433,
    });

    const domainGlue = yield* Railway.CustomDomain("DomainConsentVeilNyc", {
      service: glue,
      environment: veilProject,
      domain: "consent.veil.nyc",
      targetPort: 4456,
    });

    const postgresVolume = yield* Railway.Volume("PostgresVolume", {
      project: veilProject,
      mountPath: "/var/lib/postgresql/data",
      service: postgres,
      region: "sfo",
      sizeMB: 5000,
    });

    const pwmVolume = yield* Railway.Volume("PwmVolume", {
      project: veilProject,
      mountPath: "/data",
      service: veilMigrate,
      region: "sfo",
      sizeMB: 500,
    });

    const veilSpoolVolume = yield* Railway.Volume("VeilSpoolVolume", {
      project: veilProject,
      mountPath: "/spool",
      service: veilOrigin,
      region: "sfo",
      sizeMB: 500,
    });

    const veilBackupsVolume = yield* Railway.Volume("VeilBackupsVolume", {
      project: veilProject,
      mountPath: "/backups",
      service: veilBackup,
      region: "sfo",
      sizeMB: 2000,
    });

    const identity = yield* Railway.Group("Identity", {
      project: veilProject,
      name: "Identity",
      resources: [hydra, kratos, keto, glue, ...identityVariables.resources],
    });

    const store = yield* Railway.Group("Store", {
      project: veilProject,
      name: "Store",
      resources: [
        postgres,
        network.mesh,
        network.postgresEndpoint,
        postgresVolume,
        pwmVolume,
        veilSpoolVolume,
      ],
    });

    const broker = yield* Railway.Group("Broker", {
      project: veilProject,
      name: "Broker",
      resources: [veilOrigin, veilMigrate, veilSweep, ...brokerVariables.resources],
    });

    const backupPlane = yield* Railway.Group("BackupPlane", {
      project: veilProject,
      name: "Backup plane",
      resources: [
        offsiteShared,
        auditIngestShared,
        ...variables.monitor.resources,
        veilBackup,
        veilBackupsVolume,
        veilWal,
        veilWalDr,
        veilAuditExport,
        veilMonitor,
      ],
    });

    return {
      postgres,
      network,
      variables,
      hydra,
      kratos,
      keto,
      glue,
      veilOrigin,
      veilMigrate,
      veilSweep,
      veilBackup,
      veilWal,
      veilWalDr,
      veilAuditExport,
      veilMonitor,
      domains: { domainVeil, domainHydra, domainKratos, domainGlue },
      volumes: { postgresVolume, pwmVolume, veilSpoolVolume, veilBackupsVolume },
      groups: { identity, store, broker, backupPlane },
    };
  });
