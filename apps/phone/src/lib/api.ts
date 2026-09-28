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
  login?: string;
  has_totp?: boolean;
  archived?: boolean;
};

export type FillEntry = {
  uuid: string;
  name: string;
  login: string;
  password: string;
  totp?: string;
};

export const listItems = () => call<{ items: Item[] }>("/v1/items").then((r) => r.items);

export const fillLogin = (uuid: string) =>
  call<{ entries: FillEntry[] }>("/v1/fill/logins", {
    method: "POST",
    body: JSON.stringify({ uuid, mintTotp: true }),
  }).then((r) => r.entries[0] ?? null);

export const addLogin = (input: { name: string; uri?: string; login?: string; secret: string }) =>
  call<unknown>("/v1/items", { method: "POST", body: JSON.stringify(input) });

/** Signup/invite acceptance: plants the humans row the broker keys on. */
export const provision = () => call<unknown>("/v1/provision", { method: "POST" });
