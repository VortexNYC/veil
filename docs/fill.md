# Fill

Canonical fill design. Slices 32–37 implement this. Do not copy KeePassXC-Browser or Bitwarden (GPL-3). Do not copy this into a second vault.

The job is **write values into fields**. Username, password, minted TOTP, card number, expiry, name, address, phone — or answer WebAuthn — after Touch ID. That is the product. We do not place orders. We do not charge cards. We do not build a path for agents to spend money. Origin is the source of truth. The laptop holds an encrypted replica so fill still works with no internet. The extension, OS provider, and phone keyboard are pipes. Secrets never return on MCP, never sit in `browser.storage`, never log.

## Two modes

People already expect two products. Agents driving a browser expect the same two. That is not two architectures. Creating an account is the same two modes against a **new-password** field: offer a generated password (choose) or write it (execute) — and the item is already in Veil.

| | **Choose** | **Execute** |
|---|---|---|
| What the human thinks | I clicked the field. Show me what matches this site (or this app). I pick. | The cursor is in the field. Don’t show a menu. Fill it. Then the password. Then the TOTP field when it appears. |
| Starts | Click on our affordance, or click/focus the field and ask to pick. | Trusted focus on a field after this page/app already matched, and the item is unambiguous. |
| Matching | Same. Name + domain, or app id. Happens *before* any secret moves. | Same match. Secrets move only after Touch ID. |
| Ambiguous (0 or >1 items) | Show the chooser. Empty is honest. | Fall back to choose. Never guess the GitHub org. |
| Agent | Agent can open the chooser the same way a human does. Agent does not receive the secret. | Agent clicks or focuses with a real pointer. Veil writes the fields. Agent still does not receive the secret on MCP. The DOM will contain it — that is fill. |

**Choose** is 1Password’s field icon / QuickType bar.

**Execute** is 1Password’s [agentic autofill](https://www.1password.dev/agentic-autofill) shape we already took in `prior-art.md`: the item is named (or is the only match), fill happens outside the model. We do not return `{password, totp}` to Cursor.

You were not missing a third mode. You were missing the gates that stop execute from becoming “any page that calls `input.focus()` drains the vault.”

### What execute is not

- Not autofill-on-load **as a human default**. Visiting the page matches and may badge the toolbar. It does not write secrets. Agent **session write** is below.
- Not auto-submit **as a human default**. We fill fields. Login submit may be part of an agent session. The pay button is never submitted by us. Fill is not spend.
- Not MCP Use. Agents that need a token for an API already have inject. Fill is for a UI. Cards never inject.
- Not “the agent gets the password so it can type it.” If an agent must log into a website, it moves the cursor; Veil writes the field.
- Not silently rotating a password. `new-password` on a site we already have an item for is change-password: choose, never execute-generate.

### Fill session

Execute is a session, not one `input.value =`.

1. **Match** this URL or app id. No Touch ID. No password. Names, uuids, login hints, flags (`hasTotp`, `hasPasskey`).
2. **Unlock** (Touch ID) when we are about to write a secret. 30s reuse on the host so the chain is one gesture.
3. **Write** username, then password, into the current form (WHATWG `autocomplete`, else `type=password` + email/text in that form).
4. **Stay armed** until 30s elapses, the registrable domain (or app id) changes, or the human cancels.
5. If an `one-time-code` field appears or is focused while armed, write the minted TOTP. Do not round-trip a second Touch ID.
6. If the site invokes WebAuthn while armed, `passkeyCreate` / `passkeyGet`. That is the site’s gesture; the 30s window still covers it.
7. End the session. Drop the values from the content script.

If the TOTP field only exists after Sign in, the human or agent submits. We do not. We fill the OTP field when it shows up inside the window.

## Surfaces

The two modes are the product. The OS API is not the same on every surface. Design the jobs once. Speak whatever primitive that OS actually has. Do not invent Auto-Type as the browser path.

| Surface | Slice | Choose | Execute | Matching key |
|---|---|---|---|---|
| Chrome / Edge / Firefox | **37** (next) | Toolbar or in-field chooser. | Trusted focus (`isTrusted` / real pointer). Unambiguous item. | Registrable domain. Passkeys: exact origin + `relatedOrigins`. |
| Safari Web Extension | **37** | Same jobs. Native messaging is a containing `.app` + `SafariWebExtensionHandler`, not Chrome’s `NativeMessagingHosts` JSON. Bitwarden’s handler is public GPL-3 — physics only, never their Swift. | Same. Containing app stays up; host lifetime is easier than MV3. | Same as other browsers. |
| Native Mac apps (Slack, Xcode, Settings > Passwords) | **32** (later) | `ASCredentialProvider` + identities in `ASCredentialIdentityStore` (QuickType). That *is* choose. | `provideCredentialWithoutUserInteraction` after a tap. Apple does not give “write into any app’s NSTextField” from our process. | Bundle id + associated domains + PMR appID map. |
| Windows native apps | **35** (later) | No Apple-style password provider as of 2026-09-10. Revisit: WebAuthn plugin (passkeys, Win11), Credential UI, whatever shipped. | Auto-Type is the historical hack. Reopen on the slice. Do not ship it to unblock 37. Browsers on Windows are still 37. | App identity if an API exists. Not window title. |
| Linux native apps | **36** (later) | Secret Service is a store, not fill. Revisit portals / whatever shipped. | Same Auto-Type warning. Browsers on Linux are still 37. | `.desktop` id / portal attributes if they exist. Not window title. |
| iOS | **33** (later, no pressure) | System AutoFill bar. Choose. Add a password is the other phone screen. | Selecting a suggestion writes the field. OS-shaped execute. Face ID in that process. | Associated domains + app id. |
| Android | **34** (later, no pressure) | AutofillService / Credential Manager. | Same: pick then write. | Package name + Digital Asset Links. |

Chrome on a Mac is 37, not 32. Safari *pages* are 37. Safari filling *Mail.app* is 32.

Phone is add + fill only. No grants, agents, audit, SSH, or the SPA.

## Apple and Aside

The ecosystem win is not “be iCloud.” It is: **one vault, every surface speaks the same two modes, and where the OS has AutoFill we are the provider; where it doesn’t, we ship the extension.** Apple already does both. Aside does the agent half inside a browser we will not become.

### Aside is not open source

[Aside Password Manager](https://aside.com/features/veil) is a proprietary vault inside their Chromium browser ([docs](https://docs.aside.com/help/veil.md)). GitHub `aside` is unrelated. There is no code to import. We already listed them in `prior-art.md`. What they *document* (2026-09):

| They do | We take | We leave |
|---|---|---|
| Agent has a login *action*. Raw password never enters the model. Fill goes into the page after a URL check. | Execute. MCP still never sees the secret. | Their browser. Their “login action” as a tool that implies the agent is a browser. |
| Suggest matching logins on sign-in pages. | Choose. | Autofill on by default. |
| Access policy: `Always allow` / `While unlocked` / `Never`. Default **Always allow**. Per-item override. No agent fill in incognito. Excluded domains. | Agent fill is a **grant**, not a browser setting. Incognito/excluded is a later preference. | The three-state global toggle. Unlocking the vault must not mean every agent may fill every site. Touch ID stays **per fill session**, not “vault is open.” Default for a cloud agent writing a DOM is deny; they already have MCP inject. |
| MFA / passkey / CAPTCHA stay a human step. Payments and posts wait for approval. | Fill ≠ submit ≠ act. Fill writes fields. We never click Pay. There is no agent spend path. | Building an approval UI for tweets or for placing orders. |
| Treat `google.com` / `youtube.com` / `gmail.com` as equivalent. | Affiliated domains for **choose** only. See Apple. | Silent execute across affiliated domains. |
| Touch ID to unlock the vault; agents then use `While unlocked`. | Touch ID before a secret leaves, 30s reuse. | Vault-unlocked = agents roam. |
| Import 1Password, Apple Passwords, Bitwarden, Chrome, … | Later. Displacement needs import. | Doing it in 37. |
| Become the default password manager **in Aside**. | Become the OS default on 32/33/34. In Chrome we are an extension, not the browser. | Forking Chromium. Copying 1Password’s native-messaging JSON into someone else’s profile (they document that hack). |

Aside’s comparison table claims 1Password is only “partial” on agent autofill. 1Password’s agentic autofill is HITL + fill-outside-the-model — the same shape. We already took that. Aside’s edge is they *own the browser*, so they can execute without a store extension. We will not.

### How Apple actually built it

Consumer picture: [Passwords app](https://support.apple.com/en-us/120758) (iOS 18 / Sequoia) is the human UI. iCloud Keychain is sync to approved devices. AutoFill is the pipe. Passkeys, passwords, and verification codes live in **one** store. Shared Groups are family. Windows is [iCloud for Windows + a Chrome/Edge extension](https://support.apple.com/guide/icloud-windows/set-up-icloud-passwords-ict5d0b63853/icloud). Third-party browsers on Mac also get an extension — Apple says so in that article. They already conceded: **Safari is AutoFill; everyone else is native messaging.** That is our 37. Trying to make Chrome on Windows feel like Safari without an extension is fighting Apple’s own architecture.

How it is meant to work ([credential provider extensions](https://support.apple.com/guide/security/credential-provider-extensions-sec6319ac7b9/web), [Password AutoFill security](https://support.apple.com/guide/security/password-autofill-security-sec7aefe77c3/web), [ASCredentialProviderViewController](https://developer.apple.com/documentation/authenticationservices/ascredentialproviderviewcontroller)):

1. **Metadata is not the password.** The extension may hand the OS website + username for QuickType. It must not hand the password. iOS/macOS call the extension again when the human actually fills. That is our `match` vs `fill`. Credential metadata lives in the provider app’s container and dies when the app is uninstalled.
2. **Choose in the OS, not in our window.** Push identities into `ASCredentialIdentityStore`. The QuickType bar shows matches. `prepareCredentialList` is the full picker. Apple: “exposes no credential information to an app until a user consents.” Lists are drawn out of *our* process, not Safari’s.
3. **Execute after a tap + biometrics.** `provideCredentialWithoutUserInteraction` after the human picks a suggestion. Face ID / Touch ID before access. TOTP has `prepareOneTimeCodeCredentialList`. Passkeys register and assert through the same extension. Safari on Mac: click the username field, pick the account, Touch ID writes it. Yellow highlight means filled.
4. **Match web ↔ app.** Associated domains (`apple-app-site-association`) when the app opted in — then Safari-saved logins appear in the app with no extra API. For apps that didn’t, Apple ships [`apple-appIDs-to-domains-shared-credentials.json`](https://github.com/apple/veil-resources/blob/main/quirks/apple-appIDs-to-domains-shared-credentials.json). Every credential provider gets this. That is the magic. It is not a secret Safari hook.
5. **Affiliated websites.** [`shared-credentials.json`](https://github.com/apple/veil-resources/blob/main/quirks/shared-credentials.json) (MIT). Apple’s rule: **suggest** a credential saved for site A on site B with the source named. **Do not release a secret to B without explicit review.** Affiliated → choose. Execute stays the saved domain unless the human picked the affiliated item this session.
6. **Save and generate are OS AutoFill too.** `ASSavePasswordRequest` / `ASGeneratePasswordsRequest` now exist on the same view controller. That is how Apple’s vault fills. Slice 32 later. Not 37 v1.
7. **Don’t invent a field catalog.** WHATWG `autocomplete` / `textContentType` (`username`, `password`, `newPassword`, `oneTimeCode`). PMR quirks are matching, password-rules, change-password URLs, 2FA-appended-to-password (don’t auto-submit), payment iframes (don’t save). Not `sites.js`.
8. **iCloud Passwords on Chrome** is a native-messaging helper Apple signs and allowlists via `web-browser-extension-distribution-information.json`. Same physics as our Go host. They hold the port. They don’t dump the vault into the extension.

Open source we can actually consume: [`apple/veil-resources`](https://github.com/apple/veil-resources) (MIT). Depend on the JSON. Contribute quirks back. Do not vendor a copy we drift. Do not treat it as field detection.

### Winning across OSs

Copy Apple’s *shape*, not iCloud:

| Apple | Veil |
|---|---|
| One Passwords store: password + passkey + TOTP | Origin items. Already true. |
| AutoFill provider is a Settings default | Slice 32/33/34. Become that. |
| Safari AutoFill is first-party; Chrome/Edge/Firefox/Windows get an extension | Slice 37. We ship the extension. Apple does too. |
| Associated domains + appID quirks | `match` takes URL **or** app id. Consume PMR. |
| Shared Groups | Human grants. Already true. Not a family vault type. |
| Approve a new device | Origin login + device pairing. Already true. |
| Strong password on account create + save | Same job. Host mints (`passgen` + site rules), origin stores the item, then we fill. Apple’s `ASGeneratePasswordsRequest` / `ASSavePasswordRequest` is 32. | A generator toy that doesn’t save. Recipe UI in the extension. |
| Wi-Fi passwords, AirDrop, Sign in with Apple, breach dashboard | Not this product. |

Windows and Linux native-app fill still have no Apple-style provider. Browsers there are 37. Reopen 35/36. Do not Auto-Type to fake ecosystem magic.

## Trust

Four pipes, one direction:

```
page or native field  ←  this session’s values
  ↑
extension / ASCredentialProvider / AutofillService
  ↑  JSON (browser) or OS credential API (native)
Go host  (Touch ID, remint, speak origin)
  ↑  POST /v1/fill/*  human Bearer
origin
```

- **Choose/match** may run on every navigation. It returns no secrets.
- **Fill** runs after Touch ID. Content script (or OS provider) receives values for this session only and writes them.
- Binding on Chrome: `allowed_origins` on `nyc.veil.fill`, written by `fill install`. nacl existed because KeePassXC does not ship the store listing. We do. A compromised *our* listing still gets secrets; nacl would not have saved that.
- **Trusted focus.** Page script `input.focus()` is not execute. Real pointer / `isTrusted` / our toolbar click is. CDP/Browser Use clicks that are real mouse events count. `element.focus()` from the page does not.
- Fill the visible tab (or the OS-provided field). Re-read URL / app id at dispatch. If it moved, abort. Serialize per frame. No untrusted iframe.
- Host process must outlive the Chrome MV3 service worker or the 30s Touch ID window is a lie. Hold the native port. Do not invent launchd for this. Safari’s containing app already stays up.
- Same Go binary. `nyc.veil.fill` (JSON, product). kpxc nacl remains in the binary until a later delete; it is not installed. Safari never speaks nacl.
- No shell shim. Binary + `fill.json` (origin and remint file paths, never the password).

## System

This is the machine. Product above does not change. If a later sentence fights this section, this section wins. Drawings: `docs/fill-diagrams/`.

### Cuts (2026-09-11)

Locked after walking the drawings. These beat older sentences in this file.

1. **No session submit.** Not “default off.” Delete it. An agent that needs Sign in clicks Sign in. That is the cursor. Write-without-submit is still execute. Submit is the incident (2FA-appended-to-password, pay buttons, the gate list).
2. **After replica exists, fill is always local.** Origin is sync + create only. No second decrypt path via `POST /v1/fill/logins {uuid}`. Freshness is pull-then-fill. TOTP mints from the local seed. Until replica ships, online fill still decrypts **one** uuid on origin.
3. **Match never hits origin on every navigation.** The host keeps a **RAM index** (item metadata, process lifetime). One `GET /v1/items` at host start / remint. Then `match` is local. That is not a replica. It does not need AEAD. It unblocks Chrome fill without airplane mode and without hammering origin. Replica later replaces RAM with `replica.box`.
4. **Warm replica + Touch ID fills.** A dead JWT with a reachable origin is not a different human. Remint in the background. `need_login` only when we cannot fill: no replica (and no RAM secret — RAM has no secrets) **and** origin will not decrypt. Before replica, online fill still needs origin, so a dead JWT then *is* `need_login`. A Mac unlock still does not authorize a cloud agent.
5. **Confirm `scope` waits for cards.** Global 30s reuse is enough for GitHub → TOTP → passkey. Per-registrable-domain is load-bearing once CVV exists (GitHub must not waive Amazon). Do not expand `Prompt` until slice cards.
6. **Host JSON v1 is `ping` `match` `fill`.** Do not block the Chrome wedge on generate or passkeys. Those are later cases on the same dispatcher. kpxc nacl listing is not installed.

### Capability map

One initiative. Modules a later agent can cut without rewriting the others.

| Module | Responsibility | Depends on |
|---|---|---|
| `kinds` | `card` / `identity` on `protocol.ItemKind`. `Fillable()` ≠ `Injects()`. Envelope fields for PAN/address. Login username is **metadata** on `protocol.Item`. | — |
| `host-json` | Native name `nyc.veil.fill`. uint32-LE JSON. `ping` `match` `fill` `passkeyCreate` `passkeyGet`. Later: `generate`. Same Go binary. No nacl. | `kinds`, `confirm` |
| `confirm` | Touch ID before a secret leaves. 30s reuse. Global until cards. Then per registrable domain / app id. CVV never reuses. Cancel fail-closed. | — |
| `origin-fill` | Human Bearer only. Until replica: extend `POST /v1/fill/logins` with `uuid` + `mintTotp` so 37 decrypts **one** item. After replica: that call is unused; fill is local. kpxc URL form stays until we kill that listing. `POST /v1/fill/sync` is the replica pipe. Not OpenAPI. Not MCP. | `kinds` |
| `index` | RAM metadata on the host. One `GET /v1/items` at start / remint. `match` reads this. Dies with the process. Not a replica. | `origin-fill` |
| `replica` | Sealed box next to `fill.json` (`replica.box`). XChaCha20-Poly1305 of the **whole** catalog + material. Wrapping key is 256-bit random in macOS Keychain (`WhenUnlockedThisDeviceOnly`, fill-host ACL). Not `device.key`. Not plaintext sqlite names/URIs. Replaces RAM for match after unlock. Fill is local. Origin is sync + create. | `origin-fill`, `confirm`, `index` |
| `session` | Fill session on the **host process**: `(tabId, url at start, uuid?, write)`. Survives MV3 worker death. Never submit. | `host-json`, `confirm` |
| `extension` | Thin MV3. Background holds the port, re-reads `tab.url`, runs `match` on navigation. Content writes fields. Content never talks to the host or origin. | `host-json`, `session` |
| `safari` | Containing `.app` + original Swift handler that forwards the same JSON to the same Go process. | `host-json` |
| `as-credential` | Slice 32. Same jobs, OS chooser. | `session` jobs only |

Build order stays **Increments** at the bottom. Chrome execute against origin is proven (CFT + branded 2026-09-14). Replica is live. Cards + identities are CFT-proven; branded HTML checkout wrote field lengths 2026-09-15. Import + owner create is written and live (`POST /v1/import`, `POST /v1/items` card/identity/login). Display names are not unique; import always mints an item id so it cannot clobber `github`. 1pux SSH (114) and secure notes (003) import as `ssh` / `file`. Family member create is written (`OwnerUser` stamp; list/fill is owned ∪ granted; grant list is owner). Typed-save and TOTP-from-page are written on Chrome (`save` / `enrollTotp`; origin `POST /v1/fill/totp/enroll`). Do not start Firefox or Safari. Firefox is a pipe, not the limiter.

### What already exists

Do not rebuild these:

- Origin items, grants, human JWT remint, `POST /v1/fill/logins|totp|passkeys/*` (URL form decrypts **all** matches; `{uuid, mintTotp?}` decrypts **one**)
- Go host speaking kpxc nacl as `org.keepassxc.keepassxc_browser` — uint32-LE JSON framing already, then a nacl box. Same binary also speaks `nyc.veil.fill` (uint32-LE JSON, no box): `ping` `match` `fill`. RAM index from one `GET /v1/items` at start/remint.
- `fill.json` next to the binary, no shell wrapper
- Touch ID in `internal/confirm`. `Host.confirm` reuses 30s per registrable domain. CVV never reuses.
- `store.SQLite` owner-DEK seal, `item_versions`, `device.key` + wraps
- `internal/passgen`, `material.Envelope.Login` and `protocol.Item.Login` — create/update copy it onto the item and the envelope
- `GET /v1/items` already returns metadata (id, name, kind, uris, has_totp, login) and no secret
- `grant.HostAllowed` already matches URI
- `openApp` refuses when `VEIL_ORIGIN` is set — that footgun stays closed

What is **wrong** for 37, in this tree, today:

1. kpxc `FillLogins` decrypts **every** URI match and returns passwords. Toolbar badge cannot use it. `nyc.veil.fill` `match` does not decrypt. Kill kpxc when Chrome JSON is proven.
2. kpxc `FillLogins` skips `!Injects()`, so cards/identities/passkeys would never fill through that function. Cards wait for increment 8.
3. Old items created before `Item.Login` have an empty login until updated. Empty is honest. Do not decrypt the vault to backfill.
4. Login in the host JSON is not a stored kind — website passwords are `api_key` with a URI. Leave that. Do not add `ItemLogin` until a real bug forces it. The chooser labels them `login`.
5. `POST /v1/fill/sync` is live on `veil` production 2026-09-14 (`f679407e`, git `2d60a89`). Human 200, 10 rows with material, agent 403. Kinds today: `api_key`, `passkey`. Card/identity rows appear when those items exist. `replica.box` appears on the laptop after the next fill-host ping (Keychain unwrap). Copying the box without Keychain is useless.
6. kpxc listing is still installed nowhere; leave it dead. Do not `fill install` against production `fill.json`.

### Processes

```
page
  ↑ content script (fields only, values this session, then drop)
extension background (port, tab.url, chooser UI, no secrets at rest)
  ↑ uint32-LE JSON, host name nyc.veil.fill
Go host  (Touch ID, session, Keychain replica key, remint)
  ↑ replica.box (sealed catalog + material; not device.key)
  ↑ TLS + human Bearer, only when sync/generate needs origin
origin   (source of truth)
```

Cloud agents / MCP / `https://veil.nyc/mcp` are not on this diagram. They already have inject. They never call `/v1/fill/*`.

The replica is not a product vault and not `store.SQLite` with a `device.key` beside it. That is a key-on-disk; an agent who can read the home directory decrypts instantly. CLI `openApp` still errors when `VEIL_ORIGIN` is set. MCP does not open the box. The only reader is the fill host, after Keychain unwrap. Stolen `replica.box` is ciphertext with a key that is not a password — there is nothing to brute-force.

### Decrypt boundary

**Match never decrypts.** Not on origin. Not on the host. Navigation is frequent; decrypting the vault to badge a toolbar is how you lose.

`match` reads replica metadata: uuid, name, `Item.Login`, kind, uris, hasTotp, hasPasskey, archived. Cards and identities with no URI always appear (they are not site-bound). Cards/identities *with* URIs use `grant.HostAllowed`. Logins/passkeys use `HostAllowed` plus Apple PMR affiliation **for choose only**. Execute refuses a merely-affiliated uuid.

**Login is metadata.** Promote `protocol.Item.Login` (fill username). Empty is honest. It is not a secret. MCP `list_items` may see it. The envelope keeps a copy so kpxc `FillLogins` does not break. Choose cannot depend on decrypt.

**Fill decrypts one item, after Touch ID.** Until replica: origin `POST /v1/fill/logins` with `uuid` + optional `mintTotp`. After replica: host `Secret(uuid)` locally; origin is not on this call. 37 always sends `uuid` (or the host already knows the one unambiguous uuid from match). No uuid keeps today’s kpxc “all matches, passwords in the body” — kill that listing when Chrome JSON is proven.

Online fill before replica: origin, one uuid. After replica: local, then background pull. Unreachable + empty replica = honest empty. Dead JWT: if replica can fill, fill, remint in the background; if fill still needs origin, `need_login`.

### Replica

Origin stays the source of truth. Airplane mode is a requirement.

**What we will not do:** copy origin’s master onto the laptop so replica bytes equal Railway bytes. Leave a `device.key` (or any unwrap key) next to the ciphertext. Leave names, URIs, logins, or material in plaintext sqlite columns. That is not how 1Password or Bitwarden store a locked vault, and it is how an agent brute-forces nothing because they already have the key.

**What we will do:** one `replica.box`, `crypto.Seal` of the entire catalog + envelope JSON. Wrapping key is random 32 bytes in the platform credential store (macOS Keychain, this-device, trusted fill host only). Unlock is in-process memory. Pull copies **item records + material** over TLS (human Bearer), then re-seals the box. Same item IDs. Origin’s master stays on origin. Push queued mutations later. Conflict: origin `item_versions`, last write wins. Not a CRDT.

`POST /v1/fill/sync` is the one new origin path. Human only. Not OpenAPI. Not MCP. `{ since }` cursor in, `{ items: [{item, material}], cursor }` out. Material is the envelope JSON (password, seed, PAN) on this TLS call — the same trust as today’s `fill/logins`. The host seals before the bytes hit disk. Incremental. First pairing of a new device is a full pull, online.

Hot path:

| Event | Origin | Host |
|---|---|---|
| Host start / remint | `GET /v1/items` once | RAM index |
| Navigation `match` | no | RAM, then replica |
| Execute `fill` (before replica) | one uuid | — |
| Execute `fill` (after replica) | no | local Secret(uuid); pull is background |
| Generate + save | yes if reachable | write-through after / queue |
| Airplane `fill` | no | replica |
| Dead JWT, replica warm | remint in background | fill |
| Dead JWT, no replica | `need_login` | — |

A Mac unlock does not authorize a cloud agent. Sync is not Connect. The replica is not in `browser.storage`.

### Session (host, not JS)

MV3 workers die. Chrome native messaging keeps one host process while the port is held. Session state lives there.

```
idle
  → start (toolbar, Cmd-\, agent login action)  bound (tabId, url0)
  → match (RAM / replica metadata)
  → (optional) choose
  → confirm (Touch ID; global 30s until cards)
  → write fields
  → armed (ReuseWindow)
       TOTP field appears → mint + write, no second prompt
       WebAuthn → passkey*, same window
  → end (timeout, domain change, tab close, cancel, port drop)
```

There is no submit step. The agent clicks Sign in. There is no checkout session.

Content script holds values only during write, then drops them. Background never logs them.

### Confirm

`Host.confirm` reuses 30s globally. KeePassXC-Browser still calls fill, then totp, then passkeys as separate messages — they share that window. `scope` waits for cards. CVV never reuses.

Confirm chrome is **Veil Access Requested**, not Apple's stock LocalAuthentication card and not 1Password. Same Go host. Not a desktop app. Not slice 32.

- Title: Veil Access Requested
- Requester icon (Chrome, Ghostty, …) — check — Veil mark
- **Allow {App} to {fill a sign-in | fill a card | get CLI access | …}**
- Account row is `VEIL_LOGIN_EMAIL` (one org; chevron is visual)
- Cancel fail-closed. **Authorize with Touch ID** then device-owner auth.
- Fill uses this sheet. Interactive human CLI (`item`, `import`, grants) uses **Allow {App} to get CLI access**. Remembered per bundle id in Keychain (`nyc.veil.cli`, WhenUnlockedThisDeviceOnly, no UserPresence). Agents, pipes, and `VEIL_FILL_TOUCHID=0` skip. Fill remint does not go through that CLI gate.

`confirm.Prompt(reason)` until cards. Then `Prompt(reason, scope, reuse)`:

- `scope` = registrable domain or app id
- password / TOTP / passkey: `reuse=true`, 30s, same scope
- CVV: `reuse=false` always
- mismatch of scope ends the window

Cancel = empty fill, fail closed. `Host.Confirm == nil` is deny. Tests attach an explicit fake; they do not skip confirm.

### Kinds and envelope

```
Injects()  Use / child env
Fillable() host fill / OS fill
```

| Kind | Injects | Fillable |
|---|---|---|
| `api_key` (website password today) | yes | yes, if URI matches |
| `oauth` | yes | no (not a form) |
| `ssh` | no | no |
| `file` | no | no |
| `passkey` | no | yes (WebAuthn, not a password field) |
| `card` | no | yes |
| `identity` | no | yes |

`material.Envelope` grows card + identity fields. PAN/CVV/address never on `protocol.Item`, never list JSON, never MCP, never OpenAPI. `Fillable` does not mean execute-without-choose: cards still default to choose when any card exists.

### Origin HTTP

Still not on OpenAPI. Still human Bearer.

| Call | 37 | kpxc listing |
|---|---|---|
| `POST /v1/fill/logins` `{url}` | do not call | passwords for URI matches |
| `POST /v1/fill/logins` `{uuid, mintTotp?}` | 37 fill **until replica** · one item · mint TOTP in this body | unused |
| `POST /v1/fill/totp` | unused after mint-in-fill (and unused after replica) | 6-digit code |
| `POST /v1/fill/passkeys/*` | host `passkey*` | host `passkeys-*` |
| `POST /v1/fill/sync` | replica pull | unused |
| `POST /v1/items` | generate + save; member creates a login they **own**; owner stamps org | unused |
| `GET /v1/items` | host start / remint → RAM index. **not** per navigation | unused |

Audit on origin: choose/execute/generate, uuid + url + kind, no secret.

Do not add `/v1/fill/match` or `/v1/fill/cards`. Match is local. Cards are a kind on `fill` by uuid.

### Extension

Permissions: `nativeMessaging`, `scripting`, enough tabs to re-read `tab.url`. Passkey intercept is the `<all_urls>` tax. Not `cookies`, not `webRequest`, not `clipboardWrite`.

Chooser UI is in the extension. Recipe UI is not. Passwords are not in `browser.storage`. Field maps are not v1.

Safari is the same JSON, different launcher. Original Swift. Not Bitwarden’s file.

### Failure modes we are designing against

- Page JS `input.focus()` → not execute (trusted focus).
- Hidden form on load → match may badge; no write.
- Origin down, replica warm → fill. Origin down, replica empty → empty, not a fake vault.
- Dead JWT, replica warm → fill, remint in the background.
- Dead JWT, no replica → `need_login` (online fill still needs origin).
- Two devices edit one item → origin history, last write wins.
- Agent grant on a card → still fill fields only. No submit. No inject.
- Host process dies → session ends. No secret on disk except replica ciphertext.
- Affiliated domain → choose, never silent execute.

### Deleted from this design

- Origin master on the laptop
- Match hitting origin on every navigation
- Session submit, login or otherwise
- `/v1/fill/match`, `/v1/fill/cards`, a fifth *fill verb*
- `ItemLogin` as a stored kind
- Session state in the MV3 worker or in `browser.storage`
- CRDT replica
- Level-2 card grant, pay-button submit, buy action
- nacl on `nyc.veil.fill`
- Blocking the Chrome wedge on generate or passkeys
- Confirm `scope` before cards exist

## Protocol

uint32 LE + UTF-8 JSON. No box. Native host name `nyc.veil.fill`.

```
ping                         -> { version }

match  { url | app }         -> { entries: [{ uuid, name, login, kind, hasTotp, hasPasskey, savedFor, affiliated? }] }
                             kind is login | card | identity | passkey
                             (stored website passwords stay api_key; chooser says login).
                             no Touch ID. no decrypt. no PAN, no password, no totp code.
                             cards/identities are not site-bound unless tagged.
                             host reads the RAM index, then replica.
                             origin is not on this call (one GET at host start).

fill   { url | app, uuid? }  -> { entries: [{ kind, login, password, totp, uuid, name,
                                             number, expMonth, expYear, cvv,
                                             givenName, familyName, address, ... }] }
                             Touch ID first. only the fields that kind needs.
                             card cvv only if the item has it; still Touch ID.
                             uuid omitted only when match was length 1 *and*
                             that entry is not merely affiliated *and*
                             not a card (cards default to choose if >0).
                             execute never silently uses an affiliated match.
                             empty login is honest.

generate { url | app, login?, passwordRules? } -> { uuid, name, login, password }
                             new-password field only.
                             Touch ID first. cancel is no mint and no POST.
                             then mints on the host (`passgen` + `passwordRules`
                             minlength/maxlength). creates the item on origin.
                             then returns the password. not MCP. not CLI `gen`.
                             existing login match → `choose` (change-password).

passkeyCreate { origin, publicKey, relatedOrigins } -> { response }
passkeyGet    { origin, publicKey }                 -> { response }
```

Cancel / Touch ID deny → empty entries / passkey error. Fail closed.

Content script never calls the host. Background does.

Origin: human Bearer, not OpenAPI, not MCP. Until replica, 37 `fill` is `POST /v1/fill/logins` with `uuid` (and `mintTotp` when the TOTP field is in play). After replica, fill does not call origin. The URL-only form stays for the kpxc listing until we kill it. `POST /v1/fill/sync` is replica pull — the one new path. Do not add `/v1/fill/match` or `/v1/fill/cards`. Keep `totp:"*"` + `POST /v1/fill/totp` only for kpxc. `match` reads the RAM index (one `GET /v1/items` at start / remint), then replica. Never per navigation.

Field maps (which input is username on this domain) are not secrets. v1 does not persist them. If a real site forces it, store on origin (registrable domain + field fingerprint) so it follows the human. Not `browser.storage`. Not `sites.js`.

## Fields

1. WHATWG `autocomplete`: `username` / `email` / `current-password` / `new-password` / `one-time-code` / `cc-*` / name / address / `tel`.
2. Else `input[type=password]` plus the email or text in that form; `cc-number`-shaped fields for cards.
3. Else the in-session chooser — the human points at the field.

`new-password` is generate. `current-password` is fill. Do not guess.

No field catalog. No Bitwarden detector. No kpxc `sites.js`. Matching affiliation is Apple’s MIT `veil-resources` (shared-credentials + appIDs), consumed as data, not as a detector. Password generation rules from the same repo (`password-rules.json`) plus the page’s `passwordRules` attribute.

37 fill first does not: HTTP Basic, auto-submit, fill-on-load, clipboard TOTP as the product, default-veil prompt, **save-banner for a password the human typed**. Generate-on-new-account is the next layer of 37, not a toy popup.

## New account

This is what people love about 1Password on signup. Click the password field on a **new** account. Get a strong unique password. It is already saved. Next visit, fill works. Apple does the same (Suggest New Password, yellow field, save to Passwords). Aside generates too. We already have `internal/passgen` (`crypto/rand`, CLI `gen`, not MCP). Fill is the path that makes the value a vault item.

**The job is generate + save + fill, in that order, on origin.** A generator that does not save is a toy. A save-banner that captures whatever the human typed is a different product (later). Do not generate in the extension. Do not return the password on MCP.

### When

- Field is `autocomplete=new-password` (or Apple `newPassword`). That is the signal. Not a catalog of “signup pages.”
- `current-password` is fill, never generate.
- **Signup:** `match` for this domain/app is empty. Choose offers the suggestion. Execute may generate+save+fill (trusted focus, same gates as fill).
- **Change password:** `match` already has items. Chooser shows existing logins **and** “suggest new.” Execute never generates here — that would rotate the GitHub password because an agent focused the field.
- Confirm fields (`new-password` twice): write the same minted value to both. One item.

### How

1. Touch ID. Cancel is no item and no password on the wire.
2. Host mints with `passgen`. If the page sent `passwordRules`, honor `minlength` / `maxlength` and `required` character classes (`special`, `digit`, `upper`, `lower`). Else the current alphabet (no ambiguous `0/O/1/l`). Apple PMR `password-rules.json` is later — do not vendor a drifting copy in this increment.
3. Create the item on origin (human Bearer, same owner `POST /v1/items` we already have). URL is this page’s registrable domain. Login is the username/email in the form, empty if honest. Name is the host (what the chooser shows). ID is an Infisical-style slug when the host is not already one.
4. Write the password into every `new-password` field (confirm fields get the same value). Same 30s session as fill.
5. If they never submit the form, the item still exists. Same as 1Password. We do not wait for submit (that is `webRequest`; we are not taking that permission for this).

Do not save in: untrusted iframes, payment iframes (PMR list), overwriting an existing uuid without an explicit update gesture.

Agent: cursor only. The password still does not appear on MCP. A cloud agent that needs a login we don’t have uses inject after a human created the item, or this generate path in the browser as the human.

Mac 32: `ASGeneratePasswordsRequest` / `ASSavePasswordRequest` on the credential provider. Same job, OS-shaped.

Do not ship a length/symbols recipe UI in the extension. If a human wants a different shape, the SPA or CLI `gen` then item add. Site rules first.

Passkeys: there is no WebExtensions authenticator API. Intercept `navigator.credentials.create/get` at `document_start`. Original code against the spec. Not kpxc `passkeys.js`. Conditional UI / mediation is later. Untrusted iframes do not get it.

Permissions (browser): `nativeMessaging`, `scripting`, enough tab access to re-read `tab.url`. Passkey intercept is the `<all_urls>` tax. Not `cookies`, not `webRequest`, not `clipboardWrite` unless a later slice proves a need.

## What we already proved (2026-09-11)

KeePassXC-Browser 1.10.3 against origin, isolated Chrome, Go host as Mach-O:

- JWT remint, URI fill, 6-digit TOTP, webauthn.io register then login
- Touch ID before a secret leaves; cancel fail-closed
- Passkey `SignASN1` hashes `authenticatorData || SHA256(clientDataJSON)` first
- Host is the binary + `fill.json`. No shim

That listing is not the product chrome. 37 is.

## Also in. Still out.

Fill + generate is the 1Password-shaped core. A few more things belong in the requirements. Most 1Password chrome does not.

### In — still 37, after fill then generate

- **Keyboard shortcut.** 1Password’s `Cmd-\` / `Ctrl-\`. Chrome `commands`. Fills this tab: execute if unambiguous, else choose. No new protocol.
- **Host locked / JWT dead.** Remint already exists. If we can fill (replica warm), fill and remint in the background. `need_login` opens `login.veil.nyc` only when fill still needs origin and remint failed. Do not store a password in the extension.
- **Change password updates the item.** Chooser “suggest new” on an existing uuid **replaces** that item’s password (we already have item update). It does not create a second GitHub. Generate with no uuid is signup.
- **Fill audit.** Origin records choose / execute / generate with uuid + url + kind. No password, no totp, no seed. Same audit plane as Use.

### In — after 37 is proven on this Mac, before inventing a phone

- **Import, one shot.** Written + origin live. 1Password `.1pux` / CSV onto origin. Display names collide; ids do not. SSH keys and secure notes included. Apple Passwords / Bitwarden / Chrome CSV variants later. Owner. File in, items on origin. Not Connect. Not sync. Not MCP. SPEC’s “do not become their import” means do not become a vault-sync product.
- **Save what they typed.** New login, `current-password` they entered themselves, we offer to save. Same origin create as generate. Accept required. Not a harvest of every password field. Not payment iframes. This is how the vault grows when they ignore generate.
- **TOTP from the page.** After signup the site shows a QR / `otpauth:`. Host enrolls the seed onto the item we just created. Seed never in `browser.storage`. Today that path is CLI `totp enroll`. Humans will not do that twice.

### In — families, or generate is a lie

`POST /v1/items` is human. A family member filling a new Netflix creates a login they own. Org owner is not the only human who may save. Grants still gate what a member can *fill* of someone else’s. Not a second vault type.

### In — later slices, already named

- **32:** default password manager in Settings. QuickType identities. `ASGeneratePasswordsRequest` / `ASSavePasswordRequest`.
- **33–34:** phone add + fill. No SPA on the phone.
- **Never-fill this domain / incognito.** Preference on origin. Not `browser.storage` as the source of truth.

## Cards and identities

**This is fill, not spend.** Checkout fields, not Apple Wallet, not Stripe, not a second vault type, not agents buying things. 1Password calls it a wallet. We call them items: `card` and `identity`. They `Injects() == false`. PAN, CVV, full address never on MCP, never child env, never OpenAPI, never list JSON. We write the values into the fields that ask for them. Same pipe as a login. We do not click Pay / Place order / Purchase. There is no grant, session flag, or agent action that means “buy it.” A filled checkout form sitting there is the same as a filled login form sitting there.

**Card** — holder name, number, exp month/year, brand, optional CVV. Sealed like any secret. We are not a payment processor. We do not charge the card. PCI for a password manager that stores PANs is the same shape 1Password already took: sealed at rest, never logged, never in an agent payload.

**Identity** — given / family name, address lines, locality, region, postal, country, tel, email, organization. WHATWG `autocomplete` tokens (`cc-number`, `cc-exp-*`, `cc-csc`, `given-name`, `address-line1`, `postal-code`, `tel`, …). Same field rule as logins: the standard, then picker.

**Choose** is the default. People have more than one card. Match on a checkout URL returns logins for that merchant **and** the human’s cards/identities (they are not site-bound unless tagged). Empty merchant login is honest; cards still show.

**Execute** into `cc-number` only if there is exactly one card and trusted focus. Never on page load. Never into an untrusted iframe.

**CVV** is a secret, so Touch ID every time it leaves a field. Do not session-reuse CVV across sites. Writing it into `cc-csc` is still fill. Clicking the pay button after that is not.

**Pay-button auto-submit is never on.** Session submit is deleted — login or otherwise. Do not add a level-2 card grant later as a workaround — that *is* agents spending money.

## Session write

Fill-on-load and auto-submit as *globals* are how you drain a vault and buy the wrong thing. They stay off. Session submit is deleted.

The agentic version is not a setting called “autofill on load.” It is a **fill session** bound to `(tabId, url at start)`:

1. Something explicit starts it: toolbar, `Cmd-\`, or an agent **login** action — not “the page loaded,” not a checkout/buy action. There is no buy action.
2. **Session write:** when the form appears in that tab, if the URL’s registrable domain still matches the session start, execute the fill (unambiguous) or wait for choose. This is “the agent navigated to the login and the form showed up,” not fill-on-load for the whole web. Card/identity fields may be written the same way. Writing is not spending.
3. TOTP / passkey still run in the same 30s Touch ID window. The agent clicks Sign in if the site needs a submit.

Human defaults: write on trusted focus or shortcut. Never submit. There is no “buy it” session. The human clicks Place order.

## Replica

See **System → Replica**. Origin is truth. Local `replica.box` is a re-seal of the same item IDs, not a copy of Railway’s master. CLI `openApp` still refuses. MCP still origin.

## Out

Wi-Fi passwords, Hide My Email, Watchtower/breach dashboard, clipboard TOTP as the product, HTTP Basic, caching passwords in the extension, forking Chromium, Auto-Type to unblock 37, length/symbols recipe UI in the popup, pay-button auto-submit, session submit, human fill-on-load, agents spending money / place-order automation, a second *product* vault the CLI can confuse with origin.

## Increments

Each step leaves the tree working. Tests before the next file. Do not scaffold JS until increment 3 exists.

**Already in this tree (do not rebuild):** uint32-LE native messaging + nacl box on `org.keepassxc.keepassxc_browser`; origin `POST /v1/fill/logins` by URL (decrypts every match); `GET /v1/items` metadata including `Login`; `ItemOpts.Login` / create-update `login` already sealed in the envelope and copied onto `protocol.Item`; `App.Match` never `Secret()`; `grant.HostAllowed`; Touch ID per secret, cancel fail-closed; remint; passkeys on the kpxc wire; `passgen`; `store.SQLite` + `device.key`.

1. **Written.** `protocol.Item.Login` + `App.Match`. Login is metadata (sqlite, same shape as `HasTOTP`). Create/update copy it onto the item. Old items: empty is honest. `Match` is `ItemsForPrincipal` + `HostAllowed` and never `Secret()`. kpxc `FillLogins` stays until 3 is proven. Proof: compiled `veil` binary (CLI add/list, `Match` on the same vault, native-messaging `get-logins`) and the same binary against a real origin HTTP (`GET /v1/items` is choose; `POST /v1/fill/logins` is execute). Origin `veil.nyc` is not this SHA until deploy — empty login there is honest.
2. **Written + origin live.** Origin fill by uuid. `POST /v1/fill/logins` `{uuid, mintTotp?}` decrypts **one** item. URL form unchanged for kpxc. `mintTotp` puts a 6-digit in the body; without it `totp` stays `"*"`. Proof: two items same host — URL form returns both, uuid form returns one and not the other secret; native host still speaks URL. Deployed `veil` production 2026-09-11 (`21e5d513`); live `POST {uuid:github}` is 200 / 1 entry; missing uuid and `mintTotp` without uuid are 400; GitHub items still have empty `login` (honest, no backfill).
3. **Written.** `nyc.veil.fill` JSON: `ping` `match` `fill`. Same binary, second manifest. RAM index: one `GET /v1/items` at start/remint. `match` is local. `fill` is Touch ID + origin-by-uuid (host binds URL; origin still does not). Global 30s reuse so fill+TOTP is one gesture. Proof: fake origin — ping loads index once, match never decrypts, two items same host omit uuid → empty, uuid form decrypts one, confirm reuse is one prompt, cancel never POSTs fill. Compiled binary uint32-LE ping/match/fill against the same fake origin. **No JS. No generate. No passkeys on this name.**
4. **Written + CFT-proven with Touch ID. Branded approve and Cancel proven 2026-09-14.** Thin MV3 in `apps/fill`: hold the port, `match` on nav, choose + execute, write fields, `Cmd+Shift+Period` (`chrome.commands` `fill`; remap to `Cmd-\` in chrome://extensions/shortcuts). Host JSON `need_login` on dead JWT. Chrome 154 branded ignores `--load-extension`. CFT headed fill/passkey/generate/webauthn.io with Touch ID on. `TestChromeExtensionFillCancel`: chromedp Click `#user`, System Events Cancel on the Chrome for Testing sheet (not branded "Google Chrome"), `fill-debug` `confirm denied`, password empty. Copy host SHA over `~/.veil/native-host` — do not `fill install` (it rewrites production `fill.json`). Branded veil.nyc Profile 2: chooser `cloudflare-login` → LocalAuthentication → `confirm ok` (not `skipped`/`reuse`) → password written. Same page, Cancel → `confirm denied` 14:52:53 (six denies, no reuse), Password AX `len=0`. Finger is approve; Cancel is deny. 1Password still races the page. Branded soak is human pointer or AXPress/AXSetValue. Do not `agent-cu click --x --y` on daily Profile 2 (Spaces lie). Agents never `POST /v1/fill/logins`. Do not `Runtime.evaluate` `fillTab`. Do not set `HOME` to a tempdir.
5. **Written + CFT-proven with Touch ID. Branded passkeyCreate UV proven 2026-09-14.** `passkeyCreate` / `passkeyGet` on `nyc.veil.fill`. Confirm before origin. Cancel fail-closed. UV flag only after that confirm — `userVerification=discouraged` is not a skip. Content intercepts `navigator.credentials.create/get` at `document_start` in MAIN world. CFT 2026-09-14: fixture create+get and `webauthn.io/?regUserVerification=required&authUserVerification=required` register→`/profile` via chromedp Click `#register-button`. Branded: Shlomo clicked navy Register; `action=passkeyCreate` 14:48:31 then `confirm ok` 14:48:33 (~2s, not skipped/reuse). AXPress does not invoke `credentials.create`. Do not `Runtime.evaluate` create.
6. **Written + CFT-proven with Touch ID. Branded generate proven 2026-09-14.** `generate` on `nyc.veil.fill`. Confirm before mint and `POST /v1/items`. Cancel fail-closed. Existing login match is choose only. Chooser name is the host. `auto()` reads `data-autocomplete` (GitLab signup has no `autocomplete` IDL). CFT: `#pass` on `generate-fixture.html`. Branded: gitlab.com/users/sign_up, Suggest a password, `action=generate` then `confirm ok` ~2s. Do not submit GitLab. Throwaway origin item named as the host — not on origin (human list 2026-09-14). Origin `veil` is 2026-09-14 (`bb562d48`).
7. **Replica. Origin live + branded airplane proven 2026-09-14.** Sealed `replica.box` + Keychain key (`nyc.veil.fill` / `replica`). `POST /v1/fill/sync` human only. Fill after pull does not call origin. Confirm fail-closed. Airplane: fill.json origin `https://127.0.0.1:9`, Cmd+Shift+Period on dash.cloudflare.com/login, `confirm ok` 15:46:14, Password empty then 21 bullets. Box 4379 no catalog leak. `fill.json` restored to `https://veil.nyc`. Do not `fill install`.
8. **Cards + identities. Written + CFT-proven + origin live 2026-09-14 (`f679407e`). Branded Stripe-test checkout wrote fields 2026-09-15.** `ItemCard` / `ItemIdentity` are Fillable, not Injects. Envelope PAN/CVV/address never on list/MCP. Match: unbound cards/identities always appear; URI-tagged ones use HostAllowed. Fill by uuid. Cards without uuid stay choose. Confirm `scope` is eTLD+1. Password/TOTP/passkey reuse 30s same scope. CVV never reuses (a card fill that returns CVV always prompts). Never submit. CFT: `checkout-fixture.html` — click `#number` leaves fields empty; chooser uuid writes number/exp/CVV/name; `#pay` is `type=button` and was never clicked. `identity-fixture.html` chooser writes given/family/street/tel; Continue never clicked. Card fill from origin with replica nil is `POST /v1/fill/sync` by uuid, not `POST /v1/fill/logins`. Live FillSync human 200 / agent 403. Origin item `stripe-test` (`kind=card`, Stripe test Visa). Branded Profile 2 2026-09-14 18:24:50: chooser `stripe-test card` → `confirm ok` with empty fields (MV3 popup was the fill tab; replica empty Number counted as success). 2026-09-15 HTML checkout `http://127.0.0.1:8765/stripe-test-checkout.html`: confirm ok **and** Card number len=16 last4 4242, Month 12, Year 2034, CVC len=3, Name Ada. Chooser URL binds the checkout http(s) tab, not the popup. Replica empty Number falls through to origin; empty fill URL does not confirm. Tests: `tab.test.cjs` (http(s) only), `fields.test.cjs` writeCard on hidden `cc-number`, `TestJSONFillEmptyURLDoesNotConfirm`, `TestJSONCardFillReplicaEmptyFallsBackToOrigin`, `TestJSONCardFillReplicaEmptyOriginDownDoesNotConfirm`. Pay never clicked. Stripe Elements iframes are not this path (`all_frames: false`). Laptop host copied over `~/.veil/native-host`; `fill.json` untouched. Identity branded soak and airplane card are not this gate. Firefox waits.
9. **Import + owner create. Written + origin live 2026-09-15.** One-shot `.1pux` / CSV onto origin (`POST /v1/import`, CLI `item import`). Import always `id.NewItem()` — display names may collide; existing ids (`github`) are never reused. `UNIQUE(org_id, name)` is dropped on open. 1pux 114 SSH → `ItemSSH` (PEM never in list). 1pux 003 note → `ItemFile` text/plain (body never in list). Owner create is `POST /v1/items` with `kind` `api_key` / `card` / `identity`. Card/identity fields are request-only; PAN/CVV/address never in the response, list, or MCP. SPA Add and Import are the same owner calls.
10. **Family member create. Written.** `POST /v1/items` is human. Owner stamps `OwnerOrg`. Member stamps `OwnerUser`. List/fill is owned ∪ granted. Import and grant create/list stay owner. Agent 403. Not a second vault. Proof: `TestMemberCreateOwnsLogin`, `TestMemberCreateItemTheyOwn`, `TestHumanGrantAPI` member `GET /v1/grants` 403. Origin `veil` is a later deploy. **STOP: Firefox / Safari / 32.**
11. Safari `.app` + original Swift. Same JSON.
12. Slice 32 Mac AutoFill.
13. **Typed-save + TOTP-from-page. Written + CFT-proven 2026-09-15.** Chrome. `save` on `nyc.veil.fill` is a current-password they typed on a new login: chooser Accept, then the same `POST /v1/items` as generate. Existing login match is choose (change-password is later). Not a harvest: empty password does not confirm; payment iframes stay out (`all_frames: false`, `canSave` false on `cc-*`). `enrollTotp` parses `otpauth://totp` (QR via `BarcodeDetector` when present). Host enrolls the seed onto the item just created (same registrable scope). Origin `POST /v1/fill/totp/enroll` is human write (`MayWriteItem`); not OpenAPI; response has no seed. Seed never in `browser.storage`. Accept required. Already-enrolled is choose. Fill host Serve stays on the OS main thread (`runtime.LockOSThread` in `cmd/veil`); the Veil access sheet is an `NSWindow` on that thread. Proof: `TestJSONSaveTypedPasswordCreatesLogin`, `TestJSONSaveThenEnrollTotp`, `TestFillTOTPEnroll`, `TestAttachTOTPMemberOwnAndDenyOrg`, `TestFillExtensionDoesNotStoreSeed`, `TestChromeExtensionSave`, `TestChromeExtensionSaveCancel`, `TestChromeExtensionSaveThenEnrollTotp`. Origin enroll is not live until origin deploy. Do not start Firefox / Safari / 32.

Written-not-proven is not done.
