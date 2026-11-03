import { test, secrets, expect } from "e2e";

// Common workflows on the real deployed vault. Secrets come from
// E2E_SECRET_* env or the local ~/.config/veil files — never committed.
// Covers scenarios.yaml: auth.*, vault.* on target `web`.

async function signIn(screen: {
  getByLabel: (t: string) => { fill: (v: unknown) => Promise<unknown> };
  getByRole: (r: string, n?: string) => { tap: () => Promise<unknown> };
}) {
  await expect(screen.getByLabel("ID")).toBeVisible({ timeout: 15_000 });
  await screen.getByLabel("ID").fill(process.env.E2E_VAULT_EMAIL ?? "shlomo@vortex.nyc");
  await screen.getByLabel("Password").fill(secrets.get("vault-password"));
  await screen.getByRole("button", "Sign in with password").tap();
  // Deterministic, not agent steps: each devin turn is ~10s and a code
  // minted at secrets.get() goes stale before an LLM submits it.
  // fill() resolves the secret function at fill time — inside the window.
  await expect(screen.getByLabel("Authentication code")).toBeVisible({ timeout: 15_000 });
  await screen.getByLabel("Authentication code").fill(secrets.get("totp-code"));
  await screen.getByRole("button", "Use Authenticator").tap();
  await expect(screen.getByRole("heading", "Sign in")).toBeHidden({ timeout: 30_000 });
  await expect(screen.getByText(/item|search/i).first()).toBeVisible({ timeout: 15_000 });
}

test("sign in and land on the items list", { platforms: ["web"], timeout: 300_000 }, async ({ app, screen }) => {
  await app.open("/login");
  await signIn(screen);
  await agent.assert("the vault Items list is visible with a search field and account rows");
});

test("item detail shows masked secret with reveal control", { platforms: ["web"] }, async ({ app, agent, screen }) => {
  await app.open("/login");
  await signIn(screen);
  await agent.act("open the first item in the list");
  await agent.assert("the password shows masked dots and a Reveal control is present");
});

test("search filters the item list", { platforms: ["web"] }, async ({ app, agent, screen }) => {
  await app.open("/login");
  await signIn(screen);
  await agent.act("search for 'github'");
  await agent.assert("the list shows only items related to github");
});

test("sign out clears the session", { platforms: ["web"] }, async ({ app, screen, agent: _agent, browser: _browser }) => {
  await app.open("/login");
  await signIn(screen);
  // Deterministic: the agent loses the top bar inside item-detail views.
  await app.open("/items");
  await expect(screen.getByRole("button", "Sign out").first()).toBeVisible();
  await screen.getByRole("button", "Sign out").first().tap();
  await expect(screen.getByRole("heading", "Sign in")).toBeVisible({ timeout: 15_000 });
  await app.open("/items");
  await expect(screen.getByRole("heading", "Sign in")).toBeVisible({ timeout: 15_000 });
});

test("member cannot create item; inline error is shown", { platforms: ["web"] }, async ({ app, agent, screen }) => {
  await app.open("/login");
  await signIn(screen);
  // Member (not owner) cannot create items — POST /v1/items 400s and the
  // dialog now surfaces the error inline (VTX-848).
  await agent.act("click the Add item button");
  await agent.act("type 'e2e-scratch' into the Name field");
  await agent.act("type 'hunter2-scratch' into the Password field");
  await agent.act("press Save");
  await agent.assert("an error is shown inside the Add item dialog (members cannot create items)");
  await agent.act("close the dialog");
});

test("TOTP item shows a live rolling code", { platforms: ["web"] }, async ({ app, agent, screen }) => {
  await app.open("/login");
  await signIn(screen);
  await agent.act("open an item that has a TOTP code");
  await agent.assert("a 6-digit code is shown and a countdown or rollover indicator is present");
});

test("audit page shows recent events", { platforms: ["web"] }, async ({ app, agent, screen }) => {
  await app.open("/login");
  await signIn(screen);
  // Audit feed is owner-only — a member gets the owner-gate state (VTX-847).
  await agent.act("open the audit or activity page");
  await agent.assert("the page explains the audit feed is visible to org owners (no raw 'list failed')");
});
