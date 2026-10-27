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
  download(`veil-recovery-kit-${short}.txt`, blob);
}

function download(name: string, blob: Blob): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

function escapeHTML(s: string): string {
  return s
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

// VEIL-71 Emergency Kit — the printable artifact a member locks in a safe,
// modeled on 1Password's. One self-contained HTML file: no external fonts,
// scripts, or images, so it renders and prints identically offline. It
// carries the same material as the text kit — the recovery private key
// plus the enrolled pubkey — framed for a human, not a CLI. It never
// contains the org master or any item secret.
export function emergencyKitHTML(
  orgID: string,
  email: string,
  pub: Uint8Array,
  priv: Uint8Array,
): string {
  const short = orgID.slice(0, 8);
  const generated = new Date().toISOString();
  const rows: [string, string, boolean][] = [
    ["Sign-in address", "app.veil.nyc", false],
    ["Email", email || "(sign-in email)", false],
    ["Organization", `${short}…`, false],
    ["Recovery public key (base64 — already enrolled)", b64(pub), true],
    ["Recovery private key (hex — keep secret)", hex(priv), true],
  ];
  const cells = rows
    .map(
      ([label, value, mono]) => `<tr>
  <th>${escapeHTML(label)}</th>
  <td${mono ? ' class="mono"' : ""}>${escapeHTML(value)}</td>
</tr>`,
    )
    .join("\n");
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Veil Emergency Kit — ${escapeHTML(short)}</title>
<style>
  body { font-family: -apple-system, "Segoe UI", Helvetica, Arial, sans-serif;
         margin: 2.5rem auto; max-width: 42rem; color: #111; line-height: 1.5; }
  h1 { font-size: 1.5rem; margin: 0 0 .25rem; }
  .sub { color: #555; margin: 0 0 1.5rem; }
  table { border-collapse: collapse; width: 100%; margin: 1rem 0 1.5rem; }
  th, td { border: 1px solid #bbb; padding: .5rem .75rem; text-align: left;
           vertical-align: top; }
  th { width: 14rem; background: #f4f4f4; font-weight: 600; }
  .mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
          font-size: .85rem; word-break: break-all; }
  .warn { border: 1px solid #c0392b; background: #fdf1ef; padding: .75rem 1rem;
          border-radius: .35rem; }
  .steps li { margin-bottom: .35rem; }
  code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
         font-size: .85em; }
  footer { color: #777; font-size: .8rem; margin-top: 2rem; }
  @media print {
    body { margin: 0; max-width: none; }
    .noprint { display: none; }
    a { color: inherit; text-decoration: none; }
  }
</style>
</head>
<body>
<h1>Veil Emergency Kit</h1>
<p class="sub">Generated ${escapeHTML(generated)} · keep this page somewhere safe and offline</p>

<p class="warn noprint"><strong>Print or save this page now.</strong>
Use your browser's Print (&#8984;P / Ctrl+P) and choose "Save as PDF" or paper.
This file is the only copy of the recovery private key — closing it without
saving loses the key if no other kit was downloaded.</p>

<table>
${cells}
</table>

<h2>What this is for</h2>
<p>If you lose every signed-in device, this recovery key is the only
self-serve path back into the vault. Without it, an org owner must confirm
an account recovery for you.</p>

<h2>Restore with the Veil CLI</h2>
<ol class="steps">
  <li>Save the hex private key to a file, e.g. <code>recovery.hex</code>.</li>
  <li>Run
    <code>veil key recover-org ${escapeHTML(orgID)} --owner-id &lt;you&gt; --recovery-file recovery.hex</code></li>
  <li>Or decode it for the proof path:
    <code>xxd -r -p recovery.hex &gt; ~/.veil/recovery.key &amp;&amp; chmod 600 ~/.veil/recovery.key</code>
    then <code>veil recovery complete &lt;request-id&gt; --proof-file ~/.veil/recovery.key</code></li>
</ol>

<p>Veil cannot re-issue this key. It is never sent to or stored by Veil —
only the public key above is enrolled on your account.</p>

<footer>Veil · veil.nyc · Emergency Kit for org ${escapeHTML(short)}</footer>
</body>
</html>
`;
}

export function downloadEmergencyKit(
  orgID: string,
  email: string,
  pub: Uint8Array,
  priv: Uint8Array,
): void {
  const short = orgID.slice(0, 8);
  const blob = new Blob([emergencyKitHTML(orgID, email, pub, priv)], {
    type: "text/html",
  });
  download(`veil-emergency-kit-${short}.html`, blob);
}
