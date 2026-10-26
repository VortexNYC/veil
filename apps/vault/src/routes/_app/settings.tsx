import { Banner } from "@cloudflare/kumo/components/banner";
import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Meter } from "@cloudflare/kumo/components/meter";
import { Text } from "@cloudflare/kumo/components/text";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import type { BillingView } from "@vortex-api/veil";
import { billing, billingCheckout } from "../../origin";
import { PageChrome } from "../../page-chrome";

export const Route = createFileRoute("/_app/settings")({
  component: Settings,
});

// Inside Veil.app the SPA is served over the veil-app: scheme — the Mac
// Fill settings window replaces the .dmg download there.
const isMacApp =
  typeof window !== "undefined" && window.location.protocol === "veil-app:";

function Settings() {
  const [view, setView] = useState<BillingView | null>(null);
  useEffect(() => {
    let live = true;
    void billing().then((res) => {
      if (live) {
        setView(res.error || !res.data ? null : res.data);
      }
    });
    return () => {
      live = false;
    };
  }, []);

  // Vortex checkout returns here — VEIL_CHECKOUT_SUCCESS_URL lands on
  // /settings?paid=1 after a paid session completes.
  const paid =
    typeof window !== "undefined" &&
    new URLSearchParams(window.location.search).get("paid") === "1";

  return (
    <PageChrome
      title="Settings"
      subtitle="Identity is Ory Elements. This app does not restyle those screens."
    >
      <div className="flex flex-col items-start gap-3">
        {paid ? (
          <Banner title="Subscription active — thank you for backing Veil Beta!" />
        ) : null}
        {view ? (
          <LayerCard>
            <LayerCard.Secondary>Plan &amp; Billing</LayerCard.Secondary>
            <LayerCard.Primary>
              <Text as="p" variant="heading">
                {view.plan === "active" ? "Veil Pro Beta ($1/mo)" : "Free tier"}
              </Text>
              <Text as="p" variant="secondary">
                {view.used}
                {view.included != null ? ` of ${view.included}` : ""} credential
                uses this month
              </Text>
              {view.included != null ? (
                <Meter
                  label="Free-tier usage"
                  value={Math.min(
                    100,
                    (view.used / Math.max(view.included, 1)) * 100,
                  )}
                  customValue={`${view.used} / ${view.included}`}
                />
              ) : null}
              {view.plan !== "active" ? (
                <>
                  <Text as="p" variant="secondary">
                    Limited-time beta offer: $1/month through the end of the year
                    (standard $29/mo) for unlimited agent &amp; API credential
                    uses.
                  </Text>
                  <div>
                    <Button
                      type="button"
                      variant="primary"
                      onClick={() => {
                        void billingCheckout().then((res) => {
                          const url =
                            res.data?.checkout_url || view.upgrade_url;
                          if (url) {
                            window.location.assign(url);
                          }
                        });
                      }}
                    >
                      Subscribe — $1/mo Beta
                    </Button>
                  </div>
                </>
              ) : null}
            </LayerCard.Primary>
          </LayerCard>
        ) : null}
        <Text as="p">
          Password, TOTP, WebAuthn, and lookup codes live at login.veil.nyc.
        </Text>
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
