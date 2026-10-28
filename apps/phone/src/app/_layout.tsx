import "../../global.css";

import { router } from "expo-router";
import { Stack } from "expo-router/stack";
import { StatusBar } from "expo-status-bar";
import { AppState, Pressable, Text, View } from "react-native";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { useEffect } from "react";
import { armErrorReporting } from "../lib/report";
import { registerForPush, wireNotificationTap } from "../lib/push";
import { checkAndApplyOTAUpdate } from "../lib/updates";

export { ErrorBoundary } from "expo-router";

armErrorReporting();

export default function Layout() {
  // Push registration is best-effort and checks the session itself —
  // a signed-out app asks nothing.
  useEffect(() => {
    wireNotificationTap();
    void registerForPush();
    void checkAndApplyOTAUpdate();
    const sub = AppState.addEventListener("change", (state) => {
      if (state === "active") {
        void checkAndApplyOTAUpdate();
      }
    });
    return () => sub.remove();
  }, []);
  return (
    <SafeAreaProvider>
      <StatusBar style="light" />
      <Stack
        screenOptions={{
          headerStyle: { backgroundColor: "#0e0e10" },
          headerTintColor: "#f4f4f5",
          headerTitleStyle: { fontWeight: "600" },
          contentStyle: { backgroundColor: "#0e0e10" },
        }}
      >
        <Stack.Screen
          name="index"
          options={{
            title: "Veil",
            headerLargeTitle: true,
            headerRight: () => (
              <View style={{ flexDirection: "row", alignItems: "center", gap: 18 }}>
                <Pressable onPress={() => router.push("/requests")} hitSlop={12}>
                  <Text style={{ color: "#f4f4f5", fontSize: 16, fontWeight: "600" }}>
                    Requests
                  </Text>
                </Pressable>
                <Pressable onPress={() => router.push("/add")} hitSlop={12}>
                  <Text style={{ color: "#3b82f6", fontSize: 28, fontWeight: "400" }}>+</Text>
                </Pressable>
              </View>
            ),
          }}
        />
        <Stack.Screen name="sign-in" options={{ title: "Sign in", headerShown: false }} />
        <Stack.Screen name="item/[id]" options={{ title: "" }} />
        <Stack.Screen
          name="requests"
          options={{
            title: "Requests",
            headerRight: () => (
              <Pressable onPress={() => router.push("/escrow")} hitSlop={12}>
                <Text style={{ color: "#f4f4f5", fontSize: 16, fontWeight: "600" }}>Escrow</Text>
              </Pressable>
            ),
          }}
        />
        <Stack.Screen name="escrow" options={{ title: "KEK Escrow" }} />
        <Stack.Screen name="add" options={{ title: "Add password", presentation: "modal" }} />
      </Stack>
    </SafeAreaProvider>
  );
}
