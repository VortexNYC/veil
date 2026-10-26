import { Text } from "@cloudflare/kumo/components/text";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { beginLogin, finishLogin } from "../auth";
import { provisionVault } from "../origin";

// One forced re-auth is the aal2 retry budget — a brand-new account has no
// second factor enrolled yet, so retrying authorize would loop forever.
const aal2RetriedKey = "veil.aal2_retried";

export const Route = createFileRoute("/oidc/callback")({
  loader: async () => {
    try {
      await finishLogin(window.location.href);
      sessionStorage.removeItem(aal2RetriedKey);
      // First sign-in after registration creates the org, sealed master key,
      // Keto tuples, and the Vortex billing customer. Idempotent — and a
      // failure here must not strand the login.
      await provisionVault().catch(() => null);
    } catch (e) {
      // aal1 session was reused — redo authorize forcing fresh factors
      if (e instanceof Error && e.message === "aal2") {
        if (sessionStorage.getItem(aal2RetriedKey) === "1") {
          // Still no second factor after a forced re-auth — send the new
          // user to identity settings to enroll TOTP or a passkey.
          sessionStorage.removeItem(aal2RetriedKey);
          window.location.assign("https://login.veil.nyc/settings");
          return new Promise<never>(() => {});
        }
        sessionStorage.setItem(aal2RetriedKey, "1");
        window.location.assign(await beginLogin(true));
        return new Promise<never>(() => {});
      }
      throw e;
    }
    throw redirect({ to: "/items" });
  },
  component: Callback,
  errorComponent: ({ error }) => (
    <main className="p-6">
      <Text as="p">Sign-in failed.</Text>
      <Text as="p" variant="secondary" size="xs">
        {error instanceof Error ? error.message : "error"}
      </Text>
    </main>
  ),
});

function Callback() {
  return (
    <main className="p-6">
      <Text as="p">Signing in…</Text>
    </main>
  );
}
