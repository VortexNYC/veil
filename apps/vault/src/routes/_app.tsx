import { Banner } from "@cloudflare/kumo/components/banner";
import { Sidebar } from "@cloudflare/kumo/components/sidebar";
import { Text } from "@cloudflare/kumo/components/text";
import {
  Gear,
  Key,
  ListBullets,
  Robot,
  EnvelopeSimple,
  SignOut,
  Stamp,
  Users,
  type Icon,
} from "@phosphor-icons/react";
import { createFileRoute, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { beginLogin, signedIn, signOut, token, amrOf } from "../auth";
import { billing } from "../origin";
import type { BillingView } from "@vortex-api/veil";

export const Route = createFileRoute("/_app")({
  beforeLoad: async () => {
    if (signedIn()) {
      return;
    }
    window.location.assign(await beginLogin());
  },
  component: Shell,
});

const nav: ReadonlyArray<{
  to: "/items" | "/grants" | "/requests" | "/agents" | "/invites" | "/audit" | "/settings";
  label: string;
  icon: Icon;
}> = [
  { to: "/items", label: "Items", icon: Key },
  { to: "/grants", label: "Grants", icon: Users },
  { to: "/requests", label: "Requests", icon: Stamp },
  { to: "/agents", label: "Agents", icon: Robot },
  { to: "/invites", label: "Invites", icon: EnvelopeSimple },
  { to: "/audit", label: "Audit", icon: ListBullets },
  { to: "/settings", label: "Settings", icon: Gear },
];

// BillingBanner is the cap surface: at/over the free allowance the org's
// agents and API calls are already being denied — this is where the owner
// finds out why. Members (403) and paid plans render nothing.
function BillingBanner() {
  const [view, setView] = useState<BillingView | null>(null);
  useEffect(() => {
    let live = true;
    const load = async () => {
      const res = await billing();
      if (live) {
        setView(res.error || !res.data ? null : res.data);
      }
    };
    void load();
    const t = setInterval(load, 60_000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, []);
  if (!view || view.included == null) {
    return null;
  }
  const capped = view.used >= view.included;
  const near = !capped && view.used >= view.included * 0.8;
  if (!capped && !near) {
    return null;
  }
  const action = view.upgrade_url ? (
    <Banner.Action onClick={() => window.location.assign(view.upgrade_url!)}>
      Subscribe
    </Banner.Action>
  ) : undefined;
  return (
    <Banner
      variant={capped ? "error" : "alert"}
      title={capped ? "Free usage exhausted" : "Approaching free limit"}
      description={
        capped
          ? `${view.used} of ${view.included} uses this month — agent and API access is denied until the window resets or you subscribe.`
          : `${view.used} of ${view.included} uses this month.`
      }
      action={action}
    />
  );
}

function Shell() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const navigate = useNavigate();
  const raw = token();
  const aal2 = raw ? amrOf(raw).includes("totp") : false;
  return (
    <Sidebar.Provider collapsible="icon">
      <div className="bg-kumo-canvas relative z-10 flex h-dvh w-full overflow-hidden">
        <Sidebar>
          <Sidebar.Header>
            <Text as="span" variant="heading">
              Veil
            </Text>
          </Sidebar.Header>
          <Sidebar.Content>
            <Sidebar.Group>
              <Sidebar.Menu>
                {nav.map((item) => (
                  <Sidebar.MenuItem key={item.to}>
                    <Sidebar.MenuButton
                      tooltip={item.label}
                      active={pathname === item.to}
                      icon={item.icon}
                      onClick={() => {
                        void navigate({ to: item.to });
                      }}
                    >
                      {item.label}
                    </Sidebar.MenuButton>
                  </Sidebar.MenuItem>
                ))}
              </Sidebar.Menu>
            </Sidebar.Group>
          </Sidebar.Content>
          <Sidebar.Footer>
            <Sidebar.Menu>
              <Sidebar.MenuItem>
                <Sidebar.MenuButton
                  icon={SignOut}
                  onClick={() => {
                    signOut();
                    window.location.assign("/");
                  }}
                >
                  Sign out
                </Sidebar.MenuButton>
              </Sidebar.MenuItem>
            </Sidebar.Menu>
          </Sidebar.Footer>
          <Sidebar.Rail />
        </Sidebar>
        <main className="flex h-full min-h-0 flex-1 flex-col overflow-hidden">
          <div className="border-kumo-hairline flex items-center gap-3 border-b px-4 py-3">
            <Sidebar.Trigger aria-label="Toggle sidebar" />
            <div className="min-w-0">
              <Text as="p" variant="heading">
                Vault
              </Text>
              <Text variant="secondary" size="sm">
                {aal2 ? "password + TOTP" : "session"}
              </Text>
            </div>
          </div>
          <div className="flex min-h-0 flex-1 flex-col overflow-auto">
            <BillingBanner />
            <Outlet />
          </div>
        </main>
      </div>
    </Sidebar.Provider>
  );
}
