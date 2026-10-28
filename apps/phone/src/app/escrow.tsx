import * as Clipboard from "expo-clipboard";
import * as LocalAuthentication from "expo-local-authentication";
import { router } from "expo-router";
import { useState } from "react";
import { Pressable, ScrollView, Text, TextInput, View } from "react-native";

import { custodianPubHex, openParcel, type OpenedShare } from "../lib/escrow";

export default function Escrow() {
  const [pub, setPub] = useState("");
  const [parcel, setParcel] = useState("");
  const [opened, setOpened] = useState<OpenedShare | null>(null);
  const [msg, setMsg] = useState("");

  const auth = async (): Promise<boolean> => {
    const r = await LocalAuthentication.authenticateAsync({ promptMessage: "Veil Escrow" });
    return r.success;
  };

  const genKey = async () => {
    if (!(await auth())) return setMsg("Face ID required");
    setPub(await custodianPubHex());
    setMsg("Custodian key ready — share this pubkey with the operator");
  };

  const copy = async (v: string) => {
    await Clipboard.setStringAsync(v);
    setMsg("Copied");
  };

  const open = async () => {
    if (!(await auth())) return setMsg("Face ID required");
    try {
      setOpened(await openParcel(parcel));
      setMsg("Parcel opened — confirm ordinal + fingerprint out-of-band");
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "open failed");
    }
  };

  return (
    <ScrollView className="flex-1 bg-zinc-950" contentContainerClassName="p-5 gap-4">
      <View className="flex-row items-center justify-between">
        <Text className="text-zinc-100 text-xl font-semibold">KEK Escrow</Text>
        <Pressable onPress={() => router.back()}><Text className="text-blue-400">Back</Text></Pressable>
      </View>
      <Text className="text-zinc-400 text-sm">This device can hold a KEK recovery share. The custodian key never leaves this iPhone.</Text>

      <Pressable onPress={genKey} className="bg-blue-600 rounded-xl p-3 items-center">
        <Text className="text-white font-medium">Custodian Key (Face ID)</Text>
      </Pressable>
      {!!pub && (
        <Pressable onPress={() => copy(pub)} className="bg-zinc-900 rounded-xl p-3">
          <Text className="text-zinc-500 text-xs mb-1">CUSTODIAN PUBKEY — tap to copy</Text>
          <Text className="text-zinc-200 font-mono text-xs" selectable>{pub}</Text>
        </Pressable>
      )}

      <Text className="text-zinc-300 font-medium mt-2">Open a parcel</Text>
      <TextInput value={parcel} onChangeText={setParcel} placeholder="Paste base64 VEILSHR1 parcel"
        placeholderTextColor="#52525b" multiline autoCapitalize="none" autoCorrect={false}
        className="bg-zinc-900 rounded-xl p-3 text-zinc-100 font-mono text-xs min-h-20" />
      <Pressable onPress={open} className="bg-blue-600 rounded-xl p-3 items-center">
        <Text className="text-white font-medium">Open Parcel</Text>
      </Pressable>
      {!!opened && (
        <View className="bg-zinc-900 rounded-xl p-3 gap-1">
          <Text className="text-zinc-100">Share {opened.ordinal} of ceremony {opened.fp}</Text>
          <Pressable onPress={() => copy(opened.shareHex)}>
            <Text className="text-zinc-400 text-xs">Tap to copy share hex (only to feed `veil key combine`)</Text>
          </Pressable>
        </View>
      )}
      {!!msg && <Text className="text-zinc-400 text-sm">{msg}</Text>}
    </ScrollView>
  );
}
