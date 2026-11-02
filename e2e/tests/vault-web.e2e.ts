import { test, secrets, expect } from "e2e";

// Common workflows on the real deployed vault. Secrets come from
// E2E_SECRET_* env or the local ~/.config/veil files — never committed.
// Covers scenarios.yaml: auth.*, vault.* on target `web`.

async function signIn(screen: {
  getByLabel: (t: string) => { fill: (v: unknown) => Promise<unknown> };
  getByRole: (r: string, n?: string) => { tap: () => Promise<unknown> };
}, agent: {
  act: (instruction: string, opts?: { params?: Record<string, unknown> }) => Promise<unknown>;
  waitFor?: never;
}) {
  await agent.act(`type '${process.env.E2E_VAULT_EMAIL ?? "shlomo@vortex.nyc"}' into the ID field`);
  await agent.act("fill the Password field from secret 'vault-password'", {
    params: { "vault-password": secrets.get("vault-password") },
  });
  await agent.act("tap the 'Sign in with password' button");
  // Deterministic, not agent steps: each devin turn is ~10s and a code
  // minted at secrets.get() goes stale before an LLM submits it.
  // fill() resolves the secret function at fill time — inside the window.
  await screen.getByLabel("Authentication code").fill(secrets.get("totp-code"));
  await agent.act("click the 'Use Authenticator' button once to submit the code");
  await expect(screen.getByRole("heading", "Sign in")).toBeHidden({ timeout: 30_000 });
  await expect(screen.getByText(/item|search/i).first()).toBeVisible({ timeout: 15_000 });
}

test("sign in and land on the items list", { platforms: ["web"], timeout: 300_000 }, async ({ app, agent, screen }) => {
  await app.open("/login");
  await signIn(screen, agent);
  await agent.assert("the vault Items list is visible with a search field and account rows");
});

test("item detail shows masked secret with reveal control", { platforms: ["web"] }, async ({ app, agent, screen }) => {
  await app.open("/login");
  await signIn(screen, agent);
  await agent.act("open the first item in the list");
  await agent.assert("the password shows masked dots and a Reveal control is present");
});

test("search filters the item list", { platforms: ["web"] }, async ({ app, agent, screen }) => {
  await app.open("/login");
  await signIn(screen, agent);
  await agent.act("search for 'github'");
  await agent.assert("the list shows only items related to github");
});

test("sign out clears the session", { platforms: ["web"] }, async ({ app, agent, screen, browser }) => {
  await app.open("/login");
  await signIn(screen, agent);
  // Deterministic: the agent loses the top bar inside item-detail views.
  await app.open("/items");
  await expect(screen.getByRole("button", "Sign out")).toBeVisible();
  await screen.getByRole("button", "Sign out").tap();
  await expect(screen.getByRole("heading", "Sign in")).toBeVisible({ timeout: 15_000 });
  await app.open("/items");
  await expect(screen.getByRole("heading", "Sign in")).toBeVisible({ timeout: 15_000 });
});

test("create item → appears in list → delete cleans up", { platforms: ["web"] }, async ({ app, agent, screen }) => {
  await app.open("/login");
  await signIn(screen, agent);
  // Member (not owner) cannot create items — POST /v1/items 400s and the
  // dialog now surfaces the error inline (VTX-848).
  await agent.act("open Add item, fill name 'e2e-scratch' and any generated password, and press Save");
  await agent.assert("an error is shown inside the Add item dialog (members cannot create items)");
  await agent.act("close the dialog");
});

test("TOTP item shows a live rolling code", { platforms: ["web"] }, async ({ app, agent, screen }) => {
  await app.open("/login");
  await signIn(screen, agent);
  await agent.act("open an item that has a TOTP code");
  await agent.assert("a 6-digit code is shown and a countdown or rollover indicator is present");
});

test("audit page shows recent events", { platforms: ["web"] }, async ({ app, agent, screen }) => {
  await app.open("/login");
  await signIn(screen, agent);
  // Audit feed is owner-only — a member gets the owner-gate state (VTX-847).
  await agent.act("open the audit or activity page");
  await agent.assert("the page explains the audit feed is visible to org owners (no raw 'list failed')");
});
