import { Image } from "expo-image";
import { router } from "expo-router";
import { useState } from "react";
import { ActivityIndicator, Pressable, ScrollView, Text, View } from "react-native";

import { provision } from "../lib/api";
import { signIn } from "../lib/auth";

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
      contentContainerStyle={{ flexGrow: 1, padding: 24, gap: 24, justifyContent: "center" }}
    >
      <View style={{ alignItems: "center", gap: 16 }}>
        <Image
          source={require("../../assets/icon.png")}
          style={{ width: 96, height: 96, borderRadius: 22 }}
        />
        <Text className="text-foreground" style={{ fontSize: 28, fontWeight: "700" }}>
          Veil
        </Text>
        <Text className="text-muted" style={{ fontSize: 15, textAlign: "center" }}>
          Your passwords, filled. Sign in at login.veil.nyc — this app never sees your password or
          TOTP.
        </Text>
      </View>

      {error ? (
        <Text selectable className="text-danger" style={{ textAlign: "center" }}>
          {error}
        </Text>
      ) : null}

      <Pressable
        onPress={go}
        disabled={busy}
        style={({ pressed }) => ({
          backgroundColor: pressed ? "#d8d4cc" : "#f0ede8",
          borderRadius: 14,
          borderCurve: "continuous",
          paddingVertical: 14,
          alignItems: "center",
          opacity: busy ? 0.6 : 1,
        })}
      >
        {busy ? (
          <ActivityIndicator color="#141210" />
        ) : (
          <Text style={{ color: "#141210", fontSize: 16, fontWeight: "600" }}>Sign in</Text>
        )}
      </Pressable>
    </ScrollView>
  );
}
