// Old-device-approve device linking (Signal-style QR handoff).
// mode=new:      post custodian pubkey → show QR of the link token → poll
//                claim → acceptOffer → this device holds the org master wrap.
// mode=approve:  scan the QR → seal the org master to the new pubkey →
//                approve → server only ever moves ciphertext.
import { CameraView, useCameraPermissions } from "expo-camera";
import { router, useLocalSearchParams } from "expo-router";
import { useEffect, useRef, useState } from "react";
import { Pressable, Text, TextInput, View } from "react-native";
import QRCode from "react-native-qrcode-svg";

import {
  approveDeviceLink,
  ApiError,
  claimDeviceLink,
  createDeviceLink,
  getDeviceLink,
  getRecoveryShare,
} from "../lib/api";
import {
  acceptOffer,
  custodianPubHex,
  decodeWireBytes,
  openOrgMasterForApprove,
  persistLocalWrappedMaster,
  sealForRecipient,
  unhex,
} from "../lib/escrow";

const MODE_APPROVE = "approve";

function linkError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 401) return "session expired — sign in again, then retry approve";
    if (e.status === 400) return e.message || "link invalid or expired";
    return e.message;
  }
  return e instanceof Error ? e.message : "link failed";
}

export default function Link() {
  const { mode } = useLocalSearchParams<{ mode?: string }>();
  const [perm, requestPerm] = useCameraPermissions();
  const [msg, setMsg] = useState("");
  const [token, setToken] = useState("");
  const [link, setLink] = useState<{ token: string } | null>(null);
  const [polling, setPolling] = useState(false);
  const poll = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => () => { if (poll.current) clearInterval(poll.current); }, []);

  const begin = async () => {
    try {
      setMsg("");
      const pub = await custodianPubHex();
      const l = await createDeviceLink("iphone", Array.from(unhex(pub)));
      setLink(l);
      setPolling(true);
      poll.current = setInterval(async () => {
        try {
          const c = await claimDeviceLink(l.token);
          if (c.sealed) {
            if (poll.current) clearInterval(poll.current);
            setPolling(false);
            const masterHex = await acceptOffer(c.sealed);
            await persistLocalWrappedMaster(unhex(masterHex));
            setMsg(`linked — custody on this device (${masterHex.slice(0, 8)}…). Sign in to fill.`);
            router.replace("/sign-in");
          }
        } catch { /* not approved yet — keep polling */ }
      }, 3000);
    } catch (e) { setMsg(linkError(e)); setPolling(false); }
  };

  const approve = async () => {
    const tok = token.trim();
    if (!tok) {
      setMsg("scan the QR or paste the link token");
      return;
    }
    try {
      setMsg("");
      const linkRow = await getDeviceLink(tok);
      const master = await openOrgMasterForApprove(getRecoveryShare);
      const recipient = decodeWireBytes(linkRow.pubkey);
      const sealedBytes = await sealForRecipient(master, recipient);
      await approveDeviceLink(tok, Array.from(sealedBytes));
      setMsg("approved — the new device can claim its wrap");
    } catch (e) { setMsg(linkError(e)); }
  };

  return (
    <View className="flex-1 bg-zinc-950 p-5 gap-4">
      <View className="flex-row items-center justify-between">
        <Text className="text-zinc-100 text-xl font-semibold">Link Device</Text>
        <Pressable accessibilityRole="button" onPress={() => router.back()}><Text className="text-blue-400">Back</Text></Pressable>
      </View>

      {mode === MODE_APPROVE ? (
        <>
          {!perm?.granted ? (
            <Pressable accessibilityRole="button" onPress={requestPerm} className="bg-blue-600 rounded-xl p-3 items-center">
              <Text className="text-white font-medium">Grant Camera</Text>
            </Pressable>
          ) : (
            <CameraView style={{ height: 220, borderRadius: 16, overflow: "hidden" }}
              onBarcodeScanned={({ data }) => setToken(data.trim())} />
          )}
          <TextInput placeholder="link token or paste QR payload" placeholderTextColor="#52525b"
            value={token} onChangeText={setToken}
            className="bg-zinc-900 text-zinc-100 rounded-xl p-3" />
          <Pressable accessibilityRole="button" onPress={approve} className="bg-blue-600 rounded-xl p-3 items-center">
            <Text className="text-white font-medium">Approve Link</Text>
          </Pressable>
        </>
      ) : (
        <>
          <Text className="text-zinc-400 text-sm">Show this QR on an approved device to hand over custody. The master never crosses the server in the clear.</Text>
          <Pressable accessibilityRole="button" onPress={begin} disabled={polling} className="bg-blue-600 rounded-xl p-3 items-center opacity-100 disabled:opacity-50">
            <Text className="text-white font-medium">{polling ? "Waiting for approve…" : "Create Link QR"}</Text>
          </Pressable>
          {link ? (
            <View className="items-center bg-white rounded-2xl p-4">
              <QRCode value={link.token} size={180} />
              <Text className="text-zinc-500 text-xs mt-2 font-mono">{link.token}</Text>
            </View>
          ) : null}
        </>
      )}
      {msg ? <Text className="text-zinc-400 text-sm">{msg}</Text> : null}
    </View>
  );
}
