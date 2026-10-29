import { router, useFocusEffect } from "expo-router";
import { useCallback, useMemo, useState } from "react";
import { FlatList, Pressable, ScrollView, Text, TextInput, View } from "react-native";

import { syncAutofill } from "../../modules/identity-sync";
import { listItems, type Item } from "../lib/api";
import { issuer, originAPI, signOut, storedRefresh, storedToken } from "../lib/auth";

function hostOf(uri?: string): string {
  if (!uri) return "";
  try {
    return new URL(uri).hostname;
  } catch {
    return uri;
  }
}

const KIND_LABELS: Record<string, string> = {
  api_key: "Login",
  card: "Card",
  identity: "Identity",
  ssh: "SSH Key",
  oauth: "OAuth",
  secure_note: "Secure Note",
  passkey: "Passkey",
};

const PILLS = [
  { id: "all", label: "All" },
  { id: "2fa", label: "2FA" },
  { id: "api_key", label: "Logins" },
  { id: "card", label: "Cards" },
  { id: "ssh", label: "SSH" },
  { id: "secure_note", label: "Notes" },
] as const;

export default function Items() {
  const [items, setItems] = useState<Item[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<string>("all");

  const load = useCallback(async () => {
    const tok = await storedToken();
    if (!tok) {
      router.replace("/sign-in");
      return;
    }
    try {
      const got = await listItems();
      setItems(got.filter((i) => !i.archived));
      setError(null);
      void syncAutofill(
        tok,
        await storedRefresh(),
        originAPI,
        issuer,
        got.map((i) => ({
          uuid: i.id,
          name: i.name,
          login: i.login ?? "",
          uris: i.uris ?? [],
          kind: i.kind ?? "login",
          credId: i.cred_id,
          rpId: i.rp_id,
          userHandle: i.user_handle,
        })),
      ).catch(() => {});
    } catch (e) {
      setError(e instanceof Error ? e.message : "could not load items");
    }
  }, []);

  useFocusEffect(
    useCallback(() => {
      void load();
    }, [load]),
  );

  const shown = useMemo(() => {
    if (!items) return null;
    const terms = query
      .trim()
      .toLowerCase()
      .split(/\s+/)
      .filter(Boolean);
    return items.filter((i) => {
      if (filter === "2fa" && !i.has_totp) return false;
      if (filter !== "all" && filter !== "2fa" && i.kind !== filter) return false;
      if (terms.length === 0) return true;
      const hay = [
        i.name,
        i.login ?? "",
        i.kind ?? "",
        i.has_totp ? "2fa totp" : "",
        ...(i.uris ?? []),
        ...(i.tags ?? []),
      ]
        .join(" ")
        .toLowerCase();
      return terms.every((t) => hay.includes(t));
    });
  }, [items, query, filter]);

  if (error) {
    return (
      <View style={{ flex: 1, justifyContent: "center", padding: 24, gap: 12 }}>
        <Text selectable className="text-danger" style={{ textAlign: "center" }}>
          {error}
        </Text>
        <Pressable accessibilityRole="button" onPress={() => void load()} style={{ alignItems: "center" }}>
          <Text className="text-foreground" style={{ fontWeight: "600" }}>
            Retry
          </Text>
        </Pressable>
      </View>
    );
  }

  return (
    <View style={{ flex: 1 }}>
      <FlatList
        data={shown ?? []}
        keyExtractor={(i) => i.id}
        contentInsetAdjustmentBehavior="automatic"
        keyboardDismissMode="on-drag"
        refreshing={items === null}
        onRefresh={() => void load()}
        ListHeaderComponent={
          <View style={{ paddingHorizontal: 16, paddingBottom: 10, gap: 10 }}>
            <TextInput
              value={query}
              onChangeText={setQuery}
              placeholder="Search accounts, usernames, domains, or '2fa'…"
              placeholderTextColor="#a1a1aa"
              autoCapitalize="none"
              autoCorrect={false}
              style={{
                backgroundColor: "#18181b",
                borderRadius: 12,
                borderCurve: "continuous",
                paddingHorizontal: 12,
                paddingVertical: 10,
                borderWidth: 1,
                borderColor: "#27272a",
                color: "#f4f4f5",
                fontSize: 15,
              }}
            />
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              contentContainerStyle={{ gap: 6 }}
            >
              {PILLS.map((p) => {
                const active = filter === p.id;
                return (
                  <Pressable accessibilityRole="button"
                    key={p.id}
                    onPress={() => setFilter(p.id)}
                    style={{
                      paddingHorizontal: 12,
                      paddingVertical: 6,
                      borderRadius: 999,
                      backgroundColor: active ? "#2563eb" : "#18181b",
                      borderWidth: 1,
                      borderColor: active ? "#3b82f6" : "#27272a",
                    }}
                  >
                    <Text
                      style={{
                        color: active ? "#ffffff" : "#a1a1aa",
                        fontSize: 12,
                        fontWeight: "600",
                      }}
                    >
                      {p.label}
                    </Text>
                  </Pressable>
                );
              })}
            </ScrollView>
          </View>
        }
        ListEmptyComponent={
          items === null ? null : (
            <View style={{ padding: 32, alignItems: "center", gap: 8 }}>
              <Text className="text-muted">No accounts matched.</Text>
            </View>
          )
        }
        ListFooterComponent={
          items && items.length > 0 ? (
            <Pressable accessibilityRole="button"
              onPress={() => {
                void signOut().then(() => router.replace("/sign-in"));
              }}
              style={{ padding: 24, alignItems: "center" }}
            >
              <Text className="text-muted" style={{ fontSize: 14 }}>
                Sign out
              </Text>
            </Pressable>
          ) : null
        }
        renderItem={({ item }) => (
          <Pressable accessibilityRole="button"
            onPress={() =>
              router.push({
                pathname: "/item/[id]",
                params: {
                  id: item.id,
                  name: item.name,
                  login: item.login ?? "",
                  kind: item.kind ?? "api_key",
                  uri: item.uris?.[0] ?? "",
                  totp: item.has_totp ? "1" : "",
                },
              })
            }
            style={({ pressed }) => ({
              flexDirection: "row",
              alignItems: "center",
              paddingHorizontal: 16,
              paddingVertical: 13,
              gap: 12,
              backgroundColor: pressed ? "#18181b" : "transparent",
              borderBottomWidth: 0.5,
              borderBottomColor: "#27272a",
            })}
          >
            <View
              style={{
                width: 38,
                height: 38,
                borderRadius: 10,
                borderCurve: "continuous",
                backgroundColor: "#18181b",
                borderWidth: 1,
                borderColor: "#27272a",
                alignItems: "center",
                justifyContent: "center",
              }}
            >
              <Text style={{ color: "#f4f4f5", fontWeight: "700" }}>
                {item.name.slice(0, 1).toUpperCase()}
              </Text>
            </View>
            <View style={{ flex: 1, gap: 2 }}>
              <Text className="text-foreground" style={{ fontSize: 16, fontWeight: "500" }}>
                {item.name}
              </Text>
              <Text className="text-muted" style={{ fontSize: 13 }} numberOfLines={1}>
                {item.login || hostOf(item.uris?.[0]) || KIND_LABELS[item.kind ?? ""] || ""}
              </Text>
            </View>
            {item.has_totp ? (
              <View
                style={{
                  paddingHorizontal: 8,
                  paddingVertical: 3,
                  borderRadius: 999,
                  backgroundColor: "#18181b",
                  borderWidth: 1,
                  borderColor: "#27272a",
                }}
              >
                <Text style={{ color: "#a1a1aa", fontSize: 11, fontWeight: "600" }}>
                  2FA
                </Text>
              </View>
            ) : null}
          </Pressable>
        )}
      />
    </View>
  );
}
