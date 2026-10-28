import { test, expect, secrets } from "e2e";

// Common workflows on the real deployed vault. Secrets come from
// E2E_SECRET_VAULT_PASSWORD / E2E_TOTP_SEED env — never committed.

test("sign in and land on the items list", async ({ app, agent }) => {
  await app.open("/login");
  await agent.act("sign in with email 'shlomo@vortex.nyc' and password from secret vault-password");
  await agent.act("enter the 6-digit code from secret totp-code and continue");
  await agent.assert("the vault Items list is visible with a search field and account rows");
});

test("item detail shows masked secret with reveal control", async ({ app, agent }) => {
  await app.open("/");
  await agent.act("open the first item in the list");
  await agent.assert("the password shows masked dots and a Reveal control is present");
});

test("search filters the item list", async ({ app, agent }) => {
  await app.open("/");
  await agent.act("search for 'github'");
  await agent.assert("the list shows only items related to github");
});
