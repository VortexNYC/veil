import nacl from "tweetnacl";

// VEIL-78 member recovery keys. The origin wraps the org master to the
// enrolled pubkey via nacl/box SealAnonymous (device.Offer) — tweetnacl's
// box keypair is the same Curve25519 scalar-mult Go's curve25519.X25519
// uses, so a kit generated here unwraps with the CLI's device.Accept. The
// private half never leaves the browser: it goes into the downloaded kit
// file and nowhere else.
export function generateRecoveryKeypair(): { pub: Uint8Array; priv: Uint8Array } {
  const priv = new Uint8Array(32);
  crypto.getRandomValues(priv);
  const kp = nacl.box.keyPair.fromSecretKey(priv);
  return { pub: kp.publicKey, priv };
}

export function b64(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) {
    s += String.fromCharCode(b);
  }
  return btoa(s);
}

export function hex(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) {
    s += b.toString(16).padStart(2, "0");
  }
  return s;
}

// The kit is modeled on the KEK emergency kit: the private key plus
// restore instructions. It never contains the org master — that stays
// server-side, wrapped to this pubkey in org_key_shares.
export function recoveryKitText(orgID: string, pub: Uint8Array, priv: Uint8Array): string {
  return `VEIL RECOVERY KIT
Organization: ${orgID}
Generated: ${new Date().toISOString()}

Keep this file somewhere safe and offline — a password manager export,
an encrypted drive, or printed and locked away. This is the only copy of
your account recovery private key. Veil cannot re-issue it: if you lose
every device AND this file, only an org owner can confirm an account
recovery for you.

RECOVERY PRIVATE KEY (hex, 32 bytes — keep secret):
${hex(priv)}

PUBLIC KEY (base64 — already enrolled on your account):
${b64(pub)}

To restore with the Veil CLI, save the hex line to a file and either
pass it straight to the lost-KEK path, or decode it to raw bytes for
the proof path:

  veil key recover-org ${orgID} --owner-id <you> --recovery-file <hex file>

  xxd -r -p <hex file> > ~/.veil/recovery.key
  chmod 600 ~/.veil/recovery.key
  veil recovery complete <request-id> --proof-file ~/.veil/recovery.key
`;
}

export function downloadRecoveryKit(orgID: string, pub: Uint8Array, priv: Uint8Array): void {
  const short = orgID.slice(0, 8);
  const blob = new Blob([recoveryKitText(orgID, pub, priv)], { type: "text/plain" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `veil-recovery-kit-${short}.txt`;
  a.click();
  URL.revokeObjectURL(url);
}
