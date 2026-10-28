import { Image } from "expo-image";
import { router } from "expo-router";
import { useState } from "react";
import { ActivityIndicator, Pressable, ScrollView, Text, View } from "react-native";

import { provision } from "../lib/api";
import { signIn } from "../lib/auth";
import { registerForPush } from "../lib/push";

export default function SignIn() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function go() {
    setBusy(true);
    setError(null);
    try {
      await signIn();
      // Provision is the signup half of login: plants the humans row the
      // broker keys on. Invited users pass; strangers 403.
      await provision();
      // A sign-in that lands after mount needs push registration re-tried —
      // the layout effect fires once, before the session exists.
      void registerForPush();
      router.replace("/");
    } catch (e) {
      setError(e instanceof Error ? e.message : "sign-in failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <ScrollView
      contentInsetAdjustmentBehavior="automatic"
      className="bg-zinc-950"
      contentContainerStyle={{ flexGrow: 1, padding: 24, gap: 28, justifyContent: "center" }}
    >
      <View style={{ alignItems: "center", gap: 18 }}>
        <Image
          source={require("../../assets/icon.png")}
          style={{ width: 88, height: 88, borderRadius: 20 }}
        />
        <Text className="text-zinc-100" style={{ fontSize: 30, fontWeight: "700" }}>
          Veil
        </Text>
        <Text className="text-zinc-400" style={{ fontSize: 15, lineHeight: 22, textAlign: "center" }}>
          Your passwords, filled. Sign in at login.veil.nyc — this app never sees your password or
          TOTP.
        </Text>
      </View>

      {error ? (
        <Text selectable className="text-red-400" style={{ textAlign: "center", fontSize: 14 }}>
          {error}
        </Text>
      ) : null}

      <Pressable
        onPress={go}
        disabled={busy}
        className="bg-blue-600"
        style={({ pressed }) => ({
          borderRadius: 12,
          paddingVertical: 15,
          alignItems: "center",
          opacity: busy ? 0.6 : pressed ? 0.85 : 1,
        })}
      >
        {busy ? (
          <ActivityIndicator color="#ffffff" />
        ) : (
          <Text className="text-white" style={{ fontSize: 16, fontWeight: "600" }}>Sign in</Text>
        )}
      </Pressable>
    </ScrollView>
  );
}
