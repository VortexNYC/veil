import { LinkButton } from "@cloudflare/kumo/components/button";
import { Text } from "@cloudflare/kumo/components/text";
import { createFileRoute } from "@tanstack/react-router";
import { PageChrome } from "../../page-chrome";

export const Route = createFileRoute("/_app/settings")({
  component: Settings,
});

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
          <LinkButton
            href="https://github.com/VortexNYC/veil/releases/latest/download/Veil.dmg"
            variant="outline"
          >
            Download Veil for macOS (.dmg)
          </LinkButton>
        </div>
      </div>
    </PageChrome>
  );
}
