import { test, secrets, expect } from "e2e";

// iOS — @e2e-dev/mobile + agent-device. Sign-in opens ASWebAuthenticationSession
// (Ory at login.veil.nyc); deterministic steps on the sheet, same as vault-web.

async function signInFromPhone(screen: {
  getByRole: (r: string, n?: string) => { tap: () => Promise<unknown> };
  getByLabel: (t: string) => { fill: (v: unknown) => Promise<unknown> };
}) {
  await screen.getByRole("button", "Sign in").tap();
  await expect(screen.getByLabel("ID")).toBeVisible({ timeout: 45_000 });
  await screen.getByLabel("ID").fill(process.env.E2E_VAULT_EMAIL ?? "shlomo@vortex.nyc");
  await screen.getByLabel("Password").fill(secrets.get("vault-password"));
  await screen.getByRole("button", "Sign in with password").tap();
  await expect(screen.getByLabel("Authentication code")).toBeVisible({ timeout: 20_000 });
  await screen.getByLabel("Authentication code").fill(secrets.get("totp-code"));
  await screen.getByRole("button", "Use Authenticator").tap();
  await expect(screen.getByLabel("Search accounts")).toBeVisible({ timeout: 60_000 });
}

test(
  "phone sign-in → items → item detail",
  { platforms: ["ios"], timeout: 600_000 },
  async ({ app, screen }) => {
    await app.open("/sign-in");
    await signInFromPhone(screen);
    await expect(screen.getByRole("button", "All")).toBeVisible({ timeout: 15_000 });
    const itemName = process.env.E2E_PHONE_ITEM_NAME ?? "github";
    await screen.getByRole("button", itemName).tap();
    await expect(screen.getByText(/PASSWORD|SECRET|USERNAME|2FA|TOTP/i).first()).toBeVisible({
      timeout: 15_000,
    });
  },
);
