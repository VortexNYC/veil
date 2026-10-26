import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Text } from "@cloudflare/kumo/components/text";
import { createFileRoute } from "@tanstack/react-router";
import { PageChrome } from "../../page-chrome";

export const Route = createFileRoute("/_app/settings")({
  component: Settings,
});

// Inside Veil.app the SPA is served over the veil-app: scheme — the Mac
// Fill settings window replaces the .dmg download there.
const isMacApp =
  typeof window !== "undefined" && window.location.protocol === "veil-app:";

function Settings() {
  return (
    <PageChrome
      title="Settings"
      subtitle="Identity is Ory Elements. This app does not restyle those screens."
    >
      <div className="flex flex-col items-start gap-3">
        <Text as="p">Password, TOTP, WebAuthn, and lookup codes live at login.veil.nyc.</Text>
        <div className="flex flex-wrap items-center gap-2">
          <LinkButton href="https://login.veil.nyc/settings" variant="outline">
            Open identity settings
          </LinkButton>
          {isMacApp ? (
            <Button
              type="button"
              variant="outline"
              onClick={() => window.location.assign("veil-app://vault/mac-settings")}
            >
              Mac Fill & Shortcuts Settings…
            </Button>
          ) : (
            <LinkButton
              href="https://github.com/VortexNYC/veil/releases/latest/download/Veil.dmg"
              variant="outline"
            >
              Download Veil for macOS (.dmg)
            </LinkButton>
          )}
        </div>
      </div>
    </PageChrome>
  );
}
