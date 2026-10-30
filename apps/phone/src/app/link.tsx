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

import { approveDeviceLink, claimDeviceLink, createDeviceLink, getDeviceLink, getRecoveryShare } from "../lib/api";
import {
  acceptOffer,
  custodianPubHex,
  decodeWireBytes,
  openWrappedMaster,
  persistLocalWrappedMaster,
  sealForRecipient,
  unhex,
} from "../lib/escrow";

const MODE_NEW = "new", MODE_APPROVE = "approve";

export default function Link() {
  const { mode } = useLocalSearchParams<{ mode?: string }>();
  const [perm, requestPerm] = useCameraPermissions();
  const [msg, setMsg] = useState("");
  const [token, setToken] = useState("");
  const [link, setLink] = useState<{ token: string } | null>(null);
  const [claimed, setClaimed] = useState<string | null>(null);
  const [sealed, setSealed] = useState<string | null>(null);
  const poll = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => () => { if (poll.current) clearInterval(poll.current); }, []);

  const begin = async () => {
    try {
      const pub = await custodianPubHex();
      const l = await createDeviceLink("iphone", Array.from(unhex(pub)));
      setLink(l);
      poll.current = setInterval(async () => {
        try {
          const c = await claimDeviceLink(l.token);
          if (c.sealed) {
            if (poll.current) clearInterval(poll.current);
            setClaimed(c.sealed);
            const masterHex = await acceptOffer(c.sealed);
            await persistLocalWrappedMaster(unhex(masterHex));
            setMsg(`linked — custody on this device (${masterHex.slice(0, 8)}…). Sign in to fill.`);
            router.replace("/sign-in");
          }
        } catch { /* not approved yet — keep polling */ }
      }, 3000);
    } catch (e) { setMsg(e instanceof Error ? e.message : "link failed"); }
  };

  const approve = async () => {
    try {
      // Fetch the pending link — the new device's custodian pubkey lives
      // here, so a QR photo alone can't redirect the handoff.
      const linkRow = await getDeviceLink(token.trim());
      // This device's custody wrap (org_key_shares, sealed to its
      // custodian pubkey) — open it, re-seal to the link's pubkey.
      // Server only ever moves ciphertext.
      const share = await getRecoveryShare();
      if (!share.enrolled || !share.wrapped_master) throw new Error("no custody share on this device");
      const master = await openWrappedMaster(decodeWireBytes(share.wrapped_master));
      const sealed = await sealForRecipient(master, new Uint8Array(linkRow.pubkey));
      await approveDeviceLink(token.trim(), Array.from(sealed));
      setMsg("approved — the new device can claim its wrap");
    } catch (e) { setMsg(e instanceof Error ? e.message : "approve failed"); }
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
          <Pressable accessibilityRole="button" onPress={begin} className="bg-blue-600 rounded-xl p-3 items-center">
            <Text className="text-white font-medium">Create Link QR</Text>
          </Pressable>
          {link ? (
            <View className="items-center bg-white rounded-2xl p-4">
              <QRCode value={link.token} size={180} />
            </View>
          ) : null}
        </>
      )}
      {msg ? <Text className="text-zinc-400 text-sm">{msg}</Text> : null}
      {claimed ? <Text className="text-emerald-400 text-sm">custody linked</Text> : null}
    </View>
  );
}
