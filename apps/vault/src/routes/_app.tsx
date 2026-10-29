import { Badge } from "@cloudflare/kumo/components/badge";
import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { Input } from "@cloudflare/kumo/components/input";
import { Label } from "@cloudflare/kumo/components/label";
import { Sidebar } from "@cloudflare/kumo/components/sidebar";
import { Text } from "@cloudflare/kumo/components/text";
import {
  Gear,
  Heartbeat,
  Key,
  ListBullets,
  Robot,
  EnvelopeSimple,
  ShareNetwork,
  SignOut,
  Stamp,
  Users,
  UsersThree,
  type Icon,
} from "@phosphor-icons/react";
import {
  createFileRoute,
  Outlet,
  redirect,
  useNavigate,
  useRouterState,
} from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { signedIn, signOut, token, amrOf } from "../auth";
import { CACHE_KEYS, preloadVaultCache, readCache, writeCache } from "../cache";
import { FilterPill } from "../ui";
import { billing, billingCheckout, recoveryShare, registerDevice } from "../origin";
import type { BillingView, RecoveryShare } from "@vortex-api/veil";

export const Route = createFileRoute("/_app")({
  beforeLoad: async () => {
    if (signedIn()) {
      return;
    }
    // Embedded sign-in: render Elements at /login on this origin instead of
    // bouncing through Hydra to login.veil.nyc — the cross-app hop is the
    // flicker users see on entry.
    throw redirect({ to: "/login" });
  },
  component: Shell,
});

const nav: ReadonlyArray<{
  to:
    | "/items"
    | "/shares"
    | "/grants"
    | "/groups"
    | "/requests"
    | "/agents"
    | "/invites"
    | "/audit"
    | "/report"
    | "/settings";
  label: string;
  icon: Icon;
}> = [
  { to: "/items", label: "Items", icon: Key },
  { to: "/shares", label: "Shares", icon: ShareNetwork },
  { to: "/report", label: "Report", icon: Heartbeat },
  { to: "/grants", label: "Grants", icon: Users },
  { to: "/groups", label: "Groups", icon: UsersThree },
  { to: "/requests", label: "Requests", icon: Stamp },
  { to: "/agents", label: "Agents", icon: Robot },
  { to: "/invites", label: "Invites", icon: EnvelopeSimple },
  { to: "/audit", label: "Audit", icon: ListBullets },
  { to: "/settings", label: "Settings", icon: Gear },
];

// BillingBanner is the cap surface: at/over the free allowance the org's
// agents and API calls are already being denied — this is where the owner
// finds out why. Members (403) and paid plans render nothing.

// registerWebDevice — one-shot per browser: a stable id + label go to the
// linked-devices inventory so this session shows in Settings.
function useRegisterWebDevice() {
  useEffect(() => {
    let id = localStorage.getItem("veil.web.device.id");
    if (!id) {
      id = `web-${crypto.randomUUID().slice(0, 12)}`;
      localStorage.setItem("veil.web.device.id", id);
    }
    const label = `${navigator.platform === "MacIntel" ? "Mac" : navigator.platform} browser`;
    void registerDevice(id, label, "web").catch(() => {});
  }, []);
}
function BillingBanner() {
  const [view, setView] = useState<BillingView | null>(() =>
    readCache<BillingView>(CACHE_KEYS.billing),
  );
  useEffect(() => {
    let live = true;
    const load = async () => {
      const res = await billing();
      if (live && !res.error && res.data) {
        writeCache(CACHE_KEYS.billing, res.data);
        setView(res.data);
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
  // Subscribe mints a fresh org-bound checkout per click — checkout URLs
  // are single-use hash tokens, so caching one would dead-end on the pay
  // page. upgrade_url stays as a static fallback when billing is unwired.
  const action = (
    <Banner.Action
      onClick={() => {
        void billingCheckout().then((res) => {
          const url = res.data?.checkout_url || view.upgrade_url;
          if (url) {
            window.location.assign(url);
          }
        });
      }}
    >
      Subscribe ($1/mo per user)
    </Banner.Action>
  );
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

// RecoveryKitBanner is the lazy VEIL-78 backfill: any signed-in human with
// no enrolled org-key share gets the nudge — the private half is minted in
// the browser on Settings, so this banner is the only reminder they get.
function RecoveryKitBanner() {
  const navigate = useNavigate();
  const [unenrolled, setUnenrolled] = useState<boolean>(() => {
    const cached = readCache<RecoveryShare>(CACHE_KEYS.recoveryShare);
    return cached ? cached.enrolled === false : false;
  });
  useEffect(() => {
    let live = true;
    void recoveryShare().then((res) => {
      if (live && !res.error && res.data) {
        writeCache(CACHE_KEYS.recoveryShare, res.data);
        setUnenrolled(res.data.enrolled === false);
      }
    });
    return () => {
      live = false;
    };
  }, []);
  if (!unenrolled) {
    return null;
  }
  return (
    <Banner
      variant="alert"
      title="Save your Recovery Kit to protect your account against device loss"
      description="Generate a recovery key in Settings — your vault stays recoverable even if every device is lost."
      action={
        <Banner.Action onClick={() => void navigate({ to: "/settings" })}>
          Open Settings
        </Banner.Action>
      }
    />
  );
}

const WORDS = [
  "amber", "anchor", "apollo", "arctic", "aspen", "atlas", "aurora", "beacon",
  "bison", "breeze", "bronze", "canyon", "cedar", "cobalt", "comet", "coral",
  "cosmos", "crystal", "cypress", "delta", "dune", "eclipse", "ember", "falcon",
  "fjord", "flint", "forest", "glacier", "granite", "harbor", "horizon", "indigo",
  "island", "jasper", "juniper", "kestrel", "lagoon", "lantern", "laurel", "lotus",
  "magnet", "maple", "mariner", "meadow", "mercury", "meteor", "monarch", "nebula",
  "obsidian", "onyx", "opal", "orbit", "osprey", "panther", "pebble", "phoenix",
  "pioneer", "plaza", "polaris", "prism", "quartz", "raven", "redwood", "ridge",
  "river", "saffron", "sapphire", "sequoia", "shadow", "sierra", "silver", "solstice",
  "sparrow", "summit", "tempest", "thistle", "thunder", "timber", "topaz", "tundra",
  "velvet", "vertex", "Vortex", "voyager", "whisper", "willow", "zenith", "zephyr",
];

function buildGeneratedValue(opts: {
  mode: "password" | "passphrase";
  length: number;
  upper: boolean;
  lower: boolean;
  digits: boolean;
  symbols: boolean;
  words: number;
  sep: string;
}): { value: string; bits: number } {
  if (opts.mode === "passphrase") {
    const buf = new Uint32Array(opts.words);
    crypto.getRandomValues(buf);
    const picked: string[] = [];
    for (const v of buf) {
      picked.push(WORDS[v % WORDS.length]!);
    }
    const suffix = (buf[0]! % 90) + 10;
    return {
      value: `${picked.join(opts.sep)}${opts.sep}${suffix}`,
      bits: Math.round(opts.words * Math.log2(WORDS.length) + 6.5),
    };
  }
  let pool = "";
  if (opts.upper) pool += "ABCDEFGHJKLMNPQRSTUVWXYZ";
  if (opts.lower) pool += "abcdefghijkmnopqrstuvwxyz";
  if (opts.digits) pool += "23456789";
  if (opts.symbols) pool += "!@#$%^&*-_=+";
  if (!pool) pool = "abcdefghijkmnopqrstuvwxyz23456789";
  const buf = new Uint32Array(opts.length);
  crypto.getRandomValues(buf);
  let out = "";
  for (const v of buf) {
    out += pool[v % pool.length];
  }
  return {
    value: out,
    bits: Math.round(opts.length * Math.log2(pool.length)),
  };
}

function PasswordGeneratorModal({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (next: boolean) => void;
}) {
  const [mode, setMode] = useState<"password" | "passphrase">("password");
  const [length, setLength] = useState(24);
  const [upper, setUpper] = useState(true);
  const [lower, setLower] = useState(true);
  const [digits, setDigits] = useState(true);
  const [symbols, setSymbols] = useState(true);
  const [words, setWords] = useState(5);
  const [sep, setSep] = useState("-");
  const [nonce, setNonce] = useState(0);
  const [copied, setCopied] = useState(false);

  const generated = buildGeneratedValue({
    mode,
    length,
    upper,
    lower,
    digits,
    symbols,
    words,
    sep,
  });
  void nonce;

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog size="lg" className="w-[32rem] max-w-[92vw] p-6">
        <div className="flex flex-col gap-4">
          <div className="flex items-center justify-between">
            <Dialog.Title>Password Generator</Dialog.Title>
            <Badge variant="neutral">
              {generated.bits >= 100
                ? `Strong · ${generated.bits} bits`
                : `${generated.bits} bits`}
            </Badge>
          </div>
          <div className="flex gap-1.5">
            <FilterPill
              active={mode === "password"}
              onClick={() => setMode("password")}
            >
              Random Password
            </FilterPill>
            <FilterPill
              active={mode === "passphrase"}
              onClick={() => setMode("passphrase")}
            >
              Memorable Passphrase
            </FilterPill>
          </div>
          <div className="flex items-center gap-2">
            <Input
              className="w-full font-mono text-sm"
              readOnly
              value={generated.value}
            />
            <Button
              type="button"
              variant="secondary"
              onClick={() => {
                setCopied(false);
                setNonce((n) => n + 1);
              }}
            >
              Regenerate
            </Button>
            <Button
              type="button"
              variant="primary"
              onClick={() => {
                void navigator.clipboard
                  ?.writeText(generated.value)
                  .then(() => setCopied(true));
              }}
            >
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
          {mode === "password" ? (
            <>
              <div className="flex flex-col gap-1">
                <Label>Length: {length} characters</Label>
                <input
                  type="range"
                  min={8}
                  max={64}
                  value={length}
                  onChange={(e) => {
                    setCopied(false);
                    setLength(Number(e.currentTarget.value));
                  }}
                />
              </div>
              <div className="flex flex-wrap gap-4 text-xs">
                <label className="flex items-center gap-1.5">
                  <input
                    type="checkbox"
                    checked={upper}
                    onChange={(e) => setUpper(e.currentTarget.checked)}
                  />
                  A–Z
                </label>
                <label className="flex items-center gap-1.5">
                  <input
                    type="checkbox"
                    checked={lower}
                    onChange={(e) => setLower(e.currentTarget.checked)}
                  />
                  a–z
                </label>
                <label className="flex items-center gap-1.5">
                  <input
                    type="checkbox"
                    checked={digits}
                    onChange={(e) => setDigits(e.currentTarget.checked)}
                  />
                  0–9
                </label>
                <label className="flex items-center gap-1.5">
                  <input
                    type="checkbox"
                    checked={symbols}
                    onChange={(e) => setSymbols(e.currentTarget.checked)}
                  />
                  !@#$%^&*
                </label>
              </div>
            </>
          ) : (
            <div className="flex items-center justify-between gap-4">
              <div className="flex flex-1 flex-col gap-1">
                <Label>Words: {words}</Label>
                <input
                  type="range"
                  min={4}
                  max={8}
                  value={words}
                  onChange={(e) => {
                    setCopied(false);
                    setWords(Number(e.currentTarget.value));
                  }}
                />
              </div>
              <div className="flex items-center gap-1.5 text-xs">
                <span>Separator:</span>
                {["-", ".", "_"].map((s) => (
                  <button
                    key={s}
                    type="button"
                    onClick={() => setSep(s)}
                    className={`cursor-pointer rounded px-2 py-1 font-mono ${
                      sep === s
                        ? "bg-kumo-tint text-kumo-default"
                        : "bg-kumo-elevated/60 text-kumo-subtle"
                    }`}
                  >
                    {s}
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>
      </Dialog>
    </Dialog.Root>
  );
}

function lockVaultNow() {
  if (typeof window !== "undefined" && window.location.protocol === "veil-app:") {
    window.location.assign("veil-app://vault/lock");
    return;
  }
  signOut();
  window.location.assign("/");
}

function Shell() {
  useRegisterWebDevice();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const navigate = useNavigate();
  const [genOpen, setGenOpen] = useState(false);
  const [updateReady, setUpdateReady] = useState(false);
  const raw = token();
  const aal2 = raw ? amrOf(raw).includes("totp") : false;

  useEffect(() => {
    preloadVaultCache();
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "l") {
        e.preventDefault();
        lockVaultNow();
      } else if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "g") {
        e.preventDefault();
        setGenOpen(true);
      }
    }
    const currentScript = document
      .querySelector<HTMLScriptElement>(
        'script[type="module"][src*="/assets/index-"]',
      )
      ?.getAttribute("src");
    function checkBundleUpdate() {
      if (!currentScript) return;
      void fetch("https://app.veil.nyc/", { cache: "no-store" })
        .then((r) => (r.ok ? r.text() : ""))
        .then((html) => {
          const m = html.match(/\/assets\/index-[A-Za-z0-9_-]+\.js/);
          if (m && m[0] && m[0] !== currentScript) {
            setUpdateReady(true);
          }
        })
        .catch(() => {});
    }
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("focus", checkBundleUpdate);
    const t = setInterval(checkBundleUpdate, 300_000);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("focus", checkBundleUpdate);
      clearInterval(t);
    };
  }, []);

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
          <div className="border-kumo-hairline flex shrink-0 items-center justify-between gap-3 border-b px-4 py-2">
            <div className="flex items-center gap-2">
              <Sidebar.Trigger aria-label="Toggle sidebar" />
              <Text as="span" size="sm">
                Vault
              </Text>
              <Badge variant="neutral">
                {aal2 ? "password + TOTP" : "session"}
              </Badge>
            </div>
            <div className="flex items-center gap-2">
              {updateReady ? (
                <Button
                  type="button"
                  variant="primary"
                  onClick={() => window.location.reload()}
                >
                  Update available — Reload
                </Button>
              ) : null}
              <Button
                type="button"
                variant="secondary"
                title="Open Password & Passphrase Generator (⌘G)"
                onClick={() => setGenOpen(true)}
              >
                Generator ⌘G
              </Button>
              <Button
                type="button"
                variant="outline"
                title="Lock Vault (⌘L)"
                onClick={lockVaultNow}
              >
                Lock ⌘L
              </Button>
            </div>
          </div>
          <div className="flex min-h-0 flex-1 flex-col overflow-auto">
            <BillingBanner />
            <RecoveryKitBanner />
            <Outlet />
          </div>
        </main>
        <PasswordGeneratorModal open={genOpen} onOpenChange={setGenOpen} />
      </div>
    </Sidebar.Provider>
  );
}
