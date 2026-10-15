# Chrome Web Store — Veil listing

Package: `~/.veil/store/veil-chrome-0.0.1.zip` (run `make fill-pack` after `veil fill install` to rebuild)

- Publisher: Organization 13 Inc. (Vortex)
- Category: Productivity → Tools
- Language: English
- Privacy policy: https://veil.nyc/privacy
- Homepage: https://veil.nyc

## Name
Veil

## Short description (132 chars)
Credential broker for agents. Fill sign-ins without exposing secrets — the host injects, the agent never sees plaintext.

## Detailed description
Veil is an agent-first credential broker. Save logins once; fill them in the browser through Touch ID-gated native fill, or let your coding agent Use them through an inject that never hands over the plaintext.

- Browser autofill for Chrome — pick a matching item, Touch ID or device credential confirms, fields fill
- Agents call `veil use` / MCP — grants decide what each agent may touch; secrets are injected at the socket, never marshalled
- Passkeys, TOTP, save-on-submit, cross-device approval on iOS and Android
- Per-item, time-bounded grants — not org-wide membership

## Permission justifications
- nativeMessaging: required — the extension hands every fill to the local Veil host; secrets never enter browser storage
- tabs: required — matches vault items to the current tab's origin
- scripting: required — injects the field-level fill payload on the page you approve
- storage: required — caches non-secret item metadata (names, URLs) for the chooser UI
- host_permissions (<all_urls>): required — autofill must be available on any login page; no browsing data is collected or transmitted
- Remote code: none — no remote JS is fetched or evaluated

## Single-purpose statement
Veil fills saved credentials in the browser. Every fill is user-confirmed on-device; secrets never reach the page's scripts or any third party.

## Data usage disclosures
- Collects: none — no analytics, no telemetry, no browsing history
- Transmits: vault item metadata to/from your own Veil org only
