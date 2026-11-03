import { test, secrets, expect } from "e2e";

// Production vault workflows @ app.veil.nyc. Secrets from E2E_SECRET_* or
// ~/.config/veil — never committed. Default human is the org *member* used for
// launch regression (see E2E_VAULT_EMAIL); owner-only surfaces assert the gate.
//
// Transport: this file runs on @e2e-dev/web (Playwright). Manual browser proof
// uses agent-browser (`vb`); iOS/Android legs use agent-device (see phone.e2e.ts);
// macOS menu-bar / tray proof uses agent-desktop when installed.

async function signIn(screen: {
  getByLabel: (t: string) => { fill: (v: unknown) => Promise<unknown> };
  getByRole: (r: string, n?: string) => { tap: () => Promise<unknown> };
}) {
  await expect(screen.getByLabel("ID")).toBeVisible({ timeout: 15_000 });
  await screen.getByLabel("ID").fill(process.env.E2E_VAULT_EMAIL ?? "shlomo@vortex.nyc");
  await screen.getByLabel("Password").fill(secrets.get("vault-password"));
  await screen.getByRole("button", "Sign in with password").tap();
  await expect(screen.getByLabel("Authentication code")).toBeVisible({ timeout: 15_000 });
  await screen.getByLabel("Authentication code").fill(secrets.get("totp-code"));
  await screen.getByRole("button", "Use Authenticator").tap();
  await expect(screen.getByRole("heading", "Sign in")).toBeHidden({ timeout: 30_000 });
  await expect(screen.getByLabel("Search items")).toBeVisible({ timeout: 15_000 });
}

async function waitForAccounts(screen: {
  getByRole: (r: string, n?: string) => {
    first: () => { tap: () => Promise<unknown> };
  };
}) {
  await expect(screen.getByRole("listbox", "Accounts")).toBeVisible({ timeout: 15_000 });
  await expect(screen.getByRole("option").first()).toBeVisible({ timeout: 15_000 });
}

test("sign in and land on the items list", { platforms: ["web"], timeout: 300_000 }, async ({ app, screen }) => {
  await app.open("/login");
  await signIn(screen);
});

test("item detail shows masked secret with reveal control", { platforms: ["web"] }, async ({ app, screen }) => {
  await app.open("/login");
  await signIn(screen);
  await waitForAccounts(screen);
  await screen.getByRole("option").first().tap();
  await expect(screen.getByRole("button", "Reveal")).toBeVisible({ timeout: 10_000 });
  await expect(screen.getByText(/••••/)).toBeVisible();
});

test("search filters the item list", { platforms: ["web"] }, async ({ app, screen }) => {
  await app.open("/login");
  await signIn(screen);
  await waitForAccounts(screen);
  await screen.getByLabel("Search items").fill("github");
  await expect(screen.getByText("No matches")).toBeHidden({ timeout: 10_000 });
  await expect(screen.getByRole("option").first()).toBeVisible();
});

test("sign out clears the session", { platforms: ["web"] }, async ({ app, screen }) => {
  await app.open("/login");
  await signIn(screen);
  await app.open("/items");
  await expect(screen.getByRole("button", "Sign out").first()).toBeVisible();
  await screen.getByRole("button", "Sign out").first().tap();
  await expect(screen.getByRole("heading", "Sign in")).toBeVisible({ timeout: 15_000 });
  await app.open("/items");
  await expect(screen.getByRole("heading", "Sign in")).toBeVisible({ timeout: 15_000 });
});

test("add item dialog reports outcome (member error or owner create)", { platforms: ["web"] }, async ({ app, screen }) => {
  await app.open("/login");
  await signIn(screen);
  const name = `e2e-scratch-${Date.now()}`;
  await screen.getByRole("button", "Add").tap();
  await expect(screen.getByRole("heading", "Add item")).toBeVisible({ timeout: 10_000 });
  await screen.getByLabel("Name").fill(name);
  await screen.getByRole("button", "Generate").tap();
  await screen.getByRole("button", "Save").tap();
  const dialogClosed = screen.getByRole("heading", "Add item");
  let ownerCreated = false;
  try {
    await expect(dialogClosed).toBeHidden({ timeout: 15_000 });
    ownerCreated = true;
  } catch {
    await expect(screen.getByText("create failed")).toBeVisible({ timeout: 5_000 });
  }
  if (ownerCreated) {
    await screen.getByLabel("Search items").fill(name);
    await expect(screen.getByText(name).first()).toBeVisible({ timeout: 10_000 });
    await screen.getByRole("option").first().tap();
    await screen.getByRole("button", "Delete").tap();
    await screen.getByRole("button", "Confirm Delete").tap();
    await screen.getByLabel("Search items").fill(name);
    await expect(screen.getByText("No matches")).toBeVisible({ timeout: 15_000 });
  }
});

test("TOTP item shows a live rolling code", { platforms: ["web"] }, async ({ app, screen }) => {
  await app.open("/login");
  await signIn(screen);
  await waitForAccounts(screen);
  await screen.getByRole("button", "2FA").tap();
  await expect(screen.getByRole("option").first()).toBeVisible({ timeout: 10_000 });
  await screen.getByRole("option").first().tap();
  await expect(screen.getByText("One-Time Password (2FA)")).toBeVisible({ timeout: 15_000 });
  await expect(screen.getByText(/\d{3}\s?\d{3}/)).toBeVisible({ timeout: 15_000 });
  await expect(screen.getByText(/\d+s/)).toBeVisible();
});

test("audit page loads for signed-in human", { platforms: ["web"] }, async ({ app, screen }) => {
  await app.open("/login");
  await signIn(screen);
  await app.open("/audit");
  await expect(screen.getByRole("heading", "Audit")).toBeVisible({ timeout: 15_000 });
  await expect(screen.getByRole("button", "Refresh")).toBeVisible();
  // Owner: filter + table. Member: owner-gate copy — never a raw "list failed".
  await expect(screen.getByText(/list failed/i)).toBeHidden();
  await expect(
    screen.getByLabel("Filter audit events").or(screen.getByText(/org owners/i)),
  ).toBeVisible({ timeout: 10_000 });
});
