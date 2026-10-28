import * as Clipboard from "expo-clipboard";
import * as LocalAuthentication from "expo-local-authentication";
import { Stack, useLocalSearchParams } from "expo-router";
import { useEffect, useState } from "react";
import { Linking, Pressable, ScrollView, Text, View } from "react-native";

import { fillLogin, mintTotp, type FillEntry } from "../../lib/api";

function FieldRow({
  label,
  value,
  mono,
  badge,
  actionLabel,
  onAction,
}: {
  label: string;
  value: string;
  mono?: boolean;
  badge?: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <View
      style={{
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 12,
        paddingHorizontal: 16,
        paddingVertical: 14,
        borderBottomWidth: 0.5,
        borderBottomColor: "#27272a",
      }}
    >
      <View style={{ flex: 1, gap: 4 }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
          <Text
            style={{
              color: "#a1a1aa",
              fontSize: 11,
              fontWeight: "600",
              letterSpacing: 0.5,
            }}
          >
            {label.toUpperCase()}
          </Text>
          {badge ? (
            <View
              style={{
                paddingHorizontal: 6,
                paddingVertical: 1,
                borderRadius: 999,
                backgroundColor: "#27272a",
              }}
            >
              <Text style={{ color: "#f4f4f5", fontSize: 10, fontWeight: "600" }}>
                {badge}
              </Text>
            </View>
          ) : null}
        </View>
        <Text
          selectable
          style={{
            color: "#f4f4f5",
            fontSize: 16,
            fontFamily: mono ? "Menlo" : undefined,
            fontVariant: ["tabular-nums"],
          }}
        >
          {value}
        </Text>
      </View>
      {onAction && actionLabel ? (
        <Pressable accessibilityRole="button"
          onPress={onAction}
          style={({ pressed }) => ({
            paddingHorizontal: 12,
            paddingVertical: 7,
            borderRadius: 8,
            backgroundColor: pressed ? "#27272a" : "#1f1f23",
            borderWidth: 1,
            borderColor: "#27272a",
          })}
        >
          <Text style={{ color: "#f4f4f5", fontSize: 13, fontWeight: "600" }}>
            {actionLabel}
          </Text>
        </Pressable>
      ) : null}
    </View>
  );
}

export default function ItemDetail() {
  const { id, name, login, uri, totp } = useLocalSearchParams<{
    id: string;
    name: string;
    login?: string;
    kind?: string;
    uri?: string;
    totp?: string;
  }>();
  const [entry, setEntry] = useState<FillEntry | null>(null);
  const [totpCode, setTotpCode] = useState<string | null>(null);
  const [totpLeft, setTotpLeft] = useState<number>(
    30 - (Math.floor(Date.now() / 1000) % 30),
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState<string | null>(null);

  useEffect(() => {
    if (!totpCode || !id) return;
    const t = setInterval(() => {
      const rem = 30 - (Math.floor(Date.now() / 1000) % 30);
      setTotpLeft(rem);
      if (rem === 30) {
        void mintTotp(String(id))
          .then((c) => setTotpCode(c))
          .catch(() => {});
      }
    }, 1000);
    return () => clearInterval(t);
  }, [totpCode, id]);

  async function reveal() {
    setBusy(true);
    setError(null);
    try {
      const ok = await LocalAuthentication.authenticateAsync({
        promptMessage: "Veil wants to reveal this account",
        cancelLabel: "Cancel",
      });
      if (!ok.success) return;
      const got = await fillLogin(String(id), totp === "1");
      if (!got) throw new Error("no entry");
      setEntry(got);
      if (got.totp && got.totp !== "*") {
        setTotpCode(got.totp);
      } else if (totp === "1" || got.totp === "*") {
        const minted = await mintTotp(String(id)).catch(() => null);
        if (minted) setTotpCode(minted);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "could not reveal");
    } finally {
      setBusy(false);
    }
  }

  async function copy(key: string, value: string) {
    await Clipboard.setStringAsync(value);
    setCopied(key);
    setTimeout(() => setCopied((c) => (c === key ? null : c)), 1500);
  }

  const displayLogin = entry?.login || login || "";
  const formattedTotp =
    totpCode && totpCode.length === 6
      ? `${totpCode.slice(0, 3)} ${totpCode.slice(3)}`
      : totpCode;

  return (
    <ScrollView
      contentInsetAdjustmentBehavior="automatic"
      contentContainerStyle={{ padding: 16, gap: 16 }}
    >
      <Stack.Screen options={{ title: name ?? "" }} />

      <View
        style={{
          backgroundColor: "#18181b",
          borderRadius: 14,
          borderCurve: "continuous",
          borderWidth: 1,
          borderColor: "#27272a",
          overflow: "hidden",
        }}
      >
        {displayLogin ? (
          <FieldRow
            label="Username / Email"
            value={displayLogin}
            mono
            actionLabel={copied === "login" ? "Copied" : "Copy"}
            onAction={() => void copy("login", displayLogin)}
          />
        ) : null}

        {entry ? (
          <FieldRow
            label="Password / Secret"
            value={entry.password || "—"}
            mono
            actionLabel={copied === "password" ? "Copied" : "Copy"}
            onAction={
              entry.password
                ? () => void copy("password", entry.password)
                : undefined
            }
          />
        ) : (
          <FieldRow
            label="Password / Secret"
            value="••••••••••••••"
            mono
            actionLabel={busy ? "…" : "Reveal"}
            onAction={() => void reveal()}
          />
        )}

        {formattedTotp ? (
          <FieldRow
            label="One-Time Password (2FA)"
            value={formattedTotp}
            mono
            badge={`${totpLeft}s`}
            actionLabel={copied === "totp" ? "Copied" : "Copy 2FA"}
            onAction={() => void copy("totp", totpCode!)}
          />
        ) : totp === "1" ? (
          <FieldRow
            label="One-Time Password (2FA)"
            value="••• •••"
            mono
            actionLabel={busy ? "…" : "Show 2FA"}
            onAction={() => void reveal()}
          />
        ) : null}
      </View>

      {entry?.card ? (
        <View
          style={{
            backgroundColor: "#18181b",
            borderRadius: 14,
            borderCurve: "continuous",
            borderWidth: 1,
            borderColor: "#27272a",
            overflow: "hidden",
          }}
        >
          {entry.card.holder ? (
            <FieldRow
              label="Cardholder"
              value={entry.card.holder}
              actionLabel={copied === "holder" ? "Copied" : "Copy"}
              onAction={() => void copy("holder", entry.card!.holder!)}
            />
          ) : null}
          {entry.card.number ? (
            <FieldRow
              label="Card Number"
              value={entry.card.number}
              mono
              actionLabel={copied === "cardnum" ? "Copied" : "Copy"}
              onAction={() => void copy("cardnum", entry.card!.number!)}
            />
          ) : null}
          {entry.card.exp_month || entry.card.exp_year ? (
            <FieldRow
              label="Expires"
              value={`${entry.card.exp_month ?? ""}/${entry.card.exp_year ?? ""}`}
              mono
            />
          ) : null}
          {entry.card.cvv ? (
            <FieldRow
              label="CVV"
              value={entry.card.cvv}
              mono
              actionLabel={copied === "cvv" ? "Copied" : "Copy"}
              onAction={() => void copy("cvv", entry.card!.cvv!)}
            />
          ) : null}
        </View>
      ) : null}

      {entry?.notes ? (
        <View
          style={{
            backgroundColor: "#18181b",
            borderRadius: 14,
            borderCurve: "continuous",
            borderWidth: 1,
            borderColor: "#27272a",
            overflow: "hidden",
          }}
        >
          <FieldRow
            label="Notes"
            value={entry.notes}
            mono
            actionLabel={copied === "notes" ? "Copied" : "Copy"}
            onAction={() => void copy("notes", entry.notes!)}
          />
        </View>
      ) : null}

      {uri ? (
        <View
          style={{
            backgroundColor: "#18181b",
            borderRadius: 14,
            borderCurve: "continuous",
            borderWidth: 1,
            borderColor: "#27272a",
            overflow: "hidden",
          }}
        >
          <FieldRow
            label="Website"
            value={uri}
            mono
            actionLabel="Open"
            onAction={() => void Linking.openURL(uri)}
          />
        </View>
      ) : null}

      {error ? (
        <Text selectable className="text-danger">
          {error}
        </Text>
      ) : null}

      {!entry ? (
        <Pressable accessibilityRole="button"
          onPress={() => void reveal()}
          disabled={busy}
          style={({ pressed }) => ({
            backgroundColor: pressed ? "#1d4ed8" : "#2563eb",
            borderRadius: 14,
            borderCurve: "continuous",
            paddingVertical: 14,
            alignItems: "center",
            opacity: busy ? 0.6 : 1,
          })}
        >
          <Text style={{ color: "#ffffff", fontSize: 16, fontWeight: "600" }}>
            {busy ? "Checking Face ID…" : "Reveal with Face ID"}
          </Text>
        </Pressable>
      ) : null}
    </ScrollView>
  );
}
