# Store listing — Veil extension (Chrome Web Store / AMO)

Publisher identity is **Vortex** everywhere — store developer name, support
contact, homepage. Veil is the product; Vortex is the company that ships it.
Accounts: Google `shlomo@vortex.nyc`, Apple `shlomo@plasmapos.com`
(team VFWGNKKT4G, Plasma POS, Inc.). Nothing personal, nothing invented.

Private alpha distribution: CWS "unlisted" (link-only) + AMO self-distributed
signed .xpi. Safari ships inside Veil.app (Developer ID + notarize).

## Chrome Web Store

**Account state (2026-09-30):** publisher `shlomo@vortex.nyc` (ID
`db4d4328-8fd0-44cc-bcc5-f824185c2b5d`), $5 fee paid, contact email
**verified**. Item created: Veil 0.0.1, draft `cfkdimikgngipjiiklmnoagnanpkapcn`.
Trader declaration (trader), display name **Vortex**, and address (895
Broadway, New York, NY 10003) are entered but **cannot save until publisher
verification completes** — Google Payments KYC ("Start verification" on
Settings). The payments iframe rejects synthetic input; the profile picker
offers **Organization XIII Inc., 895 Broadway 4th Floor, NY 10003-1226**
(the Google Workspace/Ads org profile = Vortex's legal entity). Select it,
then org name/address/phone + registration proof. Until then the item editor
redirects to Settings. Once verified: re-enter name/trader/address, save, then
upload icon + screenshots below.

- **Name:** Veil
- **Summary (132):** Fill saved sign-ins. Touch ID-gated fills, passkeys, TOTP — secrets never leave your Mac.
- **Category:** Productivity
- **Language:** English
- **Privacy policy:** https://veil.nyc/privacy
- **Visibility:** Unlisted
- **Screenshots:** shot-1-fill-popup.png, shot-2-touchid.png (1280×800)

**Description:**

Veil fills your saved sign-ins into web pages — but only after you approve
each one. Every fill, save, password change, and authenticator enrollment is
gated by Touch ID on your Mac. Secrets never leave the host; the extension
sees a credential only at the moment you authorize it.

- Pick a saved sign-in inline or from the toolbar — Touch ID, then it fills.
- Save new sign-ins as you type; change-password updates the same item.
- Enroll authenticator (TOTP) seeds from QR links — you pick the account,
  Touch ID confirms.
- Passkey sign-in and registration on the same confirmation surface.
- Agents and extensions never receive vault contents — grants, not dumps.

Requires the Veil host app (veil fill install) — free at https://veil.nyc.

**Permission justifications:**

- `nativeMessaging`: Talks to the Veil host on this Mac — the vault,
  authorization, and Touch ID confirmation live there. The extension is a
  thin client.
- `host_permissions` (all sites): Detects sign-in fields on pages you visit
  to offer fills and saves — the core job of a credential extension.
- `tabs`: Routes a fill/save to the tab it was typed in.
- `scripting`: Writes the approved credential into the page form.
- `storage`: Remembers the in-progress fill tab across service-worker
  restarts.

**Single purpose:** Fills and saves credentials after explicit user approval.
Nothing else.

**Remote code:** None. All code ships in the package.

**Data handling:** Credentials are processed locally by the companion app;
authentication information is used only for the core function, never sold
or transferred to third parties.

## AMO (Firefox)

Submitted 2026-09-30 as **Vortex** (`shlomo@vortex.nyc`, 2FA on — TOTP seed
+ recovery codes in Veil items `mozilla-amo` / `mozilla-amo-recovery`).
Channel: **unlisted** (self-distributed signed .xpi). Add-on `081badce029046738f07`,
version 0.0.1 auto-approved; signed artifact at
`~/.veil/store/veil-firefox-signed-0.0.1.xpi`. API credentials for
`web-ext sign` live in Veil item `mozilla-amo-api`.
Manifest declares `data_collection_permissions` (authenticationInfo,
websiteActivity, websiteContent) — AMO validator requires it.
Known validator warnings for next version: innerHTML assignment, 6 others.

Same copy. Category: Security & Privacy + Productivity. Notes for reviewer:

> Veil pairs with a native companion app (nyc.veil.fill) via native messaging;
> install from https://veil.nyc. Without the host the extension is inert —
> all authorization and Touch ID confirmation happen in the host process.
> Source of every listed behavior is auditable in the shipped JS (no
> minification, no remote code).

## Safari

No separate listing — the web extension ships inside Veil.app
(`apps/fill-safari`). Alpha distribution: Developer ID-signed + notarized
.zip/.dmg, or TestFlight later.
