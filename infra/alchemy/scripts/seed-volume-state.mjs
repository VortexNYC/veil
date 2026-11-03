// Seed adopt state for volumes whose Railway names predate Alchemy physical names.
// Run once after `pnpm plan` shows volume create/replace — then `pnpm deploy`.
// IDs from live production (2026-11-03).

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(fileURLToPath(new URL(".", import.meta.url)), "..");
const stage = process.env.ALCHEMY_STAGE ?? `live_${process.env.USER ?? "local"}`;
const stateDir = join(root, ".alchemy/state/VeilRailway", stage);

const projectId = "92578292-9436-407b-af3e-0f7b19a74289";
const environmentId = "a4b00b5d-533a-4701-b96a-bbf9bedd35c9";

const seeds = {
  PostgresVolume: {
    volumeId: "d374606b-2a0c-4805-8e72-568fc7fe8e30",
    volumeInstanceId: "8eded854-3fd9-4c95-8b8b-ef91ed2b066a",
    name: "postgres-volume",
    mountPath: "/var/lib/postgresql/data",
    serviceId: "88259809-8163-4ce4-9d62-dbb6eb8b3ab4",
    region: "sfo",
    sizeMB: 5000,
  },
  PwmVolume: {
    volumeId: "474002f6-6773-42a4-aaa2-767ee16d62b3",
    volumeInstanceId: "8cc694bb-86f0-4fc8-9a34-ec36c4cba588",
    name: "pwm-volume",
    mountPath: "/data",
    serviceId: "a51258d1-8742-49af-b1ca-5b8d677160ed",
    region: "sfo",
    sizeMB: 500,
  },
  VeilSpoolVolume: {
    volumeId: "f527debb-44d2-47ea-9bec-049d648fb549",
    volumeInstanceId: "f8083443-8cc1-4285-9e20-a3b3fd612b81",
    name: "veil-spool",
    mountPath: "/spool",
    serviceId: "7d8c7c08-c72b-4b18-9e0a-30bdfb740011",
    region: "sfo",
    sizeMB: 500,
  },
  VeilBackupsVolume: {
    volumeId: "952a3a50-b741-4337-bdfb-6be37814695f",
    volumeInstanceId: "867edcf2-73c8-4e3f-8cfc-dfa3115772f7",
    name: "veil-backups",
    mountPath: "/backups",
    serviceId: "f8b4b114-cef5-453d-8874-fd0fc52ba2e8",
    region: "sfo",
    sizeMB: 2000,
  },
};

mkdirSync(stateDir, { recursive: true });

for (const [logicalId, seed] of Object.entries(seeds)) {
  const path = join(stateDir, `${logicalId}.json`);
  let volumeInstanceId = seed.volumeInstanceId;
  try {
    const existing = JSON.parse(readFileSync(path, "utf8"));
    volumeInstanceId = existing.attr?.volumeInstanceId ?? volumeInstanceId;
  } catch {
    /* fresh */
  }

  const doc = {
    status: "updated",
    fqn: logicalId,
    logicalId,
    instanceId: volumeInstanceId.length > 0 ? volumeInstanceId : seed.volumeInstanceId,
    resourceType: "Railway.Volume",
    props: {
      project: {
        projectId,
        name: "veil",
        environmentId,
      },
      mountPath: seed.mountPath,
      region: seed.region,
      sizeMB: seed.sizeMB,
      service: { serviceId: seed.serviceId },
    },
    attr: {
      volumeId: seed.volumeId,
      volumeInstanceId: volumeInstanceId.length > 0 ? volumeInstanceId : seed.volumeInstanceId,
      name: seed.name,
      projectId,
      environmentId,
      mountPath: seed.mountPath,
      region: seed.region,
      serviceId: seed.serviceId,
      sizeMB: seed.sizeMB,
      createdAt: new Date().toISOString(),
    },
    providerVersion: 0,
    bindings: [],
    removalPolicy: "destroy",
  };
  writeFileSync(path, `${JSON.stringify(doc, null, 2)}\n`);
  process.stdout.write(`Wrote ${path}\n`);
}
