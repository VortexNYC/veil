import { originAPI, signOut, storedToken } from "./auth";

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const tok = await storedToken();
  if (!tok) throw new ApiError(401, "signed out");
  const res = await fetch(originAPI + path, {
    ...init,
    headers: {
      Authorization: `Bearer ${tok}`,
      "Content-Type": "application/json",
      ...init?.headers,
    },
  });
  if (res.status === 401) {
    await signOut();
    throw new ApiError(401, "session expired — sign in again");
  }
  const raw: unknown = await res.json().catch(() => ({}));
  if (!res.ok) {
    const msg =
      typeof raw === "object" && raw !== null && "error" in raw && typeof raw.error === "string"
        ? raw.error
        : `request failed (${res.status})`;
    throw new ApiError(res.status, msg);
  }
  return raw as T;
}

export type Item = {
  id: string;
  name: string;
  kind?: string;
  uris?: string[];
  tags?: string[];
  login?: string;
  has_totp?: boolean;
  archived?: boolean;
  cred_id?: string;
  rp_id?: string;
  user_handle?: string;
};

export type FillEntry = {
  uuid: string;
  name: string;
  login: string;
  password: string;
  totp?: string;
  notes?: string;
  card?: {
    number?: string;
    exp_month?: string;
    exp_year?: string;
    cvv?: string;
    holder?: string;
  };
  identity?: {
    given_name?: string;
    family_name?: string;
    address?: string;
    city?: string;
    region?: string;
    postal?: string;
    country?: string;
    phone?: string;
    email?: string;
  };
};

export const listItems = () => call<{ items: Item[] }>("/v1/items").then((r) => r.items);

export const fillLogin = async (uuid: string, wantTotp: boolean): Promise<FillEntry | null> => {
  try {
    return await call<FillEntry>(`/v1/items/${encodeURIComponent(uuid)}/reveal`, {
      method: "POST",
      body: JSON.stringify({ mint_totp: wantTotp }),
    });
  } catch {
    const r = await call<{ entries: FillEntry[] }>("/v1/fill/logins", {
      method: "POST",
      body: JSON.stringify(wantTotp ? { uuid, mintTotp: true } : { uuid }),
    });
    return r.entries[0] ?? null;
  }
};

export const mintTotp = (uuid: string) =>
  call<{ code: string }>("/v1/fill/totp", {
    method: "POST",
    body: JSON.stringify({ uuid }),
  }).then((r) => r.code);

export const addLogin = (input: {
  name: string;
  uri?: string;
  login?: string;
  secret: string;
  totp_seed?: string;
}) => call<unknown>("/v1/items", { method: "POST", body: JSON.stringify(input) });

/** Signup/invite acceptance: plants the humans row the broker keys on. */
export const provision = () => call<unknown>("/v1/provision", { method: "POST" });

export type ApprovalRequest = {
  id: string;
  agent_id: string;
  item_id: string;
  grant_id: string;
  action: string;
  status: string;
  created_at: string;
  expires_at: string;
  resolved_at?: string;
  resolved_by?: string;
  approval_id?: string;
};

export const listOpenRequests = () =>
  call<{ requests: ApprovalRequest[] }>("/v1/requests?status=open").then(
    (r) => r.requests ?? [],
  );

export const resolveRequest = (id: string, verb: "approve" | "deny") =>
  call<ApprovalRequest>(`/v1/requests/${encodeURIComponent(id)}/${verb}`, {
    method: "POST",
    body: verb === "approve" ? JSON.stringify({ ttl: "10m" }) : undefined,
  });

/** Register this device in the linked-devices inventory (trust surface). */
export const registerLinkedDevice = (id: string, label: string, pubkeyB64?: string) =>
  call<unknown>("/v1/linked-devices", {
    method: "POST",
    body: JSON.stringify({ id, label, platform: "ios", pubkey: pubkeyB64 }),
  });

/** Register the OS push token so asks and ops pages reach the lock screen. */
export const registerDevice = (platform: "apns" | "fcm", token: string, label: string) =>
  call<unknown>("/v1/devices", {
    method: "POST",
    body: JSON.stringify({ platform, token, label }),
  });

// Device-links — old-device-approve custody handoff (sealed ciphertext only).
export interface DeviceLink { token: string; expires_at: string; sealed?: string }
export const createDeviceLink = (label: string, pubkey: number[]) =>
  call<DeviceLink>("/v1/device-links", { method: "POST", body: JSON.stringify({ label, pubkey }) });
export const approveDeviceLink = (token: string, sealed: number[]) =>
  call<unknown>(`/v1/device-links/${token}/approve`, { method: "POST", body: JSON.stringify({ sealed }) });
export const claimDeviceLink = (token: string) =>
  call<DeviceLink>(`/v1/device-links/${token}/claim`, { method: "POST" });
export const getDeviceLink = (token: string) =>
  call<DeviceLink & { pubkey: number[]; label: string }>(`/v1/device-links/${token}`);
