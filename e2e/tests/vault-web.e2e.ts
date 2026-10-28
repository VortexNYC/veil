import { test, secrets } from "e2e";

// Common workflows on the real deployed vault. Secrets come from
// E2E_SECRET_* env or the local ~/.config/veil files — never committed.

async function signIn(agent: {
  act: (instruction: string, opts?: { params?: Record<string, unknown> }) => Promise<unknown>;
}) {
  await agent.act("type 'shlomo@vortex.nyc' into the ID field");
  await agent.act("fill the Password field from secret 'vault-password'", {
    params: { "vault-password": secrets.get("vault-password") },
  });
  await agent.act("tap the 'Sign in with password' button");
  await agent.act("enter the 6-digit authenticator code from secret 'totp-code' into the Authentication code field and tap Continue", {
    params: { "totp-code": secrets.get("totp-code") },
  });
}

test("sign in and land on the items list", async ({ app, agent }) => {
  await app.open("/login");
  await signIn(agent);
  await agent.assert("the vault Items list is visible with a search field and account rows");
});

test("item detail shows masked secret with reveal control", async ({ app, agent }) => {
  await app.open("/login");
  await signIn(agent);
  await agent.act("open the first item in the list");
  await agent.assert("the password shows masked dots and a Reveal control is present");
});

test("search filters the item list", async ({ app, agent }) => {
  await app.open("/login");
  await signIn(agent);
  await agent.act("search for 'github'");
  await agent.assert("the list shows only items related to github");
});
