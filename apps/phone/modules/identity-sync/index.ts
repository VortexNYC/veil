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

type NativeIdentitySync = {
  syncAutofill(token: string, origin: string, items: HandoffItem[]): Promise<void>;
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

/** Publish the credential handoff the AutoFill provider reads: the bearer
 *  token plus item metadata (never secrets). iOS writes the shared app-group
 *  container + pushes QuickType identities; Android drops the file in
 *  filesDir where VeilAutofillService reads it in-process. */
export async function syncAutofill(token: string, origin: string, items: HandoffItem[]): Promise<void> {
  await native()?.syncAutofill(token, origin, items);
}

/** Drop the handoff + every synced identity — call on sign-out. */
export async function clearAutofill(): Promise<void> {
  await native()?.clearAutofill();
}
