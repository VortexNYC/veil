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
  if (Platform.OS !== "ios") return null;
  try {
    return requireNativeModule<NativeIdentitySync>("IdentitySync");
  } catch {
    return null;
  }
}

/** Publish the credential handoff the AutoFill appex reads: the bearer
 *  token plus item metadata (never secrets) into the shared app-group
 *  container, then push QuickType identities to ASCredentialIdentityStore. */
export async function syncAutofill(token: string, origin: string, items: HandoffItem[]): Promise<void> {
  await native()?.syncAutofill(token, origin, items);
}

/** Drop the handoff + every synced identity — call on sign-out. */
export async function clearAutofill(): Promise<void> {
  await native()?.clearAutofill();
}
