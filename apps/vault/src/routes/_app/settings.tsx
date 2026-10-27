import { Banner } from "@cloudflare/kumo/components/banner";
import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Meter } from "@cloudflare/kumo/components/meter";
import { Text } from "@cloudflare/kumo/components/text";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import type { BillingView, RecoveryShare } from "@vortex-api/veil";
import {
  billing,
  billingCheckout,
  enrollRecovery,
  provisionVault,
  recoveryShare,
  setTravelMode,
  travelMode,
} from "../../origin";
import {
  b64,
  downloadEmergencyKit,
  downloadRecoveryKit,
  generateRecoveryKeypair,
} from "../../recovery";
import { claimsOf, token } from "../../auth";
import { frontend } from "../../ory";
import { PageChrome } from "../../page-chrome";

export const Route = createFileRoute("/_app/settings")({
  component: Settings,
});

// Inside Veil.app the SPA is served over the veil-app: scheme — the Mac
// Fill settings window replaces the .dmg download there.
const isMacApp = typeof window !== "undefined" && window.location.protocol === "veil-app:";

// RecoveryKitCard is the VEIL-78 enrollment surface: the browser mints the
// X25519 pair, downloads the private half as the kit file, then posts only
// the pubkey to enroll. The org master never crosses this boundary.
function RecoveryKitCard() {
  const [share, setShare] = useState<RecoveryShare | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [email, setEmail] = useState("");

  useEffect(() => {
    let live = true;
    void recoveryShare().then((res) => {
      if (!live) {
        return;
      }
      if (!res.error && res.data) {
        setShare(res.data);
      }
      setLoaded(true);
    });
    // The Emergency Kit names the member — Kratos traits carry the email;
    // the Hydra id_token email claim is the fallback.
    void frontend
      .toSession()
      .then((session) => {
        if (!live) {
          return;
        }
        const traits = (session.identity?.traits ?? {}) as { email?: unknown };
        const found =
          typeof traits.email === "string"
            ? traits.email
            : String(claimsOf(token() ?? "").email ?? "");
        setEmail(found);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, []);

  async function generate() {
    setBusy(true);
    setError(null);
    try {
      const { pub, priv } = generateRecoveryKeypair();
      // org_id is only for the kit filename — provision is idempotent.
      const prov = await provisionVault();
      const orgID = prov.data?.org_id ?? "veil";
      downloadRecoveryKit(orgID, pub, priv);
      // The printable Emergency Kit carries the same key — both downloads
      // must happen here, while the private half still exists in memory.
      downloadEmergencyKit(orgID, email, pub, priv);
      const res = await enrollRecovery(b64(pub));
      if (res.error) {
        // 409 — another device beat this click. Re-read tells the truth.
        if (res.response?.status !== 409) {
          setError("enrollment failed — keep the downloaded kit and retry");
        }
        const again = await recoveryShare();
        if (!again.error && again.data) {
          setShare(again.data);
        }
        return;
      }
      if (res.data) {
        setShare(res.data);
      }
    } finally {
      setBusy(false);
    }
  }

  if (!loaded) {
    return null;
  }
  return (
    <LayerCard>
      <LayerCard.Secondary>Recovery Kit</LayerCard.Secondary>
      <LayerCard.Primary>
        {share?.enrolled ? (
          <>
            <Text as="p" variant="heading">
              Recovery key enrolled
            </Text>
            <Text as="p" variant="secondary">
              Wrap epoch {share.wrap_pubkey_epoch} · org key v{share.org_key_version}
            </Text>
            {share.created_at ? (
              <Text as="p" variant="secondary" size="sm">
                Enrolled {share.created_at}
                {share.updated_at ? ` · updated ${share.updated_at}` : ""}
              </Text>
            ) : null}
            <Text as="p" variant="secondary" size="sm">
              Your private key lives only in the kit file you downloaded. If you lose every device
              and the kit, an org owner must confirm an account recovery.
            </Text>
          </>
        ) : (
          <>
            <Banner
              variant="alert"
              title="No recovery kit saved"
              description="If you lose every signed-in device, an enrolled recovery key is the only self-serve path back into this vault."
            />
            <Text as="p" variant="secondary">
              Generate an X25519 recovery keypair in this browser. The private half downloads once
              as your Recovery Kit plus a printable Emergency Kit (veil-emergency-kit-*.html — open
              it and Print to PDF or paper). Veil only ever receives the public half.
            </Text>
            {error ? (
              <Text as="p" variant="error">
                {error}
              </Text>
            ) : null}
            <div>
              <Button
                type="button"
                variant="primary"
                disabled={busy}
                onClick={() => void generate()}
              >
                {busy ? "Generating…" : "Generate & Download Recovery Kit"}
              </Button>
            </div>
          </>
        )}
      </LayerCard.Primary>
    </LayerCard>
  );
}

// TravelModeCard is the VTX-788 owner toggle. The GET is open to any org
// human so members see why items vanished; the POST is owner-only — a
// member click fails 403 and the card says so.
function TravelModeCard() {
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [readonly, setReadonly] = useState(false);

  useEffect(() => {
    let live = true;
    void travelMode().then((res) => {
      if (!live) {
        return;
      }
      if (!res.error && res.data) {
        setEnabled(res.data.enabled);
      }
    });
    return () => {
      live = false;
    };
  }, []);

  async function toggle(next: boolean) {
    setBusy(true);
    setError(null);
    try {
      const res = await setTravelMode(next);
      if (res.error || !res.data) {
        if (res.response?.status === 403) {
          setReadonly(true);
          setError("Only org owners can change Travel Mode.");
          return;
        }
        setError("update failed");
        return;
      }
      setEnabled(res.data.enabled);
    } finally {
      setBusy(false);
    }
  }

  if (enabled === null) {
    return null;
  }
  return (
    <LayerCard>
      <LayerCard.Secondary>Travel Mode</LayerCard.Secondary>
      <LayerCard.Primary>
        <Text as="p" variant="heading">
          {enabled ? "On — only travel-safe items are visible" : "Off"}
        </Text>
        <Text as="p" variant="secondary">
          While on, items not marked travel safe are hidden from every list, fill, and
          agent Use — on every device. Mark items travel safe from the Items page.
        </Text>
        {error ? (
          <Text as="p" variant="error">
            {error}
          </Text>
        ) : null}
        {!readonly ? (
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant={enabled ? "secondary" : "primary"}
              disabled={busy}
              onClick={() => void toggle(!enabled)}
            >
              {busy ? "Updating…" : enabled ? "Turn off" : "Turn on"}
            </Button>
          </div>
        ) : null}
      </LayerCard.Primary>
    </LayerCard>
  );
}

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
        {paid ? <Banner title="Subscription active — thank you for backing Veil Beta!" /> : null}
        {view ? (
          <LayerCard>
            <LayerCard.Secondary>Plan &amp; Billing</LayerCard.Secondary>
            <LayerCard.Primary>
              <Text as="p" variant="heading">
                {view.plan === "active" ? "Veil Pro Beta ($1/mo)" : "Free tier"}
              </Text>
              <Text as="p" variant="secondary">
                {view.used}
                {view.included != null ? ` of ${view.included}` : ""} credential uses this month
              </Text>
              {view.included != null ? (
                <Meter
                  label="Free-tier usage"
                  value={Math.min(100, (view.used / Math.max(view.included, 1)) * 100)}
                  customValue={`${view.used} / ${view.included}`}
                />
              ) : null}
              {view.plan !== "active" ? (
                <>
                  <Text as="p" variant="secondary">
                    Limited-time beta offer: $1/month through the end of the year (standard $29/mo)
                    for unlimited agent &amp; API credential uses.
                  </Text>
                  <div>
                    <Button
                      type="button"
                      variant="primary"
                      onClick={() => {
                        void billingCheckout().then((res) => {
                          const url = res.data?.checkout_url || view.upgrade_url;
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
        <TravelModeCard />
        <RecoveryKitCard />
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
