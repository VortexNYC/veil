// push.ts — register the OS push token so approval asks and ops pages
// reach the lock screen. Raw device token (not Expo's relay) — the
// origin holds the APNs/FCM send path itself. Registration is
// best-effort: the vault works without it, pushes are the wake channel.
import * as Notifications from "expo-notifications";
import * as Device from "expo-device";
import { Platform } from "react-native";
import { router } from "expo-router";
import { registerDevice } from "./api";
import { storedToken } from "./auth";
import { report } from "./report";

let registered = false;

// Foreground presentation: an approval ask landing while the app is open
// still deserves a banner — the agent is waiting.
Notifications.setNotificationHandler({
  handleNotification: async () =>
    ({
      shouldPlaySound: true,
      shouldSetBadge: false,
      shouldShowAlert: true,
      shouldShowBanner: true,
      shouldShowList: true,
    }) as Notifications.NotificationBehavior,
});

// Tapping an approval banner (or its ops sibling) opens the requests
// screen — the ask's own surface is the only place a decision belongs.
let tapWired = false;
export function wireNotificationTap(): void {
  if (tapWired) return;
  tapWired = true;
  Notifications.addNotificationResponseReceivedListener((res) => {
    const data = res.notification.request.content.data as
      | { type?: string }
      | undefined;
    if (data?.type === "approve_request" || data?.type === "ops_finding") {
      router.push("/requests");
    }
  });
}

export async function registerForPush(): Promise<void> {
  if (registered) return;
  if (!Device.isDevice) return; // simulators can't take push tokens
  try {
    const human = await Promise.race([
      storedToken(),
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 8000)),
    ]);
    if (!human) return; // not signed in — nothing to bind
    const res = await Notifications.requestPermissionsAsync();
    // The base PermissionResponse isn't resolving in this workspace's
    // expo types — the docs' granted + ios.status both exist at runtime.
    const granted =
      (res as { granted?: boolean }).granted === true ||
      res.ios?.status === Notifications.IosAuthorizationStatus.AUTHORIZED ||
      res.ios?.status === Notifications.IosAuthorizationStatus.PROVISIONAL;
    if (!granted) return;
    const raw = await Notifications.getDevicePushTokenAsync();
    const token =
      typeof raw.data === "string" ? raw.data : (raw.data as { token?: string }).token ?? "";
    if (!token) return;
    await registerDevice(
      Platform.OS === "ios" ? "apns" : "fcm",
      token,
      `${Platform.OS} ${Device.modelName ?? "device"}`,
    );
    registered = true;
  } catch (err) {
    // A registration failure must never break the app — it degrades to
    // email/ntfy paging silently.
    report.error(err, { op: "push_register" });
  }
}
