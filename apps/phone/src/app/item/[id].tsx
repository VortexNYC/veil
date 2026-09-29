import * as Clipboard from "expo-clipboard";
import * as LocalAuthentication from "expo-local-authentication";
import { Stack, useLocalSearchParams } from "expo-router";
import { useState } from "react";
import { Pressable, ScrollView, Text, View } from "react-native";

import { fillLogin, type FillEntry } from "../../lib/api";

function Row({ label, value, onCopy }: { label: string; value: string; onCopy?: () => void }) {
  return (
    <View style={{ gap: 4 }}>
      <Text className="text-muted" style={{ fontSize: 12, fontWeight: "600", letterSpacing: 0.4 }}>
        {label.toUpperCase()}
      </Text>
      <Pressable onPress={onCopy} disabled={!onCopy}>
        <Text selectable className="text-foreground" style={{ fontSize: 16, fontVariant: ["tabular-nums"] }}>
          {value}
        </Text>
      </Pressable>
    </View>
  );
}

export default function ItemDetail() {
  const { id, name, totp } = useLocalSearchParams<{ id: string; name: string; totp?: string }>();
  const [entry, setEntry] = useState<FillEntry | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState<string | null>(null);

  async function reveal() {
    setBusy(true);
    setError(null);
    try {
      const ok = await LocalAuthentication.authenticateAsync({
        promptMessage: "Veil wants to reveal this password",
        cancelLabel: "Cancel",
      });
      if (!ok.success) return;
      const got = await fillLogin(String(id), totp === "1");
      if (!got) throw new Error("no entry");
      setEntry(got);
    } catch (e) {
      setError(e instanceof Error ? e.message : "could not reveal");
    } finally {
      setBusy(false);
    }
  }

  async function copy(label: string, value: string) {
    await Clipboard.setStringAsync(value);
    setCopied(label);
    setTimeout(() => setCopied((c) => (c === label ? null : c)), 1500);
  }

  return (
    <ScrollView
      contentInsetAdjustmentBehavior="automatic"
      contentContainerStyle={{ padding: 20, gap: 20 }}
    >
      <Stack.Screen options={{ title: name ?? "" }} />
      {entry ? (
        <>
          <Row label="Name" value={entry.name} onCopy={() => void copy("name", entry.name)} />
          <Row label="Username" value={entry.login} onCopy={() => void copy("login", entry.login)} />
          <Row label="Password" value={entry.password} onCopy={() => void copy("password", entry.password)} />
          {entry.totp ? (
            <Row label="One-time code" value={entry.totp} onCopy={() => void copy("totp", entry.totp!)} />
          ) : null}
          <Text className="text-muted" style={{ fontSize: 12 }}>
            {copied ? `${copied} copied` : "Tap a field to copy."}
          </Text>
        </>
      ) : (
        <View style={{ gap: 16 }}>
          <Text className="text-muted" style={{ fontSize: 15 }}>
            Revealing needs Face ID — same rule as the desktop host.
          </Text>
          {error ? (
            <Text selectable className="text-danger">
              {error}
            </Text>
          ) : null}
          <Pressable
            onPress={() => void reveal()}
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
            <Text style={{ color: "#141210", fontSize: 16, fontWeight: "600" }}>
              {busy ? "Checking…" : "Reveal password"}
            </Text>
          </Pressable>
        </View>
      )}
    </ScrollView>
  );
}
