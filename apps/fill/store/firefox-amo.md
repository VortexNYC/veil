# Firefox Add-ons (AMO) & Microsoft Edge Add-ons — Veil listing

Package (Firefox MV3 / Gecko): `~/.veil/store/veil-firefox-0.0.2.zip` (built automatically by `make deploy` or `make fill-pack`)
Package (Edge Chromium MV3): `~/.veil/store/veil-chrome-0.0.2.zip`
Screenshot (1280×800): `~/.veil/store/screenshot-vault-1280x800.png`
Store Icon (128×128): `~/.veil/store/icon-128.png`

- Add-on ID (Gecko): `fill@veil.nyc`
- Publisher: Organization 13 Inc. (Vortex)
- Category: Security & Privacy / Passwords
- License: Custom / Proprietary (Organization 13 Inc.)
- Privacy policy: https://veil.nyc/privacy
- Homepage: https://veil.nyc

## Summary (max 250 chars)
Agent-first credential broker & password manager. Autofill logins, 2FA (TOTP), passkeys, and cards with Touch ID — while coding agents Use credentials at the edge without ever seeing plaintext secrets.

## Reviewer Notes (for AMO / CWS human review)
1. Veil is a native-messaging companion extension for the Veil desktop app (`https://veil.nyc`).
2. The extension communicates exclusively with the local `nyc.veil.fill` native messaging host over stdio (`chrome.runtime.sendNativeMessage("nyc.veil.fill", ...)`).
3. All JavaScript is bundled in the extension archive (`popup.js`, `background.js`, `content.js`, `fields.js`, `ui.js`, `passkeys.js`, `passkeys-page.js`, `tab.js`, `report.js`) with zero minification, zero obfuscation, and zero remote code execution.
