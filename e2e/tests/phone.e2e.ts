import { test } from "e2e";

// iOS target — drives the installed nyc.veil.phone build via agent-device.

test("phone sign-in → items → item detail", async ({ app, agent }) => {
  await agent.act("tap Sign in on the Veil welcome screen");
  await agent.act("enter email 'shlomo@vortex.nyc' and password from secret vault-password, then sign in");
  await agent.act("enter the authenticator code from secret totp-code if asked");
  await agent.assert("the items list shows a search field and account rows");
});
