# ✗ item detail shows masked secret with reveal control

`e2e/tests/vault-web.e2e.ts` · target `web` · failed · 1m 8s

Ran on: browser `chromium 156.0.8078.4` · user agent `Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/156.0.8078.4 Safari/537.36`

**TEST_SETUP_FAILED**

```text
agent.act blocked: devin executor exhausted 8 turns without a verdict
```

Look at: `e2e/tests/vault-web.e2e.ts:14`  

## Steps

1. ✓ `app.open` `/login` (446ms) — `e2e/tests/vault-web.e2e.ts:27`
   - ✗ console error: `Loading the script 'https://static.cloudflareinsights.com/beacon.min.js/vce701823e78c4301a35b7e07294d87301792088814092' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The actio…`
   - ✗ network error: `GET https://static.cloudflareinsights.com/beacon.min.js/vce701823e78c4301a35b7e07294d87301792088814092 csp`
   - ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/static/1.438.8/exception-autocapture.js' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action has been blocked. (at htt…`
   - ✗ network error: `GET https://us-assets.i.posthog.com/static/1.438.8/exception-autocapture.js csp`
   - ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config.js' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action …`
   - ✗ network error: `GET https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config.js csp`
   - ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/static/exception-autocapture.js?v=1.438.8' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action has been blocked. (at h…`
   - ✗ network error: `GET https://us-assets.i.posthog.com/static/exception-autocapture.js?v=1.438.8 csp`
   - ✗ console error: `Connecting to 'https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config' violates the following Content Security Policy directive: "connect-src 'self' https://veil.nyc https://id.veil.nyc https://accounts.veil.nyc https://login.veil.nyc https://us.i.posthog.com"…`
   - ✗ console error: `Fetch API cannot load https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config. Refused to connect because it violates the document's Content Security Policy. (at https://app.veil.nyc/assets/index-Bp02gZL1.js:67)`
   - screen: 1 node at `https://app.veil.nyc/login`
2. ✓ `agent.act` "type `'shlomo@vortex.nyc'` into the ID field" (8.3s) — `e2e/tests/vault-web.e2e.ts:9`
   - cache: no replay: nothing recorded for this step yet; recording not saved: no check passed after this step (`.e2e/cache/a778c4929b0d2976796fb9195dfe4af284ac499e00070ef98bdb954e61b678c0.json`)
   - ⚠ network warning: `GET https://accounts.veil.nyc/sessions/whoami 401`
   - ↪ navigated to `https://app.veil.nyc/login?flow=c5c31e37-c26a-4461-af63-876d7bd05d5b`
   - ✗ console error: `Loading the script 'https://static.cloudflareinsights.com/beacon.min.js/vce701823e78c4301a35b7e07294d87301792088814092' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The actio…`
   - ✗ network error: `GET https://static.cloudflareinsights.com/beacon.min.js/vce701823e78c4301a35b7e07294d87301792088814092 csp`
   - ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/static/1.438.8/exception-autocapture.js' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action has been blocked. (at htt…`
   - ✗ network error: `GET https://us-assets.i.posthog.com/static/1.438.8/exception-autocapture.js csp`
   - ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config.js' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action …`
   - ↪ navigated to `https://app.veil.nyc/login?flow=c5c31e37-c26a-4461-af63-876d7bd05d5b`
   - ✗ network error: `GET https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config.js csp`
   - ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/static/exception-autocapture.js?v=1.438.8' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action has been blocked. (at h…`
   - ✗ network error: `GET https://us-assets.i.posthog.com/static/exception-autocapture.js?v=1.438.8 csp`
   - ✗ console error: `Connecting to 'https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config' violates the following Content Security Policy directive: "connect-src 'self' https://veil.nyc https://id.veil.nyc https://accounts.veil.nyc https://login.veil.nyc https://us.i.posthog.com"…`
   - ✗ console error: `Fetch API cannot load https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config. Refused to connect because it violates the document's Content Security Policy. (at https://app.veil.nyc/assets/index-Bp02gZL1.js:67)`
   - type `"shlomo@vortex.nyc"` into textbox "ID" (29ms)
   - screen: 11 changes since step 1 at `https://app.veil.nyc/login?flow=c5c31e37-c26a-4461-af63-876d7bd05d5b`, 12 nodes
     - `added heading "Veil"`
     - `added heading "Sign in"`
     - `added text="Sign in with your email and password or a security key"`
     - `added text="ID"`
     - `added textbox "ID" value="shlomo@vortex.nyc" purpose=username`
     - `added button "Sign in with hardware key"`
     - `added text="or"`
     - `added text="Password"`
     - `added button "Show"`
     - `added textbox "Password" value=<secure> purpose=password [secure]`
     - `added button "Sign in with password"`
3. ✓ `agent.act` "fill the Password field from secret 'vault-password'" (4.0s) — `e2e/tests/vault-web.e2e.ts:10`
   - cache: no replay: nothing recorded for this step yet; nothing recorded: the step changed nothing a replay could check (`.e2e/cache/0464c8c5c8f8edaace038bb228f0acc99d2209d19292bdd9a110242b0ae8e915.json`)
   - fill secret "vault-password" into textbox "Password" (50ms)
   - screen: unchanged since step 2 at `https://app.veil.nyc/login?flow=c5c31e37-c26a-4461-af63-876d7bd05d5b`
4. ✓ `agent.act` "tap the 'Sign in with password' button" (5.0s) — `e2e/tests/vault-web.e2e.ts:13`
   - cache: no replay: nothing recorded for this step yet; recording not saved: no check passed after this step (`.e2e/cache/27dc185dafaf0df28a7fb6d963f6fad505f12602b0281b70d17836e46723a73c.json`)
   - tap button "Sign in with password" (698ms)
   - ↪ navigated to `https://app.veil.nyc/login?flow=2d59636d-04e0-450b-8ce3-839516e0fdce`
   - ✗ console error: `Loading the script 'https://static.cloudflareinsights.com/beacon.min.js/vce701823e78c4301a35b7e07294d87301792088814092' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The actio…`
   - ✗ network error: `GET https://static.cloudflareinsights.com/beacon.min.js/vce701823e78c4301a35b7e07294d87301792088814092 csp`
   - ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/static/1.438.8/exception-autocapture.js' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action has been blocked. (at htt…`
   - ✗ network error: `GET https://us-assets.i.posthog.com/static/1.438.8/exception-autocapture.js csp`
   - ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config.js' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action …`
   - ↪ navigated to `https://app.veil.nyc/login?flow=2d59636d-04e0-450b-8ce3-839516e0fdce`
   - ✗ network error: `GET https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config.js csp`
   - ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/static/exception-autocapture.js?v=1.438.8' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action has been blocked. (at h…`
   - ✗ network error: `GET https://us-assets.i.posthog.com/static/exception-autocapture.js?v=1.438.8 csp`
   - ✗ console error: `Connecting to 'https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config' violates the following Content Security Policy directive: "connect-src 'self' https://veil.nyc https://id.veil.nyc https://accounts.veil.nyc https://login.veil.nyc https://us.i.posthog.com"…`
   - ✗ console error: `Fetch API cannot load https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config. Refused to connect because it violates the document's Content Security Policy. (at https://app.veil.nyc/assets/index-Bp02gZL1.js:67)`
   - screen: 14 changes since step 3 at `https://app.veil.nyc/login?flow=2d59636d-04e0-450b-8ce3-839516e0fdce`, 12 nodes
     - `added text="Please confirm this action by verifying that it is you. Please complete the second authentication challenge."`
     - `added text="Authentication code"`
     - `added textbox "Authentication code" purpose=one-time-code`
     - `added button "Use Authenticator"`
     - `added text="Recovery code"`
     - `added textbox "Recovery code"`
     - `added button "Use backup recovery code"`
     - `removed text="ID"`
     - `removed textbox "ID" value="shlomo@vortex.nyc" purpose=username`
     - `removed button "Sign in with hardware key"`
     - `removed text="Password"`
     - `removed button "Show"`
     - 2 more
5. ✗ `agent.act` "enter the 6-digit authenticator code from secret 'totp-code' into the Authentication code field and tap Continue" (50.1s) — **TEST_SETUP_FAILED** — `e2e/tests/vault-web.e2e.ts:14`
   > devin executor exhausted 8 turns without a verdict
   - cache: no replay: nothing recorded for this step yet (`.e2e/cache/89d7e0ff91a247de68eee6ba1ea0ba83816d19b0f71f78fb9aee4eac4e26f267.json`)
   - fill secret "totp-code" into textbox "Authentication code" (29ms)
   - tap button "Use Authenticator" (17ms)
   - tap button "Use Authenticator" (46ms)
   - tap button "Use Authenticator" (36ms)
   - tap button "Use Authenticator" (33ms)
   - tap button "Use Authenticator" (34ms)
   - tap button "Use Authenticator" (34ms)
   - tap button "Use Authenticator" (39ms)
   - screen: 1 change since step 4 at `https://app.veil.nyc/login?flow=2d59636d-04e0-450b-8ce3-839516e0fdce`, 12 nodes
     - `changed textbox "Authentication code" value="<secret:totp-code>" purpose=one-time-code (was: textbox "Authentication code" purpose=one-time-code)`

## App log

What the app logged, oldest first, with the step it happened in.

- step 1 · ✗ console error: `Loading the script 'https://static.cloudflareinsights.com/beacon.min.js/vce701823e78c4301a35b7e07294d87301792088814092' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The actio…`
- step 1 · ✗ network error: `GET https://static.cloudflareinsights.com/beacon.min.js/vce701823e78c4301a35b7e07294d87301792088814092 csp`
- step 1 · ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/static/1.438.8/exception-autocapture.js' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action has been blocked. (at htt…`
- step 1 · ✗ network error: `GET https://us-assets.i.posthog.com/static/1.438.8/exception-autocapture.js csp`
- step 1 · ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config.js' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action …`
- step 1 · ✗ network error: `GET https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config.js csp`
- step 1 · ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/static/exception-autocapture.js?v=1.438.8' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action has been blocked. (at h…`
- step 1 · ✗ network error: `GET https://us-assets.i.posthog.com/static/exception-autocapture.js?v=1.438.8 csp`
- step 1 · ✗ console error: `Connecting to 'https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config' violates the following Content Security Policy directive: "connect-src 'self' https://veil.nyc https://id.veil.nyc https://accounts.veil.nyc https://login.veil.nyc https://us.i.posthog.com"…`
- step 1 · ✗ console error: `Fetch API cannot load https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config. Refused to connect because it violates the document's Content Security Policy. (at https://app.veil.nyc/assets/index-Bp02gZL1.js:67)`
- step 2 · ⚠ network warning: `GET https://accounts.veil.nyc/sessions/whoami 401`
- step 2 · ✗ console error: `Loading the script 'https://static.cloudflareinsights.com/beacon.min.js/vce701823e78c4301a35b7e07294d87301792088814092' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The actio…`
- step 2 · ✗ network error: `GET https://static.cloudflareinsights.com/beacon.min.js/vce701823e78c4301a35b7e07294d87301792088814092 csp`
- step 2 · ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/static/1.438.8/exception-autocapture.js' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action has been blocked. (at htt…`
- step 2 · ✗ network error: `GET https://us-assets.i.posthog.com/static/1.438.8/exception-autocapture.js csp`
- step 2 · ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config.js' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action …`
- step 2 · ✗ network error: `GET https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config.js csp`
- step 2 · ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/static/exception-autocapture.js?v=1.438.8' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action has been blocked. (at h…`
- step 2 · ✗ network error: `GET https://us-assets.i.posthog.com/static/exception-autocapture.js?v=1.438.8 csp`
- step 2 · ✗ console error: `Connecting to 'https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config' violates the following Content Security Policy directive: "connect-src 'self' https://veil.nyc https://id.veil.nyc https://accounts.veil.nyc https://login.veil.nyc https://us.i.posthog.com"…`
- step 2 · ✗ console error: `Fetch API cannot load https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config. Refused to connect because it violates the document's Content Security Policy. (at https://app.veil.nyc/assets/index-Bp02gZL1.js:67)`
- step 4 · ✗ console error: `Loading the script 'https://static.cloudflareinsights.com/beacon.min.js/vce701823e78c4301a35b7e07294d87301792088814092' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The actio…`
- step 4 · ✗ network error: `GET https://static.cloudflareinsights.com/beacon.min.js/vce701823e78c4301a35b7e07294d87301792088814092 csp`
- step 4 · ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/static/1.438.8/exception-autocapture.js' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action has been blocked. (at htt…`
- step 4 · ✗ network error: `GET https://us-assets.i.posthog.com/static/1.438.8/exception-autocapture.js csp`
- step 4 · ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config.js' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action …`
- step 4 · ✗ network error: `GET https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config.js csp`
- step 4 · ✗ console error: `Loading the script 'https://us-assets.i.posthog.com/static/exception-autocapture.js?v=1.438.8' violates the following Content Security Policy directive: "script-src 'self'". Note that 'script-src-elem' was not explicitly set, so 'script-src' is used as a fallback. The action has been blocked. (at h…`
- step 4 · ✗ network error: `GET https://us-assets.i.posthog.com/static/exception-autocapture.js?v=1.438.8 csp`
- step 4 · ✗ console error: `Connecting to 'https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config' violates the following Content Security Policy directive: "connect-src 'self' https://veil.nyc https://id.veil.nyc https://accounts.veil.nyc https://login.veil.nyc https://us.i.posthog.com"…`
- step 4 · ✗ console error: `Fetch API cannot load https://us-assets.i.posthog.com/array/phc_AjkhQ4ziUxEs9jM6JXJQjya2WkRKuLDWa4QR5XS9zYT8/config. Refused to connect because it violates the document's Content Security Policy. (at https://app.veil.nyc/assets/index-Bp02gZL1.js:67)`

## Screen at failure

`https://app.veil.nyc/login?flow=2d59636d-04e0-450b-8ce3-839516e0fdce` · viewport 1280x720 · 12 nodes  

![screenshot at failure](attempt-1/screenshots/001-failure.png)

The screen as the agent reads it, one node per line: `#id role "name" text="…" [states]`.

```text
#root document "Veil"
 #n63001 heading "Veil"
 #n63002 heading "Sign in"
 #n63003 text="Sign in with your email and password or a security key"
 #n63004 text="Please confirm this action by verifying that it is you. Please complete the second authentication challenge."
 #n63005 text="Authentication code"
 #n63006 textbox "Authentication code" value="<secret:totp-code>" purpose=one-time-code
 #n63007 button "Use Authenticator"
 #n63008 text="or"
 #n63009 text="Recovery code"
 #n63010 textbox "Recovery code" [focused]
 #n63011 button "Use backup recovery code"
```

## Evidence

- [screenshot](attempt-1/screenshots/001-failure.png) `.e2e/results/vault-web-item-detail-shows-masked-secret-973256f8fd7f6857/attempt-1/screenshots/001-failure.png`
- [screen](attempt-1/screen-at-failure.txt) `.e2e/results/vault-web-item-detail-shows-masked-secret-973256f8fd7f6857/attempt-1/screen-at-failure.txt`

<sub>e2e 0.21.3 · run `01a184d8-ea1d-7d4a-8f40-7382754ec4e8` · the whole run is in `report.json`</sub>
