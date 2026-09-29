import { router, useFocusEffect } from "expo-router";
import { useCallback, useMemo, useState } from "react";
import { FlatList, Pressable, Text, TextInput, View } from "react-native";

import { listItems, type Item } from "../lib/api";
import { signOut, storedToken } from "../lib/auth";

function hostOf(uri?: string): string {
  if (!uri) return "";
  try {
    return new URL(uri).hostname;
  } catch {
    return uri;
  }
}

export default function Items() {
  const [items, setItems] = useState<Item[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");

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
    const q = query.trim().toLowerCase();
    if (!q || !items) return items;
    return items.filter(
      (i) =>
        i.name.toLowerCase().includes(q) ||
        (i.login ?? "").toLowerCase().includes(q) ||
        (i.uris ?? []).some((u) => u.toLowerCase().includes(q)),
    );
  }, [items, query]);

  if (error) {
    return (
      <View style={{ flex: 1, justifyContent: "center", padding: 24, gap: 12 }}>
        <Text selectable className="text-danger" style={{ textAlign: "center" }}>
          {error}
        </Text>
        <Pressable onPress={() => void load()} style={{ alignItems: "center" }}>
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
          <View style={{ paddingHorizontal: 16, paddingBottom: 8 }}>
            <TextInput
              value={query}
              onChangeText={setQuery}
              placeholder="Search items"
              placeholderTextColor="#8a8378"
              autoCapitalize="none"
              autoCorrect={false}
              style={{
                backgroundColor: "#1e1b18",
                borderRadius: 12,
                borderCurve: "continuous",
                paddingHorizontal: 12,
                paddingVertical: 10,
                borderWidth: 1,
                borderColor: "#2e2924",
                color: "#f0ede8",
                fontSize: 15,
              }}
            />
          </View>
        }
        ListEmptyComponent={
          items === null ? null : (
            <View style={{ padding: 32, alignItems: "center", gap: 8 }}>
              <Text className="text-muted">No passwords yet.</Text>
              <Text className="text-muted" style={{ fontSize: 13 }}>
                Add one, or create items from the CLI or vault.
              </Text>
            </View>
          )
        }
        ListFooterComponent={
          items && items.length > 0 ? (
            <Pressable
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
          <Pressable
            onPress={() =>
              router.push({
                pathname: "/item/[id]",
                params: { id: item.id, name: item.name, totp: item.has_totp ? "1" : "" },
              })
            }
            style={({ pressed }) => ({
              flexDirection: "row",
              alignItems: "center",
              paddingHorizontal: 16,
              paddingVertical: 14,
              gap: 12,
              backgroundColor: pressed ? "#1e1b18" : "transparent",
            })}
          >
            <View
              style={{
                width: 36,
                height: 36,
                borderRadius: 10,
                borderCurve: "continuous",
                backgroundColor: "#1e1b18",
                borderWidth: 1,
                borderColor: "#2e2924",
                alignItems: "center",
                justifyContent: "center",
              }}
            >
              <Text style={{ color: "#f0ede8", fontWeight: "700" }}>
                {item.name.slice(0, 1).toUpperCase()}
              </Text>
            </View>
            <View style={{ flex: 1, gap: 1 }}>
              <Text className="text-foreground" style={{ fontSize: 16, fontWeight: "500" }}>
                {item.name}
              </Text>
              <Text className="text-muted" style={{ fontSize: 13 }} numberOfLines={1}>
                {item.login || hostOf(item.uris?.[0]) || item.kind || ""}
              </Text>
            </View>
            {item.has_totp ? (
              <Text className="text-muted" style={{ fontSize: 11, fontWeight: "600" }}>
                TOTP
              </Text>
            ) : null}
          </Pressable>
        )}
      />
    </View>
  );
}
