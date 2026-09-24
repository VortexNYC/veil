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
import { beginLogin, signedIn, signOut, token, amrOf } from "../auth";

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
            <Outlet />
          </div>
        </main>
      </div>
    </Sidebar.Provider>
  );
}
