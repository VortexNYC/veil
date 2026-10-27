import { router } from "expo-router";
import { useState } from "react";
import { ActivityIndicator, Pressable, ScrollView, Text, TextInput, View } from "react-native";

import { addLogin } from "../lib/api";

const field = {
  backgroundColor: "#18181b",
  borderRadius: 12,
  borderCurve: "continuous" as const,
  paddingHorizontal: 12,
  paddingVertical: 12,
  borderWidth: 1,
  borderColor: "#27272a",
  color: "#f4f4f5",
  fontSize: 16,
};

function generateMobilePassword(): string {
  const chars =
    "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*-_=+";
  let out = "";
  for (let i = 0; i < 24; i++) {
    out += chars[Math.floor(Math.random() * chars.length)];
  }
  return out;
}

export default function Add() {
  const [name, setName] = useState("");
  const [uri, setUri] = useState("");
  const [login, setLogin] = useState("");
  const [secret, setSecret] = useState("");
  const [totpSeed, setTotpSeed] = useState("");
  const [showSecret, setShowSecret] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save() {
    if (!name.trim() || !secret) return;
    setBusy(true);
    setError(null);
    try {
      await addLogin({
        name: name.trim(),
        uri: uri.trim() || undefined,
        login: login.trim() || undefined,
        secret,
        totp_seed: totpSeed.trim() || undefined,
      });
      router.back();
    } catch (e) {
      setError(e instanceof Error ? e.message : "could not save");
    } finally {
      setBusy(false);
    }
  }

  return (
    <ScrollView
      contentInsetAdjustmentBehavior="automatic"
      keyboardDismissMode="on-drag"
      contentContainerStyle={{ padding: 20, gap: 16 }}
    >
      <TextInput
        value={name}
        onChangeText={setName}
        placeholder="Account name — GitHub, Stripe…"
        placeholderTextColor="#a1a1aa"
        autoCapitalize="none"
        autoCorrect={false}
        style={field}
      />
      <TextInput
        value={uri}
        onChangeText={setUri}
        placeholder="Website — https://github.com"
        placeholderTextColor="#a1a1aa"
        autoCapitalize="none"
        autoCorrect={false}
        keyboardType="url"
        style={field}
      />
      <TextInput
        value={login}
        onChangeText={setLogin}
        placeholder="Username or email"
        placeholderTextColor="#a1a1aa"
        autoCapitalize="none"
        autoCorrect={false}
        style={field}
      />
      <View style={{ flexDirection: "row", gap: 8, alignItems: "center" }}>
        <TextInput
          value={secret}
          onChangeText={setSecret}
          placeholder="Password"
          placeholderTextColor="#a1a1aa"
          autoCapitalize="none"
          autoCorrect={false}
          secureTextEntry={!showSecret}
          style={[field, { flex: 1 }]}
        />
        <Pressable
          onPress={() => {
            setSecret(generateMobilePassword());
            setShowSecret(true);
          }}
          style={({ pressed }) => ({
            paddingHorizontal: 14,
            paddingVertical: 13,
            borderRadius: 12,
            backgroundColor: pressed ? "#27272a" : "#18181b",
            borderWidth: 1,
            borderColor: "#27272a",
          })}
        >
          <Text style={{ color: "#f4f4f5", fontSize: 14, fontWeight: "600" }}>
            Generate
          </Text>
        </Pressable>
      </View>
      <TextInput
        value={totpSeed}
        onChangeText={setTotpSeed}
        placeholder="2FA seed or otpauth:// URI (optional)"
        placeholderTextColor="#a1a1aa"
        autoCapitalize="none"
        autoCorrect={false}
        style={field}
      />
      {error ? (
        <Text selectable className="text-danger">
          {error}
        </Text>
      ) : null}
      <Pressable
        onPress={() => void save()}
        disabled={busy || !name.trim() || !secret}
        style={({ pressed }) => ({
          backgroundColor: pressed ? "#1d4ed8" : "#2563eb",
          borderRadius: 14,
          borderCurve: "continuous",
          paddingVertical: 14,
          alignItems: "center",
          opacity: busy || !name.trim() || !secret ? 0.5 : 1,
        })}
      >
        {busy ? (
          <ActivityIndicator color="#ffffff" />
        ) : (
          <Text style={{ color: "#ffffff", fontSize: 16, fontWeight: "600" }}>Save</Text>
        )}
      </Pressable>
    </ScrollView>
  );
}
