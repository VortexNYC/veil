/** Live Veil production Railway ids (import/adopt slice — not secrets). */
export const veilProjectId = "92578292-9436-407b-af3e-0f7b19a74289";
export const veilEnvironmentId = "a4b00b5d-533a-4701-b96a-bbf9bedd35c9";

export const services = {
  kratos: "38d057f6-8505-4877-935f-909ffcd1d65e",
  keto: "59ecdbd2-1940-4946-bcdf-409317ec3e8a",
  glue: "91358b0f-8887-4d70-9787-d5ddec615df5",
  hydra: "e3d9773a-bbce-4e5b-856c-64b1e8e6ec36",
  postgres: "88259809-8163-4ce4-9d62-dbb6eb8b3ab4",
  veil: "7d8c7c08-c72b-4b18-9e0a-30bdfb740011",
  veilMigrate: "a51258d1-8742-49af-b1ca-5b8d677160ed",
  veilSweep: "b45f9ae0-aa04-4261-8455-2e3a36d29b9a",
  veilBackup: "f8b4b114-cef5-453d-8874-fd0fc52ba2e8",
  veilWal: "f6aaa0ab-ebd6-4135-9cec-ab5997b95eb3",
  veilWalDr: "2d96f168-918b-4356-97f3-9ee0c9d8a438",
  veilAuditExport: "e6884f69-af2d-417d-8a1b-719d452180c4",
  veilMonitor: "e709fc87-f18a-44c8-8d33-16d9279fdef4",
} as const;

export const volumes = {
  postgres: "d374606b-2a0c-4805-8e72-568fc7fe8e30",
  pwm: "474002f6-6773-42a4-aaa2-767ee16d62b3",
  veilSpool: "f527debb-44d2-47ea-9bec-049d648fb549",
  veilBackups: "952a3a50-b741-4337-bdfb-6be37814695f",
} as const;

export const svc = (id: string) => ({ serviceId: id });
export const vol = (id: string) => ({ volumeId: id });
