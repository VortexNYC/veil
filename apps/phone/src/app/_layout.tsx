import "../../global.css";

import { router } from "expo-router";
import { Stack } from "expo-router/stack";
import { StatusBar } from "expo-status-bar";
import { Pressable, Text, View } from "react-native";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { useEffect } from "react";
import { armErrorReporting } from "../lib/report";
import { registerForPush, wireNotificationTap } from "../lib/push";

export { ErrorBoundary } from "expo-router";

armErrorReporting();

export default function Layout() {
  // Push registration is best-effort and checks the session itself —
  // a signed-out app asks nothing.
  useEffect(() => {
    wireNotificationTap();
    void registerForPush();
  }, []);
  return (
    <SafeAreaProvider>
      <StatusBar style="light" />
      <Stack
        screenOptions={{
          headerStyle: { backgroundColor: "#141210" },
          headerTintColor: "#f0ede8",
          headerTitleStyle: { fontWeight: "600" },
          contentStyle: { backgroundColor: "#141210" },
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
                  <Text style={{ color: "#f0ede8", fontSize: 16, fontWeight: "600" }}>
                    Requests
                  </Text>
                </Pressable>
                <Pressable onPress={() => router.push("/add")} hitSlop={12}>
                  <Text style={{ color: "#f0ede8", fontSize: 28, fontWeight: "300" }}>+</Text>
                </Pressable>
              </View>
            ),
          }}
        />
        <Stack.Screen name="sign-in" options={{ title: "Sign in", headerShown: false }} />
        <Stack.Screen name="item/[id]" options={{ title: "" }} />
        <Stack.Screen name="requests" options={{ title: "Requests" }} />
        <Stack.Screen name="add" options={{ title: "Add password", presentation: "modal" }} />
      </Stack>
    </SafeAreaProvider>
  );
}
