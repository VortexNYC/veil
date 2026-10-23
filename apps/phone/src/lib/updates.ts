import * as Updates from "expo-updates";

// OTA update checker for iOS and Android using standard expo-updates when
// running in a standalone build. In dev/Expo Go (or when Updates.isEnabled is
// false), it is a safe no-op.
export async function checkAndApplyOTAUpdate(): Promise<void> {
  if (__DEV__ || !Updates.isEnabled) {
    return;
  }
  try {
    const update = await Updates.checkForUpdateAsync();
    if (update.isAvailable) {
      const fetched = await Updates.fetchUpdateAsync();
      if (fetched.isNew) {
        await Updates.reloadAsync();
      }
    }
  } catch {
    // Best-effort OTA check — never disrupt vault access.
  }
}
