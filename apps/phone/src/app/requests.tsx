import { router, useFocusEffect } from "expo-router";
import { useCallback, useMemo, useRef, useState } from "react";
import { FlatList, Pressable, Text, View } from "react-native";
import * as LocalAuthentication from "expo-local-authentication";

import {
  listItems,
  listOpenRequests,
  resolveRequest,
  type ApprovalRequest,
} from "../lib/api";
import { storedToken } from "../lib/auth";

function fmtAgo(ts: string): string {
  const s = Math.max(0, (Date.now() - Date.parse(ts)) / 1000) | 0;
  if (s < 60) return `${s}s ago`;
  return `${(s / 60) | 0}m ago`;
}

function fmtLeft(ts: string): string {
  const s = Math.max(0, (Date.parse(ts) - Date.now()) / 1000) | 0;
  if (s < 60) return `${s}s`;
  return `${(s / 60) | 0}m`;
}

export default function Requests() {
  const [requests, setRequests] = useState<ApprovalRequest[] | null>(null);
  const [names, setNames] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const open = useRef(true);

  const load = useCallback(async () => {
    if (!(await storedToken())) {
      router.replace("/sign-in");
      return;
    }
    try {
      const [reqs, items] = await Promise.all([listOpenRequests(), listItems()]);
      setRequests(reqs);
      setNames(Object.fromEntries(items.map((i) => [i.id, i.name])));
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "could not load requests");
    }
  }, []);

  // Poll while the screen is open — the fill host's own poll is 2s, so a
  // resolved ask drops off this list in step with the requester's loop.
  useFocusEffect(
    useCallback(() => {
      open.current = true;
      void load();
      const tick = setInterval(() => {
        if (open.current) void load();
      }, 2000);
      return () => {
        open.current = false;
        clearInterval(tick);
      };
    }, [load]),
  );

  const resolve = useCallback(
    async (id: string, verb: "approve" | "deny") => {
      if (busy) return;
      // Approve is the grant — gate it on biometrics like the Mac's Touch
      // ID sheet. Deny isn't a privilege; a stolen-but-locked phone that
      // can deny is still fail-closed.
      if (verb === "approve") {
        const auth = await LocalAuthentication.authenticateAsync({
          promptMessage: "Approve Veil request",
          cancelLabel: "Cancel",
          fallbackLabel: "Use Passcode",
        });
        if (!auth.success) return;
      }
      setBusy(id + verb);
      try {
        await resolveRequest(id, verb);
        setRequests((rs) => rs?.filter((r) => r.id !== id) ?? rs);
      } catch (e) {
        setError(e instanceof Error ? e.message : "resolve failed");
      } finally {
        setBusy(null);
      }
    },
    [busy],
  );

  const shown = useMemo(() => requests ?? [], [requests]);

  return (
    <View style={{ flex: 1 }}>
      <FlatList
        data={shown}
        keyExtractor={(r) => r.id}
        contentInsetAdjustmentBehavior="automatic"
        refreshing={requests === null}
        onRefresh={() => void load()}
        ListEmptyComponent={
          requests === null ? null : (
            <View style={{ padding: 40, alignItems: "center", gap: 8 }}>
              <Text className="text-muted">
                {error ?? "No open requests."}
              </Text>
              {!error ? (
                <Text className="text-muted" style={{ fontSize: 13 }}>
                  When an agent asks to use a credential it lands here.
                </Text>
              ) : null}
            </View>
          )
        }
        renderItem={({ item: r }) => (
          <View
            style={{
              paddingHorizontal: 16,
              paddingVertical: 14,
              gap: 10,
              borderBottomWidth: 1,
              borderBottomColor: "#27272a",
            }}
          >
            <View style={{ flexDirection: "row", alignItems: "center", gap: 12 }}>
              <View
                style={{
                  width: 36,
                  height: 36,
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
                  {r.agent_id.slice(0, 1).toUpperCase()}
                </Text>
              </View>
              <View style={{ flex: 1, gap: 1 }}>
                <Text
                  className="text-foreground"
                  style={{ fontSize: 15, fontWeight: "600" }}
                  numberOfLines={2}
                >
                  {r.agent_id} wants to use {names[r.item_id] ?? r.item_id}
                </Text>
                <Text className="text-muted" style={{ fontSize: 12 }}>
                  {r.action} · {fmtAgo(r.created_at)} · {fmtLeft(r.expires_at)} left
                </Text>
              </View>
            </View>
            <View style={{ flexDirection: "row", gap: 10 }}>
              <Pressable accessibilityRole="button"
                onPress={() => void resolve(r.id, "deny")}
                disabled={busy !== null}
                style={({ pressed }) => ({
                  flex: 1,
                  paddingVertical: 10,
                  borderRadius: 10,
                  borderCurve: "continuous",
                  borderWidth: 1,
                  borderColor: "#27272a",
                  alignItems: "center",
                  backgroundColor: pressed ? "#18181b" : "transparent",
                  opacity: busy === null || busy === r.id + "deny" ? 1 : 0.4,
                })}
              >
                <Text className="text-foreground" style={{ fontWeight: "600" }}>
                  Deny
                </Text>
              </Pressable>
              <Pressable accessibilityRole="button"
                onPress={() => void resolve(r.id, "approve")}
                disabled={busy !== null}
                style={({ pressed }) => ({
                  flex: 1,
                  paddingVertical: 10,
                  borderRadius: 10,
                  borderCurve: "continuous",
                  alignItems: "center",
                  backgroundColor: pressed ? "#1d4ed8" : "#2563eb",
                  opacity: busy === null || busy === r.id + "approve" ? 1 : 0.4,
                })}
              >
                <Text style={{ color: "#ffffff", fontWeight: "700" }}>
                  Approve
                </Text>
              </Pressable>
            </View>
          </View>
        )}
      />
    </View>
  );
}
