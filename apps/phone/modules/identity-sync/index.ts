import { Platform } from "react-native";
import { requireNativeModule } from "expo";

export type HandoffItem = {
  uuid: string;
  name: string;
  login: string;
  uris: string[];
  kind: string;
  credId?: string;
  rpId?: string;
  userHandle?: string;
};

export type AutofillAuth = {
  token: string;
  refresh: string;
};

type NativeIdentitySync = {
  syncAutofill(
    token: string,
    refresh: string,
    origin: string,
    issuer: string,
    items: HandoffItem[],
  ): Promise<void>;
  refreshAutofill(token: string, refresh: string): Promise<void>;
  autofillAuth(): Promise<AutofillAuth | null>;
  clearAutofill(): Promise<void>;
};

function native(): NativeIdentitySync | null {
  if (Platform.OS === "web") return null;
  try {
    return requireNativeModule<NativeIdentitySync>("IdentitySync");
  } catch {
    return null;
  }
}

/** Publish the credential handoff the AutoFill provider reads: bearer +
 *  refresh tokens plus item metadata (never secrets). iOS writes the shared
 *  app-group container + pushes QuickType identities; Android drops the file
 *  in filesDir where VeilAutofillService reads it in-process. The refresh
 *  token lets the appex remint an expired id_token without a sign-in. */
export async function syncAutofill(
  token: string,
  refresh: string,
  origin: string,
  issuer: string,
  items: HandoffItem[],
): Promise<void> {
  await native()?.syncAutofill(token, refresh, origin, issuer, items);
}

/** Rewrite just the auth pair after a remint — items stay untouched. */
export async function refreshAutofill(token: string, refresh: string): Promise<void> {
  await native()?.refreshAutofill(token, refresh);
}

/** The auth pair the appex currently holds — the app falls back to it when
 *  its own refresh token rotated out from under it. */
export async function autofillAuth(): Promise<AutofillAuth | null> {
  return (await native()?.autofillAuth()) ?? null;
}

/** Drop the handoff + every synced identity — call on sign-out. */
export async function clearAutofill(): Promise<void> {
  await native()?.clearAutofill();
}
