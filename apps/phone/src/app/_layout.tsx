import "../../global.css";

import { router } from "expo-router";
import { Stack } from "expo-router/stack";
import { StatusBar } from "expo-status-bar";
import { Pressable, Text } from "react-native";
import { SafeAreaProvider } from "react-native-safe-area-context";

export { ErrorBoundary } from "expo-router";

export default function Layout() {
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
              <Pressable onPress={() => router.push("/add")} hitSlop={12}>
                <Text style={{ color: "#f0ede8", fontSize: 28, fontWeight: "300" }}>+</Text>
              </Pressable>
            ),
          }}
        />
        <Stack.Screen name="sign-in" options={{ title: "Sign in", headerShown: false }} />
        <Stack.Screen name="item/[id]" options={{ title: "" }} />
        <Stack.Screen name="add" options={{ title: "Add password", presentation: "modal" }} />
      </Stack>
    </SafeAreaProvider>
  );
}
