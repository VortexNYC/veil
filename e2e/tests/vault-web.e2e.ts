import { test, secrets } from "e2e";

// Common workflows on the real deployed vault. Secrets come from
// E2E_SECRET_* env or the local ~/.config/veil files — never committed.
// Covers scenarios.yaml: auth.*, vault.* on target `web`.

async function signIn(screen: {
  getByLabel: (t: string) => { fill: (v: unknown) => Promise<unknown> };
  getByRole: (r: string, n?: string) => { tap: () => Promise<unknown> };
}, agent: {
  act: (instruction: string, opts?: { params?: Record<string, unknown> }) => Promise<unknown>;
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
  await screen.getByRole("button", "Use Authenticator").tap();
  await agent.waitFor("the vault Items list is visible", { timeout: 15000 })
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

test("sign out clears the session", { platforms: ["web"] }, async ({ app, agent, screen }) => {
  await app.open("/login");
  await signIn(screen, agent);
  await agent.act("sign out of the vault");
  await agent.assert("the app is back on the sign-in screen");
  await app.open("/");
  await agent.assert("the app does not show the items list — it asks for sign-in");
});

test("create item → appears in list → delete cleans up", { platforms: ["web"] }, async ({ app, agent, screen }) => {
  await app.open("/login");
  await signIn(screen, agent);
  await agent.act("create a new item named 'e2e-scratch' with any generated password");
  await agent.assert("the item 'e2e-scratch' appears in the items list");
  await agent.act("open 'e2e-scratch' and delete or archive it");
  await agent.assert("the item 'e2e-scratch' no longer appears in the items list");
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
  await agent.act("open the audit or activity page");
  await agent.assert("a list of recent grant/fill/use events is visible with timestamps");
});
