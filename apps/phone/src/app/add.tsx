import { router } from "expo-router";
import { useState } from "react";
import { ActivityIndicator, Pressable, ScrollView, Text, TextInput } from "react-native";

import { addLogin } from "../lib/api";

const field = {
  backgroundColor: "#1e1b18",
  borderRadius: 12,
  borderCurve: "continuous" as const,
  paddingHorizontal: 12,
  paddingVertical: 12,
  borderWidth: 1,
  borderColor: "#2e2924",
  color: "#f0ede8",
  fontSize: 16,
};

export default function Add() {
  const [name, setName] = useState("");
  const [uri, setUri] = useState("");
  const [login, setLogin] = useState("");
  const [secret, setSecret] = useState("");
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
        placeholder="Name — github, stripe…"
        placeholderTextColor="#8a8378"
        autoCapitalize="none"
        autoCorrect={false}
        style={field}
      />
      <TextInput
        value={uri}
        onChangeText={setUri}
        placeholder="Website — https://github.com"
        placeholderTextColor="#8a8378"
        autoCapitalize="none"
        autoCorrect={false}
        keyboardType="url"
        style={field}
      />
      <TextInput
        value={login}
        onChangeText={setLogin}
        placeholder="Username or email"
        placeholderTextColor="#8a8378"
        autoCapitalize="none"
        autoCorrect={false}
        style={field}
      />
      <TextInput
        value={secret}
        onChangeText={setSecret}
        placeholder="Password"
        placeholderTextColor="#8a8378"
        autoCapitalize="none"
        autoCorrect={false}
        secureTextEntry
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
          backgroundColor: pressed ? "#d8d4cc" : "#f0ede8",
          borderRadius: 14,
          borderCurve: "continuous",
          paddingVertical: 14,
          alignItems: "center",
          opacity: busy || !name.trim() || !secret ? 0.5 : 1,
        })}
      >
        {busy ? (
          <ActivityIndicator color="#141210" />
        ) : (
          <Text style={{ color: "#141210", fontSize: 16, fontWeight: "600" }}>Save</Text>
        )}
      </Pressable>
    </ScrollView>
  );
}
