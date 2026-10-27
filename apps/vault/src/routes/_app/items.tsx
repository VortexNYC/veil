import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Input, Textarea } from "@cloudflare/kumo/components/input";
import { Label } from "@cloudflare/kumo/components/label";
import { Select } from "@cloudflare/kumo/components/select";
import { Text } from "@cloudflare/kumo/components/text";
import {
  CreditCard,
  File,
  FileText,
  Fingerprint,
  IdentificationCard,
  Key,
  LockKey,
  Star,
  Terminal,
} from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import {
  CACHE_KEYS,
  fmtRelativeTime,
  readCache,
  writeCache,
} from "../../cache";
import {
  addItem,
  archiveVaultItem,
  createShareLink,
  deleteVaultItem,
  enrollVaultTOTP,
  importItems,
  items,
  mintVaultTOTP,
  parseTOTPSeed,
  revealVaultItem,
  rotateProviders,
  rotateVaultItem,
  updateVaultItem,
  verifyVaultItem,
  type RevealedItem,
} from "../../origin";
import { sealShare } from "../../share";
import type {
  AuditEvent,
  Grant,
  Item,
  VaultReport,
} from "@vortex-api/veil";

export const Route = createFileRoute("/_app/items")({
  component: Items,
});

type Kind = "api_key" | "ssh" | "wif" | "card" | "identity" | "secure_note";

function isKind(value: string): value is Kind {
  return (
    value === "api_key" ||
    value === "ssh" ||
    value === "wif" ||
    value === "card" ||
    value === "identity" ||
    value === "secure_note"
  );
}

type KindFilter = "all" | "pinned" | "2fa" | Item["kind"];

function isKindFilter(value: string): value is KindFilter {
  return (
    value === "all" ||
    value === "pinned" ||
    value === "2fa" ||
    value === "api_key" ||
    value === "passkey" ||
    value === "card" ||
    value === "identity" ||
    value === "ssh" ||
    value === "oauth" ||
    value === "file" ||
    value === "secure_note"
  );
}

const KIND_FILTER_OPTIONS: { value: KindFilter; label: string }[] = [
  { value: "all", label: "All accounts" },
  { value: "pinned", label: "Pinned" },
  { value: "2fa", label: "2FA / TOTP enabled" },
  { value: "api_key", label: "Passwords / API keys" },
  { value: "passkey", label: "Passkeys" },
  { value: "card", label: "Cards" },
  { value: "identity", label: "Identities" },
  { value: "ssh", label: "SSH keys" },
  { value: "oauth", label: "OAuth / WIF" },
  { value: "file", label: "Files" },
  { value: "secure_note", label: "Secure notes" },
];

const QUICK_PILLS: { value: KindFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "pinned", label: "Pinned" },
  { value: "2fa", label: "2FA" },
  { value: "api_key", label: "Logins" },
  { value: "card", label: "Cards" },
  { value: "ssh", label: "SSH" },
  { value: "secure_note", label: "Notes" },
];

// CSPRNG password for the inline Generate buttons — 24 chars from a
// mixed alphabet, rejection-free via Uint32 modulo.
function generateSecret(): string {
  const alphabet =
    "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*-_=+";
  const buf = new Uint32Array(24);
  crypto.getRandomValues(buf);
  let out = "";
  for (const v of buf) {
    out += alphabet[v % alphabet.length];
  }
  return out;
}

// Row icons the way 1Password's rich icons make items recognizable. Like
// the fill host's icon cache, the favicon is fetched from the site itself —
// no aggregator learns which domains this vault holds. Remote icons stay
// opt-in: rendering them pings every saved domain with the member's IP, so
// rows draw kind glyphs until the human flips the toggle.
const KIND_ICON = {
  api_key: Key,
  passkey: Fingerprint,
  card: CreditCard,
  identity: IdentificationCard,
  ssh: Terminal,
  oauth: LockKey,
  file: File,
  secure_note: FileText,
  honey: Key,
} as const;

function firstURIDomain(item: Item): string {
  for (const raw of item.uris ?? []) {
    try {
      const host = new URL(raw).hostname;
      if (host) {
        return host;
      }
    } catch {
      // ssh://, app://, or a bare hostname — not a fetchable web origin.
    }
  }
  return "";
}

function ItemIcon({ item, enabled }: { item: Item; enabled: boolean }) {
  const [failed, setFailed] = useState(false);
  const host = firstURIDomain(item);
  const Glyph = KIND_ICON[item.kind] ?? Key;
  if (host && enabled && !failed) {
    return (
      <img
        src={`https://${host}/favicon.ico`}
        alt=""
        width={20}
        height={20}
        loading="lazy"
        className="size-5 shrink-0 rounded-sm"
        onError={() => setFailed(true)}
      />
    );
  }
  return <Glyph size={20} className="shrink-0 opacity-70" aria-hidden />;
}

function parseList(value: string): string[] {
  return value
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

function itemMatches(item: Item, query: string): boolean {
  const terms = query
    .trim()
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean);
  if (terms.length === 0) {
    return true;
  }
  const haystack = [
    item.name,
    item.id,
    item.login ?? "",
    item.kind,
    item.has_totp ? "2fa totp mfa otp" : "",
    ...(item.uris ?? []),
    ...(item.tags ?? []),
  ]
    .join("\n")
    .toLowerCase();
  return terms.every((t) => haystack.includes(t));
}

const ITEMS_CACHE_KEY = "veil.items.cache";
const FAVORITES_KEY = "veil.favorites";

function loadCachedItems(): Item[] | null {
  try {
    const raw = sessionStorage.getItem(ITEMS_CACHE_KEY);
    if (!raw) {
      return null;
    }
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) ? (parsed as Item[]) : null;
  } catch {
    return null;
  }
}

function loadFavorites(): Set<string> {
  try {
    const raw = localStorage.getItem(FAVORITES_KEY);
    if (!raw) {
      return new Set();
    }
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed)
      ? new Set(parsed.filter((v): v is string => typeof v === "string"))
      : new Set();
  } catch {
    return new Set();
  }
}

function Items() {
  const [rows, setRows] = useState<Item[] | null>(() => loadCachedItems());
  const [favorites, setFavorites] = useState<Set<string>>(() => loadFavorites());
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [rotateTarget, setRotateTarget] = useState<string | null>(null);
  const [editTarget, setEditTarget] = useState<Item | null>(null);
  const [shareTarget, setShareTarget] = useState<Item | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(() => {
    const pick = sessionStorage.getItem(CACHE_KEYS.selectItemId);
    if (pick) {
      sessionStorage.removeItem(CACHE_KEYS.selectItemId);
      return pick;
    }
    return null;
  });
  const [zeroTouchHosts, setZeroTouchHosts] = useState<string[]>([]);
  const [kind, setKind] = useState<Kind>("api_key");
  const [query, setQuery] = useState("");
  const [kindFilter, setKindFilter] = useState<KindFilter>("all");
  const [iconsOn, setIconsOn] = useState<boolean>(
    () => localStorage.getItem("veil.icons") !== "0",
  );
  const [addSecret, setAddSecret] = useState("");
  const [addSecretVisible, setAddSecretVisible] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  function isPinned(item: Item): boolean {
    return favorites.has(item.id) || (item.tags ?? []).includes("favorite");
  }

  function togglePinned(item: Item) {
    setFavorites((prev) => {
      const next = new Set(prev);
      if (next.has(item.id)) {
        next.delete(item.id);
      } else {
        next.add(item.id);
      }
      try {
        localStorage.setItem(FAVORITES_KEY, JSON.stringify([...next]));
      } catch {
        // ignore storage quota errors
      }
      return next;
    });
  }

  const filtered = (rows ?? [])
    .filter(
      (item) =>
        (kindFilter === "all" ||
          (kindFilter === "pinned"
            ? isPinned(item)
            : kindFilter === "2fa"
              ? Boolean(item.has_totp)
              : item.kind === kindFilter)) &&
        itemMatches(item, query),
    )
    .sort((a, b) => {
      const ap = isPinned(a) ? 0 : 1;
      const bp = isPinned(b) ? 0 : 1;
      if (ap !== bp) {
        return ap - bp;
      }
      return a.name.localeCompare(b.name);
    });

  const selectedItem =
    filtered.find((item) => item.id === selectedId) ?? filtered[0] ?? null;

  useEffect(() => {
    if (!selectedItem || !listRef.current) {
      return;
    }
    const btn = listRef.current.querySelector<HTMLElement>(
      `[data-item-id="${CSS.escape(selectedItem.id)}"]`,
    );
    btn?.scrollIntoView({ block: "nearest" });
  }, [selectedItem?.id]);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      const dialogOpen =
        open ||
        editTarget !== null ||
        rotateTarget !== null ||
        shareTarget !== null;
      const target = e.target as HTMLElement | null;
      const inInput =
        target?.tagName === "INPUT" ||
        target?.tagName === "TEXTAREA" ||
        target?.isContentEditable;

      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        searchRef.current?.focus();
        searchRef.current?.select();
        return;
      }
      if (!inInput && !dialogOpen && e.key === "/") {
        e.preventDefault();
        searchRef.current?.focus();
        return;
      }
      if (dialogOpen) {
        return;
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "n") {
        e.preventDefault();
        setOpen(true);
        return;
      }
      if (
        !inInput &&
        (e.key === "ArrowDown" || e.key === "ArrowUp") &&
        filtered.length > 0
      ) {
        e.preventDefault();
        const curIdx = selectedItem
          ? filtered.findIndex((i) => i.id === selectedItem.id)
          : 0;
        const nextIdx =
          e.key === "ArrowDown"
            ? Math.min(filtered.length - 1, curIdx + 1)
            : Math.max(0, curIdx - 1);
        const next = filtered[nextIdx];
        if (next) {
          setSelectedId(next.id);
        }
        return;
      }
      if (!selectedItem) {
        return;
      }
      const key = e.key.toLowerCase();
      if ((e.metaKey || e.ctrlKey) && e.shiftKey && key === "c") {
        if (selectedItem.login) {
          e.preventDefault();
          void navigator.clipboard?.writeText(selectedItem.login).then(() => {
            setError(null);
            setNotice(`Copied username for ${selectedItem.name}`);
          });
        }
        return;
      }
      if ((e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey) {
        if (key === "u" && selectedItem.login) {
          e.preventDefault();
          void navigator.clipboard?.writeText(selectedItem.login).then(() => {
            setError(null);
            setNotice(`Copied username for ${selectedItem.name}`);
          });
          return;
        }
        if (key === "t" && selectedItem.has_totp) {
          e.preventDefault();
          void mintVaultTOTP(selectedItem.id).then((res) => {
            if (res.totp) {
              void navigator.clipboard?.writeText(res.totp).then(() => {
                setError(null);
                setNotice(
                  `Copied 2FA code for ${selectedItem.name}: ${res.totp}`,
                );
              });
            }
          });
          return;
        }
        if (key === "e") {
          e.preventDefault();
          setEditTarget(selectedItem);
          return;
        }
        if (key === "o") {
          const webURI = (selectedItem.uris ?? []).find(
            (u) =>
              u.toLowerCase().startsWith("http://") ||
              u.toLowerCase().startsWith("https://"),
          );
          if (webURI) {
            e.preventDefault();
            window.open(webURI, "_blank", "noopener,noreferrer");
          }
          return;
        }
        if (
          key === "c" &&
          !inInput &&
          (window.getSelection()?.toString() ?? "") === ""
        ) {
          e.preventDefault();
          void revealVaultItem(selectedItem.id, selectedItem.has_totp).then(
            (res) => {
              if (res.data?.password) {
                void navigator.clipboard
                  ?.writeText(res.data.password)
                  .then(() => {
                    setError(null);
                    setNotice(`Copied password for ${selectedItem.name}`);
                  });
              }
            },
          );
        }
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  });

  async function reload() {
    const res = await items();
    if (res.error || !res.data) {
      setError("list failed");
      if (rows === null) {
        setRows([]);
      }
      return;
    }
    setError(null);
    writeCache(CACHE_KEYS.items, res.data.items);
    setRows(res.data.items);
    const pres = await rotateProviders();
    if (pres.data) {
      setZeroTouchHosts(pres.data.zero_touch_hosts);
    }
  }

  useEffect(() => {
    void reload();
    function onFocus() {
      void reload();
    }
    function onVisibility() {
      if (document.visibilityState === "visible") {
        void reload();
      }
    }
    window.addEventListener("focus", onFocus);
    document.addEventListener("visibilitychange", onVisibility);
    const interval = setInterval(() => {
      if (document.visibilityState === "visible") {
        void reload();
      }
    }, 60_000);
    return () => {
      window.removeEventListener("focus", onFocus);
      document.removeEventListener("visibilitychange", onVisibility);
      clearInterval(interval);
    };
  }, []);

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col gap-3 overflow-hidden p-4">
      <div className="flex shrink-0 items-center justify-between gap-3">
        <div className="flex min-w-0 items-baseline gap-3">
          <Text as="h1" variant="heading">
            Items
          </Text>
          {rows !== null ? (
            <Text as="span" variant="secondary" size="sm">
              {filtered.length === rows.length
                ? `${rows.length} accounts`
                : `${filtered.length} of ${rows.length} accounts`}
            </Text>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          <input
            ref={fileRef}
            type="file"
            accept=".csv,.1pux,.xml,.json,.zip,text/csv,application/zip"
            className="sr-only"
            onChange={(e) => {
              const file = e.currentTarget.files?.[0];
              e.currentTarget.value = "";
              if (!file) {
                return;
              }
              void importItems(file).then(async (res) => {
                if (!res.ok) {
                  setError("import failed");
                  return;
                }
                const body = (await res.json().catch(() => null)) as {
                  count?: number;
                } | null;
                setError(null);
                setNotice(
                  body?.count != null
                    ? `Imported ${body.count} item${body.count === 1 ? "" : "s"} — try filling one to verify`
                    : "Import complete",
                );
                await reload();
              });
            }}
          />
          <Button variant="outline" type="button" onClick={() => fileRef.current?.click()}>
            Import
          </Button>
          <Button type="button" variant="primary" onClick={() => setOpen(true)}>
            Add
          </Button>
          <Dialog.Root
            open={open}
            onOpenChange={(next) => {
              setOpen(next);
              if (!next) {
                setKind("api_key");
                setAddSecret("");
                setAddSecretVisible(false);
              }
            }}
          >
            <Dialog size="sm" className="p-6">
              <form
                className="flex flex-col gap-4"
                onSubmit={(e) => {
                  e.preventDefault();
                  const fd = new FormData(e.currentTarget);
                  const name = String(fd.get("name") ?? "");
                  const tags = parseList(String(fd.get("tags") ?? ""));
                  const travelSafe = fd.get("travel_safe") === "on";
                  const body =
                    kind === "wif"
                      ? {
                          name,
                          kind: "oauth" as const,
                          uri: String(fd.get("uri") ?? "").trim() || "https://api.anthropic.com",
                          travel_safe: travelSafe,
                          wif: {
                            token_url:
                              String(fd.get("token_url") ?? "").trim() ||
                              "https://api.anthropic.com/v1/oauth/token",
                            assertion_token_url:
                              String(fd.get("assertion_token_url") ?? "").trim() ||
                              "https://id.veil.nyc/oauth2/token",
                            assertion_client_id: String(fd.get("assertion_client_id") ?? "").trim(),
                            assertion_secret: String(fd.get("assertion_secret") ?? "").trim(),
                            assertion_audience:
                              String(fd.get("assertion_audience") ?? "").trim() || undefined,
                            federation_rule_id: String(fd.get("federation_rule_id") ?? "").trim(),
                            organization_id:
                              String(fd.get("organization_id") ?? "").trim() || undefined,
                            service_account_id:
                              String(fd.get("service_account_id") ?? "").trim() || undefined,
                            workspace_id: String(fd.get("workspace_id") ?? "").trim() || undefined,
                          },
                        }
                      : kind === "card"
                      ? {
                          name,
                          kind,
                          travel_safe: travelSafe,
                          card: {
                            number: String(fd.get("number") ?? "") || undefined,
                            exp_month: String(fd.get("exp_month") ?? "") || undefined,
                            exp_year: String(fd.get("exp_year") ?? "") || undefined,
                            cvv: String(fd.get("cvv") ?? "") || undefined,
                            holder: String(fd.get("holder") ?? "") || undefined,
                          },
                        }
                      : kind === "identity"
                        ? {
                            name,
                            kind,
                            travel_safe: travelSafe,
                            identity: {
                              given_name: String(fd.get("given_name") ?? "") || undefined,
                              family_name: String(fd.get("family_name") ?? "") || undefined,
                              address: String(fd.get("address") ?? "") || undefined,
                              city: String(fd.get("city") ?? "") || undefined,
                              region: String(fd.get("region") ?? "") || undefined,
                              postal: String(fd.get("postal") ?? "") || undefined,
                              country: String(fd.get("country") ?? "") || undefined,
                              phone: String(fd.get("phone") ?? "") || undefined,
                              email: String(fd.get("email") ?? "") || undefined,
                            },
                          }
                      : kind === "ssh"
                        ? {
                            name,
                            kind: "ssh" as const,
                            travel_safe: travelSafe,
                            uri: String(fd.get("uri") ?? "").trim() || undefined,
                            secret: String(fd.get("secret") ?? "") || undefined,
                            ...(tags.length > 0 ? { tags } : {}),
                          }
                        : kind === "secure_note"
                          ? {
                              name,
                              kind,
                              travel_safe: travelSafe,
                              secret: String(fd.get("secret") ?? "") || undefined,
                              ...(tags.length > 0 ? { tags } : {}),
                            }
                          : {
                            name,
                            kind,
                            travel_safe: travelSafe,
                            uri: String(fd.get("uri") ?? "") || undefined,
                            login: String(fd.get("login") ?? "") || undefined,
                            secret: String(fd.get("secret") ?? "") || undefined,
                            totp_seed:
                              parseTOTPSeed(String(fd.get("totp_seed") ?? "")) ||
                              undefined,
                            ...(tags.length > 0 ? { tags } : {}),
                          };
                  void addItem(body).then((res) => {
                    if (res.error) {
                      setError("create failed");
                      return;
                    }
                    setOpen(false);
                    setKind("api_key");
                    setAddSecret("");
                    setAddSecretVisible(false);
                    void reload();
                  });
                }}
              >
                <Dialog.Title>Add item</Dialog.Title>
                <Dialog.Description>
                  The secret is sent once. Reveal it later from the row actions — audited, humans only.
                </Dialog.Description>
                <Select
                  value={kind}
                  label="Kind"
                  hideLabel={false}
                  renderValue={(value) =>
                    value === "api_key"
                      ? "Password / API key"
                      : value === "ssh"
                        ? "SSH private key"
                        : value === "wif"
                          ? "Outbound WIF (Anthropic / RFC 7523)"
                          : value === "card"
                            ? "Card"
                            : value === "secure_note"
                              ? "Secure note"
                              : "Identity"
                  }
                  onValueChange={(value) => {
                    if (typeof value === "string" && isKind(value)) {
                      setKind(value);
                    }
                  }}
                >
                  <Select.Option value="api_key">Password / API key</Select.Option>
                  <Select.Option value="ssh">SSH private key</Select.Option>
                  <Select.Option value="wif">Outbound WIF (Anthropic / RFC 7523)</Select.Option>
                  <Select.Option value="card">Card</Select.Option>
                  <Select.Option value="identity">Identity</Select.Option>
                  <Select.Option value="secure_note">Secure note</Select.Option>
                </Select>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="name">Name</Label>
                  <Input id="name" name="name" required autoComplete="off" />
                </div>
                {kind === "api_key" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="uri">URI</Label>
                      <Input id="uri" name="uri" placeholder="https://api.example.com" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="login">Fill username</Label>
                      <Input id="login" name="login" autoComplete="username" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="secret">Secret</Label>
                      <div className="flex items-center gap-2">
                        <div className="flex-1">
                          <Input
                            id="secret"
                            name="secret"
                            type={addSecretVisible ? "text" : "password"}
                            autoComplete="new-password"
                            value={addSecret}
                            onChange={(e) => setAddSecret(e.currentTarget.value)}
                          />
                        </div>
                        <Button
                          type="button"
                          variant="secondary"
                          onClick={() => {
                            setAddSecret(generateSecret());
                            setAddSecretVisible(true);
                          }}
                        >
                          Generate
                        </Button>
                      </div>
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="totp_seed">TOTP seed or otpauth:// URI (optional)</Label>
                      <Input
                        id="totp_seed"
                        name="totp_seed"
                        placeholder="otpauth://totp/… or base32 seed"
                        autoComplete="off"
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="tags">Tags (optional, comma-separated)</Label>
                      <Input id="tags" name="tags" placeholder="prod, github" autoComplete="off" />
                    </div>
                  </>
                ) : null}
                {kind === "ssh" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="uri">URI (optional, e.g. ssh://host)</Label>
                      <Input id="uri" name="uri" placeholder="ssh://git@github.com" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="ssh-secret">Private key (PEM / OpenSSH)</Label>
                      <Textarea
                        id="ssh-secret"
                        name="secret"
                        placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                        rows={6}
                        required
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="tags">Tags (optional)</Label>
                      <Input id="tags" name="tags" placeholder="prod, deploy" autoComplete="off" />
                    </div>
                  </>
                ) : null}
                {kind === "secure_note" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="note-secret">Note</Label>
                      <Textarea
                        id="note-secret"
                        name="secret"
                        placeholder="Recovery codes, serial numbers, instructions…"
                        rows={8}
                        required
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="tags">Tags (optional)</Label>
                      <Input id="tags" name="tags" placeholder="personal, reference" autoComplete="off" />
                    </div>
                  </>
                ) : null}
                {kind === "wif" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="uri">Target API URI</Label>
                      <Input id="uri" name="uri" defaultValue="https://api.anthropic.com" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="federation_rule_id">Federation rule ID (fdrl_...)</Label>
                      <Input id="federation_rule_id" name="federation_rule_id" placeholder="fdrl_..." required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="service_account_id">Service account ID (optional, svac_...)</Label>
                      <Input id="service_account_id" name="service_account_id" placeholder="svac_..." />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="workspace_id">Workspace ID (optional, wrkspc_...)</Label>
                      <Input id="workspace_id" name="workspace_id" placeholder="wrkspc_..." />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="organization_id">Organization ID (optional UUID)</Label>
                      <Input id="organization_id" name="organization_id" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="assertion_client_id">Assertion OIDC client ID</Label>
                      <Input id="assertion_client_id" name="assertion_client_id" placeholder="agent-claude" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="assertion_secret">Assertion OIDC client secret</Label>
                      <Input id="assertion_secret" name="assertion_secret" type="password" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="token_url">Provider WIF token URL (optional override)</Label>
                      <Input id="token_url" name="token_url" defaultValue="https://api.anthropic.com/v1/oauth/token" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="assertion_token_url">Assertion OIDC token URL (optional override)</Label>
                      <Input id="assertion_token_url" name="assertion_token_url" defaultValue="https://id.veil.nyc/oauth2/token" />
                    </div>
                  </>
                ) : null}
                {kind === "card" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="number">Number</Label>
                      <Input id="number" name="number" autoComplete="cc-number" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="exp_month">Month</Label>
                      <Input id="exp_month" name="exp_month" autoComplete="cc-exp-month" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="exp_year">Year</Label>
                      <Input id="exp_year" name="exp_year" autoComplete="cc-exp-year" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="cvv">CVV</Label>
                      <Input id="cvv" name="cvv" autoComplete="cc-csc" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="holder">Name on card</Label>
                      <Input id="holder" name="holder" autoComplete="cc-name" />
                    </div>
                  </>
                ) : null}
                {kind === "identity" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="given_name">Given name</Label>
                      <Input id="given_name" name="given_name" autoComplete="given-name" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="family_name">Family name</Label>
                      <Input id="family_name" name="family_name" autoComplete="family-name" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="address">Street</Label>
                      <Input id="address" name="address" autoComplete="address-line1" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="city">City</Label>
                      <Input id="city" name="city" autoComplete="address-level2" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="region">Region</Label>
                      <Input id="region" name="region" autoComplete="address-level1" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="postal">Postal</Label>
                      <Input id="postal" name="postal" autoComplete="postal-code" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="country">Country</Label>
                      <Input id="country" name="country" autoComplete="country-name" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="phone">Phone</Label>
                      <Input id="phone" name="phone" autoComplete="tel" />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="email">Email</Label>
                      <Input id="email" name="email" autoComplete="email" />
                    </div>
                  </>
                ) : null}
                <label className="flex items-center gap-2" htmlFor="travel_safe">
                  <input id="travel_safe" name="travel_safe" type="checkbox" />
                  <Text as="span" variant="secondary" size="sm">
                    Travel safe — stays visible when Travel Mode is on
                  </Text>
                </label>
                <div className="flex justify-end">
                  <Button type="submit" variant="primary">
                    Save
                  </Button>
                </div>
              </form>
            </Dialog>
          </Dialog.Root>
        </div>
      </div>
      {error ? (
        <Text as="p" variant="error">
          {error}
        </Text>
      ) : null}
      {notice ? (
        <Text as="p" variant="secondary">
          {notice}
        </Text>
      ) : null}
      <div className="flex shrink-0 items-center gap-3">
        <div className="flex flex-1 items-center gap-2">
          <Input
            ref={searchRef}
            className="w-full"
            aria-label="Search items"
            placeholder="Search accounts by name, username/email, domain, tag, or '2fa'… (⌘K)"
            value={query}
            onChange={(e) => setQuery(e.currentTarget.value)}
            onKeyDown={(e) => {
              if (filtered.length === 0) {
                return;
              }
              const curIdx = selectedItem
                ? filtered.findIndex((i) => i.id === selectedItem.id)
                : 0;
              if (e.key === "ArrowDown") {
                e.preventDefault();
                const next = filtered[Math.min(filtered.length - 1, curIdx + 1)];
                if (next) {
                  setSelectedId(next.id);
                }
              } else if (e.key === "ArrowUp") {
                e.preventDefault();
                const prev = filtered[Math.max(0, curIdx - 1)];
                if (prev) {
                  setSelectedId(prev.id);
                }
              }
            }}
          />
          {query ? (
            <Button
              type="button"
              variant="secondary"
              onClick={() => {
                setQuery("");
                searchRef.current?.focus();
              }}
            >
              Clear
            </Button>
          ) : null}
        </div>
        <Select
          aria-label="Filter by kind"
          value={kindFilter}
          renderValue={(val) =>
            KIND_FILTER_OPTIONS.find((o) => o.value === val)?.label ?? val
          }
          onValueChange={(value) => {
            if (typeof value === "string" && isKindFilter(value)) {
              setKindFilter(value);
            }
          }}
        >
          {KIND_FILTER_OPTIONS.map((opt) => (
            <Select.Option key={opt.value} value={opt.value}>
              {opt.label}
            </Select.Option>
          ))}
        </Select>
        <Button
          type="button"
          variant={iconsOn ? "primary" : "outline"}
          title="Load each site's favicon — sites see this device fetching them"
          onClick={() =>
            setIconsOn((v) => {
              const next = !v;
              localStorage.setItem("veil.icons", next ? "1" : "0");
              return next;
            })
          }
        >
          Icons
        </Button>
      </div>
      <div className="flex shrink-0 flex-wrap items-center gap-1.5">
        {QUICK_PILLS.map((pill) => {
          const active = kindFilter === pill.value;
          return (
            <button
              key={pill.value}
              type="button"
              onClick={() => setKindFilter(pill.value)}
              className={`cursor-pointer rounded-full px-3 py-1 text-xs font-medium transition-colors ${
                active
                  ? "bg-kumo-tint text-kumo-default ring-kumo-focus/40 ring"
                  : "bg-kumo-elevated/60 text-kumo-subtle hover:bg-kumo-elevated hover:text-kumo-default"
              }`}
            >
              {pill.label}
            </button>
          );
        })}
      </div>
      <div className="bg-kumo-base ring-kumo-line flex min-h-0 flex-1 overflow-hidden rounded-lg ring">
        {rows === null ? (
          <div className="flex flex-1 items-center justify-center p-8">
            <Text variant="secondary">Loading accounts…</Text>
          </div>
        ) : rows.length === 0 ? (
          <div className="flex flex-1 items-center justify-center p-8">
            <Empty
              title="No items"
              description="Add a password, card, or identity, or import a 1Password export."
            />
          </div>
        ) : filtered.length === 0 ? (
          <div className="flex flex-1 items-center justify-center p-8">
            <Empty
              title="No matches"
              description="Nothing matches that search or filter."
            />
          </div>
        ) : (
          <div className="grid h-full min-h-0 w-full flex-1 grid-cols-[340px_minmax(0,1fr)] overflow-hidden">
            <div
              ref={listRef}
              role="listbox"
              aria-label="Accounts"
              className="border-kumo-hairline flex h-full min-h-0 flex-col overflow-y-auto border-r"
            >
              {filtered.map((item) => {
                const active = selectedItem?.id === item.id;
                const pinned = isPinned(item);
                const sub =
                  item.login || firstURIDomain(item) || item.kind;
                return (
                  <button
                    key={item.id}
                    data-item-id={item.id}
                    type="button"
                    role="option"
                    aria-selected={active}
                    onClick={() => setSelectedId(item.id)}
                    className={`border-kumo-hairline/50 flex w-full shrink-0 cursor-pointer items-center gap-3 border-b px-3.5 py-2.5 text-left transition-colors ${
                      active
                        ? "bg-kumo-tint text-kumo-default"
                        : "hover:bg-kumo-elevated/60"
                    }`}
                  >
                    <div className="bg-kumo-elevated flex size-8 shrink-0 items-center justify-center rounded-md">
                      <ItemIcon item={item} enabled={iconsOn} />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-1.5">
                        <span className="truncate text-sm font-medium">
                          {item.name}
                        </span>
                        {pinned ? (
                          <Star
                            size={12}
                            weight="fill"
                            className="shrink-0 text-amber-400"
                            aria-label="Pinned"
                          />
                        ) : null}
                      </div>
                      <div className="text-kumo-subtle truncate text-xs opacity-75">
                        {sub}
                      </div>
                    </div>
                    {item.has_totp ? (
                      <Badge variant="neutral">2FA</Badge>
                    ) : null}
                  </button>
                );
              })}
            </div>
            <div className="flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-y-auto p-6">
              {selectedItem ? (
                <AccountInspector
                  key={selectedItem.id}
                  item={selectedItem}
                  iconsOn={iconsOn}
                  pinned={isPinned(selectedItem)}
                  onTogglePin={() => togglePinned(selectedItem)}
                  onError={setError}
                  onNotice={(msg) => {
                    setError(null);
                    setNotice(msg);
                  }}
                  onEdit={() => setEditTarget(selectedItem)}
                  onShare={() => setShareTarget(selectedItem)}
                  onRotate={() => setRotateTarget(selectedItem.name)}
                  onReload={() => void reload()}
                />
              ) : null}
            </div>
          </div>
        )}
      </div>
      <Dialog.Root
        open={editTarget !== null}
        onOpenChange={(next) => {
          if (!next) {
            setEditTarget(null);
          }
        }}
      >
        <Dialog size="sm" className="p-6">
          {editTarget ? (
            <EditItemForm
              key={editTarget.id}
              item={editTarget}
              onError={setError}
              onDone={(msg) => {
                setEditTarget(null);
                if (msg) {
                  setError(null);
                  setNotice(msg);
                }
                void reload();
              }}
            />
          ) : null}
        </Dialog>
      </Dialog.Root>
      <Dialog.Root
        open={rotateTarget !== null}
        onOpenChange={(next) => {
          if (!next) {
            setRotateTarget(null);
          }
        }}
      >
        <Dialog size="sm" className="p-6">
          <form
            key={rotateTarget ?? "closed"}
            className="flex flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              if (!rotateTarget) {
                return;
              }
              const fd = new FormData(e.currentTarget);
              const secret = String(fd.get("secret") ?? "").trim();
              const interval = String(fd.get("interval") ?? "").trim();
              const mgmt = String(fd.get("management_item") ?? "").trim();
              const body =
                secret || interval || mgmt
                  ? {
                      ...(secret ? { secret } : {}),
                      ...(interval ? { interval } : {}),
                      ...(mgmt ? { management_item: mgmt } : {}),
                    }
                  : undefined;
              void rotateVaultItem(rotateTarget, body).then((res) => {
                if (res.error) {
                  setError("rotate failed");
                  return;
                }
                setNotice(`Rotated ${rotateTarget}`);
                setRotateTarget(null);
                void reload();
              });
            }}
          >
            <Dialog.Title>Rotate {rotateTarget}</Dialog.Title>
            <Dialog.Description>
              Zero-touch hosts ({zeroTouchHosts.join(", ") || "api.resend.com, api.sendgrid.com"}) mint and verify a new key automatically. For dashboard-only providers, paste the replacement secret below.
            </Dialog.Description>
            <div className="flex flex-col gap-1">
              <Label htmlFor="rot-secret">Replacement secret (optional, for assisted rotation)</Label>
              <Input id="rot-secret" name="secret" type="password" />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="rot-interval">Auto-rotate interval (optional, e.g. 24h or 168h)</Label>
              <Input id="rot-interval" name="interval" placeholder="24h" />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="rot-mgmt">Management item (optional, separate admin credential)</Label>
              <Input id="rot-mgmt" name="management_item" />
            </div>
            <div className="flex justify-end">
              <Button type="submit" variant="primary">
                Rotate
              </Button>
            </div>
          </form>
        </Dialog>
      </Dialog.Root>
      <Dialog.Root
        open={shareTarget !== null}
        onOpenChange={(next) => {
          if (!next) {
            setShareTarget(null);
          }
        }}
      >
        <Dialog size="sm" className="p-6">
          {shareTarget ? (
            <ShareItemForm
              key={shareTarget.id}
              item={shareTarget}
              onError={setError}
              onDone={() => setShareTarget(null)}
            />
          ) : null}
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

// ShareItemForm seals a pasted value in this browser (nacl secretbox) and
// POSTs only the envelope — there is no GetSecret, so the human supplies
// the plaintext. The link key stays in the URL fragment; the origin stores
// a blob it cannot open. The share is one-time and dies on redeem.
function ShareItemForm({
  item,
  onError,
  onDone,
}: {
  item: Item;
  onError: (msg: string) => void;
  onDone: () => void;
}) {
  const [link, setLink] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [busy, setBusy] = useState(false);
  const [ttl, setTtl] = useState("86400");
  if (link !== null) {
    return (
      <div className="flex flex-col gap-4">
        <Dialog.Title>Share {item.name}</Dialog.Title>
        <Dialog.Description>
          One-time link. The key after # never reaches the server — anyone
          with the full URL can open it exactly once.
        </Dialog.Description>
        <Input readOnly value={link} onFocus={(e) => e.currentTarget.select()} />
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="primary"
            onClick={() => {
              void navigator.clipboard?.writeText(link).then(() => setCopied(true));
            }}
          >
            {copied ? "Copied" : "Copy link"}
          </Button>
          <Button type="button" variant="secondary" onClick={onDone}>
            Done
          </Button>
        </div>
      </div>
    );
  }
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        const fd = new FormData(e.currentTarget);
        const secret = String(fd.get("secret") ?? "");
        const passphrase = String(fd.get("passphrase") ?? "");
        const ttlSeconds = Number(ttl) || 86400;
        if (!secret) {
          onError("share needs a secret");
          return;
        }
        setBusy(true);
        void sealShare(secret, passphrase)
          .then(({ blobB64, meta, key }) =>
            createShareLink({
              name: item.name,
              blob_b64: blobB64,
              meta,
              ttl_seconds: ttlSeconds,
            }).then((res) => ({ res, key })),
          )
          .then(({ res, key }) => {
            setBusy(false);
            if (res.error || !res.data) {
              onError(`share failed for ${item.name}`);
              return;
            }
            setLink(`${window.location.origin}/s/${res.data.id}#k=${key}`);
          });
      }}
    >
      <Dialog.Title>Share {item.name}</Dialog.Title>
      <Dialog.Description>
        Paste the value to share. It is sealed in this browser — the origin
        stores only the envelope, and the link opens exactly once.
      </Dialog.Description>
      <div className="flex flex-col gap-1">
        <Label htmlFor="share-secret">Secret</Label>
        <Textarea
          id="share-secret"
          name="secret"
          rows={4}
          autoComplete="off"
          required
          placeholder="Paste the value the recipient should see"
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="share-ttl">Link expires after</Label>
        <Select
          value={ttl}
          hideLabel
          aria-label="Link expiry"
          onValueChange={(v) => {
            if (typeof v === "string") {
              setTtl(v);
            }
          }}
        >
          <Select.Option value="3600">1 hour</Select.Option>
          <Select.Option value="86400">24 hours</Select.Option>
          <Select.Option value="604800">7 days</Select.Option>
        </Select>
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="share-passphrase">Passphrase (optional)</Label>
        <Input
          id="share-passphrase"
          name="passphrase"
          type="password"
          autoComplete="off"
          placeholder="Second factor — sent out of band"
        />
      </div>
      <div className="flex justify-end">
        <Button type="submit" variant="primary" disabled={busy}>
          {busy ? "Sealing…" : "Create link"}
        </Button>
      </div>
    </form>
  );
}

function EditItemForm({
  item,
  onError,
  onDone,
}: {
  item: Item;
  onError: (msg: string) => void;
  onDone: (notice?: string) => void;
}) {
  const [secret, setSecret] = useState("");
  const [secretVisible, setSecretVisible] = useState(false);
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        const fd = new FormData(e.currentTarget);
        const login = String(fd.get("login") ?? "").trim();
        const uris = parseList(String(fd.get("uris") ?? ""));
        const tags = parseList(String(fd.get("tags") ?? ""));
        const newSecret = secret.trim();
        void updateVaultItem(item.id, {
          login,
          uris,
          tags,
          travel_safe: fd.get("travel_safe") === "on",
          ...(newSecret ? { secret: newSecret } : {}),
        }).then((res) => {
          if (res.error) {
            onError(`update failed for ${item.name}`);
            return;
          }
          onDone(`Updated ${item.name}`);
        });
      }}
    >
      <Dialog.Title>Edit {item.name}</Dialog.Title>
      <Dialog.Description>
        Metadata saves in place. A new secret rotates the sealed value; leave it
        blank to keep the current one.
      </Dialog.Description>
      <div className="flex flex-col gap-1">
        <Label htmlFor="edit-login">Fill username</Label>
        <Input
          id="edit-login"
          name="login"
          defaultValue={item.login ?? ""}
          autoComplete="username"
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="edit-uris">URIs (comma-separated)</Label>
        <Input
          id="edit-uris"
          name="uris"
          defaultValue={(item.uris ?? []).join(", ")}
          placeholder="https://app.example.com, https://example.com"
          autoComplete="off"
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="edit-tags">Tags (comma-separated)</Label>
        <Input
          id="edit-tags"
          name="tags"
          defaultValue={(item.tags ?? []).join(", ")}
          placeholder="prod, github"
          autoComplete="off"
        />
      </div>
      <label className="flex items-center gap-2" htmlFor="edit-travel-safe">
        <input
          id="edit-travel-safe"
          name="travel_safe"
          type="checkbox"
          defaultChecked={item.travel_safe ?? false}
        />
        <Text as="span" variant="secondary" size="sm">
          Travel safe — stays visible when Travel Mode is on
        </Text>
      </label>
      <div className="flex flex-col gap-1">
        <Label htmlFor="edit-secret">New secret / password (optional)</Label>
        <div className="flex items-center gap-2">
          <div className="flex-1">
            <Input
              id="edit-secret"
              name="secret"
              type={secretVisible ? "text" : "password"}
              autoComplete="new-password"
              value={secret}
              onChange={(e) => setSecret(e.currentTarget.value)}
            />
          </div>
          <Button
            type="button"
            variant="secondary"
            onClick={() => {
              const generated = generateSecret();
              setSecret(generated);
              setSecretVisible(true);
              void navigator.clipboard?.writeText(generated).catch(() => {});
            }}
          >
            Generate
          </Button>
        </div>
      </div>
      <div className="flex justify-end">
        <Button type="submit" variant="primary">
          Save
        </Button>
      </div>
    </form>
  );
}

function AccountInspector({
  item,
  iconsOn,
  pinned,
  onTogglePin,
  onError,
  onNotice,
  onEdit,
  onShare,
  onRotate,
  onReload,
}: {
  item: Item;
  iconsOn: boolean;
  pinned: boolean;
  onTogglePin: () => void;
  onError: (msg: string) => void;
  onNotice: (msg: string) => void;
  onEdit: () => void;
  onShare: () => void;
  onRotate: () => void;
  onReload: () => void;
}) {
  const [secret, setSecret] = useState<RevealedItem | null>(null);
  const [loadingSecret, setLoadingSecret] = useState(false);
  const [passwordVisible, setPasswordVisible] = useState(false);
  const [totp, setTotp] = useState<string | undefined>(undefined);
  const [totpRemaining, setTotpRemaining] = useState<number>(
    30 - (Math.floor(Date.now() / 1000) % 30),
  );
  const [copied, setCopied] = useState<string | null>(null);
  const [showAllURIs, setShowAllURIs] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [enrollingTotp, setEnrollingTotp] = useState(false);
  const [totpSeedInput, setTotpSeedInput] = useState("");
  const [totpBusy, setTotpBusy] = useState(false);
  const onErrorRef = useRef(onError);
  onErrorRef.current = onError;

  const canReveal = item.kind !== "passkey" && item.kind !== "file";
  const canEnrollTotp =
    !item.has_totp && !totp && (item.kind === "api_key" || item.kind === "oauth");

  // Auto-load live 2FA code as soon as a 2FA-enabled account is selected.
  useEffect(() => {
    if (!item.has_totp) {
      return;
    }
    let live = true;
    void mintVaultTOTP(item.id).then((res) => {
      if (live && res.totp) {
        setTotp(res.totp);
      }
    });
    return () => {
      live = false;
    };
  }, [item.id, item.has_totp]);

  useEffect(() => {
    if (!item.has_totp && !totp) {
      return;
    }
    let live = true;
    let lastBucket = Math.floor(Date.now() / 30_000);
    const timer = setInterval(() => {
      const nowSec = Math.floor(Date.now() / 1000);
      setTotpRemaining(30 - (nowSec % 30));
      const bucket = Math.floor(nowSec / 30);
      if (bucket !== lastBucket) {
        lastBucket = bucket;
        void mintVaultTOTP(item.id).then((res) => {
          if (live && res.totp) {
            setTotp(res.totp);
          }
        });
      }
    }, 1000);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [item.id, item.has_totp, totp]);

  async function ensureRevealed(): Promise<RevealedItem | null> {
    if (secret) {
      return secret;
    }
    setLoadingSecret(true);
    const res = await revealVaultItem(item.id, item.has_totp);
    setLoadingSecret(false);
    if (res.error || !res.data) {
      onErrorRef.current(res.error ?? `reveal failed for ${item.name}`);
      return null;
    }
    setSecret(res.data);
    if (res.data.totp) {
      setTotp(res.data.totp);
    }
    return res.data;
  }

  function copy(label: string, value: string, noticeText?: string) {
    void navigator.clipboard?.writeText(value).then(() => {
      setCopied(label);
      if (noticeText) {
        onNotice(noticeText);
      }
    });
  }

  const loginVal = secret?.login || item.login || "";
  const formattedTotp =
    totp && totp.length === 6 ? `${totp.slice(0, 3)} ${totp.slice(3)}` : totp;
  const uris = item.uris ?? [];
  const visibleURIs = showAllURIs ? uris : uris.slice(0, 5);
  const cachedReport = readCache<VaultReport>(CACHE_KEYS.report);
  const finding = cachedReport?.findings.find((f) => f.item_id === item.id);
  const itemGrants = (readCache<Grant[]>(CACHE_KEYS.grants) ?? []).filter(
    (g) => g.item_id === item.id || g.item_id === item.name,
  );
  const itemAudit = (readCache<AuditEvent[]>(CACHE_KEYS.audit) ?? [])
    .filter((e) => e.item_id === item.id || e.item_id === item.name)
    .slice(0, 5);

  return (
    <div className="flex flex-col gap-6">
      <div className="border-kumo-hairline flex flex-wrap items-start justify-between gap-4 border-b pb-4">
        <div className="flex min-w-0 items-center gap-3">
          <div className="bg-kumo-elevated flex size-11 shrink-0 items-center justify-center rounded-lg">
            <ItemIcon item={item} enabled={iconsOn} />
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <Text as="h2" variant="heading">
                {item.name}
              </Text>
              <button
                type="button"
                title={pinned ? "Unpin from top" : "Pin to top of list"}
                onClick={onTogglePin}
                className="cursor-pointer rounded p-1 opacity-80 hover:opacity-100"
              >
                <Star
                  size={16}
                  weight={pinned ? "fill" : "regular"}
                  className={pinned ? "text-amber-400" : "text-kumo-subtle"}
                />
              </button>
              <Badge variant="neutral">{item.kind}</Badge>
              {item.has_totp || totp ? <Badge variant="neutral">2FA</Badge> : null}
              {item.travel_safe ? (
                <Badge variant="neutral">travel safe</Badge>
              ) : null}
              {(item.tags ?? []).map((tag) => (
                <Badge key={tag} variant="neutral">
                  {tag}
                </Badge>
              ))}
            </div>
            {loginVal ? (
              <Text variant="secondary" size="sm">
                {loginVal}
              </Text>
            ) : null}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="button"
            variant="secondary"
            title="Edit account (⌘E)"
            onClick={onEdit}
          >
            Edit
          </Button>
          <Button
            type="button"
            variant="secondary"
            title="Create a one-time sealed share link"
            onClick={onShare}
          >
            Share
          </Button>
          {item.kind === "api_key" ? (
            <>
              <Button
                type="button"
                variant="secondary"
                onClick={() => {
                  void verifyVaultItem(item.name).then((res) => {
                    if (res.error || !res.data) {
                      onError(`verify failed for ${item.name}`);
                      return;
                    }
                    onNotice(`Verified ${item.name} (${res.data.mode})`);
                  });
                }}
              >
                Verify
              </Button>
              <Button type="button" variant="secondary" onClick={onRotate}>
                Rotate
              </Button>
            </>
          ) : null}
          <Button
            type="button"
            variant="secondary"
            onClick={() => {
              void archiveVaultItem(item.id).then((res) => {
                if (res.error) {
                  onError(`archive failed for ${item.name}`);
                  return;
                }
                onNotice(`Archived ${item.name}`);
                onReload();
              });
            }}
          >
            Archive
          </Button>
          {confirmDelete ? (
            <>
              <Button
                type="button"
                variant="primary"
                onClick={() => {
                  void deleteVaultItem(item.id).then((res) => {
                    if (res.error) {
                      onError(`delete failed for ${item.name}`);
                      return;
                    }
                    onNotice(`Deleted ${item.name}`);
                    onReload();
                  });
                }}
              >
                Confirm Delete
              </Button>
              <Button
                type="button"
                variant="secondary"
                onClick={() => setConfirmDelete(false)}
              >
                Cancel
              </Button>
            </>
          ) : (
            <Button
              type="button"
              variant="secondary"
              onClick={() => setConfirmDelete(true)}
            >
              Delete
            </Button>
          )}
        </div>
      </div>

      {finding ? (
        <div className="bg-kumo-elevated/60 ring-kumo-hairline flex items-center justify-between gap-3 rounded-lg px-4 py-3 ring">
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <Badge variant={finding.pwned > 0 ? "red" : finding.reused ? "orange" : "warning"}>
              {finding.pwned > 0
                ? `Breached ×${finding.pwned}`
                : finding.reused
                  ? `Reused ×${finding.reused}`
                  : "Weak password"}
            </Badge>
            <span className="text-kumo-subtle">
              {[
                ...(finding.weak ?? []),
                finding.reused ? `reused across ${finding.reused} accounts` : "",
              ]
                .filter(Boolean)
                .join(" · ") || "Flagged by Watchtower"}
            </span>
          </div>
          <Button type="button" variant="secondary" onClick={onEdit}>
            Change Password
          </Button>
        </div>
      ) : null}

      <div className="bg-kumo-elevated/40 ring-kumo-hairline flex flex-col divide-y divide-neutral-800/60 rounded-lg ring">
        <div className="flex items-center justify-between gap-4 px-4 py-3">
          <div className="min-w-0 flex-1">
            <div className="text-kumo-subtle text-xs font-medium uppercase tracking-wide">
              Username / Email
            </div>
            <div className="mt-0.5 truncate font-mono text-sm">
              {loginVal || "—"}
            </div>
          </div>
          {loginVal ? (
            <Button
              type="button"
              variant="secondary"
              title="Copy username / email (⌘U)"
              onClick={() =>
                copy("login", loginVal, `Copied username for ${item.name}`)
              }
            >
              {copied === "login" ? "Copied" : "Copy ⌘U"}
            </Button>
          ) : null}
        </div>

        {canReveal ? (
          <div className="flex flex-col gap-2 px-4 py-3">
            <div className="flex items-center justify-between gap-4">
              <div className="min-w-0 flex-1">
                <div className="text-kumo-subtle text-xs font-medium uppercase tracking-wide">
                  {item.kind === "secure_note"
                    ? "Secure Note"
                    : item.kind === "ssh"
                      ? "SSH Private Key"
                      : item.kind === "card"
                        ? "Card Details"
                        : item.kind === "identity"
                          ? "Identity Details"
                          : "Password / Secret"}
                </div>
                {!passwordVisible || !secret ? (
                  <div className="mt-0.5 font-mono text-sm tracking-widest opacity-70">
                    {loadingSecret ? "Unlocking…" : "••••••••••••••••"}
                  </div>
                ) : secret.password &&
                  !secret.password.includes("\n") &&
                  item.kind !== "secure_note" &&
                  item.kind !== "ssh" ? (
                  <div className="mt-0.5 break-all font-mono text-sm">
                    {secret.password}
                  </div>
                ) : null}
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Button
                  type="button"
                  variant="secondary"
                  disabled={loadingSecret}
                  onClick={() => {
                    if (passwordVisible && secret) {
                      setPasswordVisible(false);
                      return;
                    }
                    void ensureRevealed().then((data) => {
                      if (data) {
                        setPasswordVisible(true);
                      }
                    });
                  }}
                >
                  {passwordVisible && secret ? "Hide" : "Reveal"}
                </Button>
                <Button
                  type="button"
                  variant="primary"
                  disabled={loadingSecret}
                  title="Copy password (⌘C)"
                  onClick={() => {
                    void ensureRevealed().then((data) => {
                      if (data?.password) {
                        copy(
                          "password",
                          data.password,
                          `Copied password for ${item.name}`,
                        );
                      }
                    });
                  }}
                >
                  {copied === "password" ? "Copied" : "Copy ⌘C"}
                </Button>
              </div>
            </div>
            {passwordVisible &&
            secret?.password &&
            (secret.password.includes("\n") ||
              item.kind === "secure_note" ||
              item.kind === "ssh") ? (
              <Textarea
                className="w-full font-mono text-xs"
                rows={7}
                readOnly
                value={secret.password}
              />
            ) : null}
          </div>
        ) : null}

        {item.has_totp || totp ? (
          <div className="flex items-center justify-between gap-4 px-4 py-3">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="text-kumo-subtle text-xs font-medium uppercase tracking-wide">
                  One-Time Password (2FA)
                </span>
                <Badge variant="neutral">{totpRemaining}s</Badge>
              </div>
              <div className="mt-0.5 font-mono text-lg font-semibold tracking-widest">
                {formattedTotp ?? "Loading…"}
              </div>
            </div>
            <Button
              type="button"
              variant="primary"
              title="Copy 6-digit 2FA code (⌘T)"
              onClick={() => {
                if (totp) {
                  copy("totp", totp, `Copied 2FA code for ${item.name}: ${totp}`);
                  return;
                }
                void mintVaultTOTP(item.id).then((res) => {
                  if (res.totp) {
                    setTotp(res.totp);
                    copy(
                      "totp",
                      res.totp,
                      `Copied 2FA code for ${item.name}: ${res.totp}`,
                    );
                  }
                });
              }}
            >
              {copied === "totp" ? "Copied" : "Copy 2FA ⌘T"}
            </Button>
          </div>
        ) : canEnrollTotp ? (
          <div className="flex flex-col gap-2 px-4 py-3">
            {!enrollingTotp ? (
              <div className="flex items-center justify-between gap-4">
                <div>
                  <div className="text-kumo-subtle text-xs font-medium uppercase tracking-wide">
                    One-Time Password (2FA)
                  </div>
                  <div className="text-kumo-subtle mt-0.5 text-xs opacity-75">
                    No authenticator attached
                  </div>
                </div>
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => setEnrollingTotp(true)}
                >
                  + Add 2FA
                </Button>
              </div>
            ) : (
              <div className="flex flex-col gap-2">
                <Label htmlFor="enroll-totp-input">
                  Authenticator secret (base32 seed or otpauth:// URI)
                </Label>
                <div className="flex items-center gap-2">
                  <Input
                    id="enroll-totp-input"
                    className="w-full font-mono text-xs"
                    placeholder="JBSWY3DPEHPK3PXP or otpauth://totp/…"
                    value={totpSeedInput}
                    onChange={(e) => setTotpSeedInput(e.currentTarget.value)}
                  />
                  <Button
                    type="button"
                    variant="primary"
                    disabled={totpBusy || !totpSeedInput.trim()}
                    onClick={() => {
                      setTotpBusy(true);
                      void enrollVaultTOTP(item.id, totpSeedInput).then(
                        (res) => {
                          setTotpBusy(false);
                          if (res.error) {
                            onError(res.error);
                            return;
                          }
                          setEnrollingTotp(false);
                          setTotpSeedInput("");
                          onNotice(`2FA enabled for ${item.name}`);
                          onReload();
                          void mintVaultTOTP(item.id).then((tRes) => {
                            if (tRes.totp) {
                              setTotp(tRes.totp);
                            }
                          });
                        },
                      );
                    }}
                  >
                    {totpBusy ? "Saving…" : "Save 2FA"}
                  </Button>
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={() => {
                      setEnrollingTotp(false);
                      setTotpSeedInput("");
                    }}
                  >
                    Cancel
                  </Button>
                </div>
              </div>
            )}
          </div>
        ) : null}
      </div>

      {passwordVisible && secret?.card ? (
        <div className="bg-kumo-elevated/40 ring-kumo-hairline flex flex-col divide-y divide-neutral-800/60 rounded-lg ring">
          {secret.card.holder ? (
            <div className="flex items-center justify-between gap-4 px-4 py-3">
              <div>
                <div className="text-kumo-subtle text-xs font-medium uppercase tracking-wide">
                  Cardholder
                </div>
                <div className="mt-0.5 text-sm">{secret.card.holder}</div>
              </div>
              <Button
                type="button"
                variant="secondary"
                onClick={() => copy("holder", secret.card!.holder!)}
              >
                {copied === "holder" ? "Copied" : "Copy"}
              </Button>
            </div>
          ) : null}
          {secret.card.number ? (
            <div className="flex items-center justify-between gap-4 px-4 py-3">
              <div>
                <div className="text-kumo-subtle text-xs font-medium uppercase tracking-wide">
                  Card Number
                </div>
                <div className="mt-0.5 font-mono text-sm">
                  {secret.card.number}
                </div>
              </div>
              <Button
                type="button"
                variant="primary"
                onClick={() => copy("number", secret.card!.number!)}
              >
                {copied === "number" ? "Copied" : "Copy"}
              </Button>
            </div>
          ) : null}
          {secret.card.exp_month || secret.card.exp_year ? (
            <div className="flex items-center justify-between gap-4 px-4 py-3">
              <div>
                <div className="text-kumo-subtle text-xs font-medium uppercase tracking-wide">
                  Expires
                </div>
                <div className="mt-0.5 font-mono text-sm">
                  {secret.card.exp_month ?? ""}/{secret.card.exp_year ?? ""}
                </div>
              </div>
            </div>
          ) : null}
          {secret.card.cvv ? (
            <div className="flex items-center justify-between gap-4 px-4 py-3">
              <div>
                <div className="text-kumo-subtle text-xs font-medium uppercase tracking-wide">
                  CVV
                </div>
                <div className="mt-0.5 font-mono text-sm">{secret.card.cvv}</div>
              </div>
              <Button
                type="button"
                variant="secondary"
                onClick={() => copy("cvv", secret.card!.cvv!)}
              >
                {copied === "cvv" ? "Copied" : "Copy"}
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}

      {passwordVisible && secret?.identity ? (
        <div className="bg-kumo-elevated/40 ring-kumo-hairline flex flex-col divide-y divide-neutral-800/60 rounded-lg p-4 text-sm ring">
          {secret.identity.given_name || secret.identity.family_name ? (
            <div className="flex items-center justify-between py-1.5">
              <Text variant="secondary">Name</Text>
              <Text>
                {[secret.identity.given_name, secret.identity.family_name]
                  .filter(Boolean)
                  .join(" ")}
              </Text>
            </div>
          ) : null}
          {secret.identity.email ? (
            <div className="flex items-center justify-between py-1.5">
              <Text variant="secondary">Email</Text>
              <Text>{secret.identity.email}</Text>
            </div>
          ) : null}
          {secret.identity.phone ? (
            <div className="flex items-center justify-between py-1.5">
              <Text variant="secondary">Phone</Text>
              <Text>{secret.identity.phone}</Text>
            </div>
          ) : null}
          {secret.identity.address ? (
            <div className="flex items-center justify-between py-1.5">
              <Text variant="secondary">Address</Text>
              <Text>
                {[
                  secret.identity.address,
                  secret.identity.city,
                  secret.identity.region,
                  secret.identity.postal,
                  secret.identity.country,
                ]
                  .filter(Boolean)
                  .join(", ")}
              </Text>
            </div>
          ) : null}
        </div>
      ) : null}

      {uris.length > 0 ? (
        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <Label>Websites / URIs ({uris.length})</Label>
            {uris.length > 5 ? (
              <button
                type="button"
                className="cursor-pointer text-xs underline opacity-75 hover:opacity-100"
                onClick={() => setShowAllURIs((v) => !v)}
              >
                {showAllURIs ? "Show fewer" : `Show all ${uris.length} URIs`}
              </button>
            ) : null}
          </div>
          <div className="bg-kumo-elevated/40 ring-kumo-hairline flex flex-col divide-y divide-neutral-800/60 rounded-lg ring">
            {visibleURIs.map((u) => {
              const isWeb =
                u.toLowerCase().startsWith("http://") ||
                u.toLowerCase().startsWith("https://");
              return (
                <div
                  key={u}
                  className="flex items-center justify-between gap-3 px-4 py-2.5"
                >
                  <span
                    className="min-w-0 flex-1 truncate font-mono text-xs"
                    title={u}
                  >
                    {u}
                  </span>
                  <div className="flex shrink-0 items-center gap-2">
                    {isWeb ? (
                      <Button
                        type="button"
                        variant="secondary"
                        onClick={() =>
                          window.open(u, "_blank", "noopener,noreferrer")
                        }
                      >
                        Open
                      </Button>
                    ) : null}
                    <Button
                      type="button"
                      variant="secondary"
                      onClick={() => copy(u, u, `Copied URI`)}
                    >
                      {copied === u ? "Copied" : "Copy"}
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      ) : null}

      {itemGrants.length > 0 ? (
        <div className="flex flex-col gap-2">
          <Label>Granted Access ({itemGrants.length})</Label>
          <div className="bg-kumo-elevated/40 ring-kumo-hairline flex flex-col divide-y divide-neutral-800/60 rounded-lg ring">
            {itemGrants.map((g) => (
              <div
                key={g.id}
                className="flex items-center justify-between gap-3 px-4 py-2.5 text-xs"
              >
                <div className="flex items-center gap-2">
                  <Badge variant="neutral">{g.subject_kind}</Badge>
                  <span className="font-mono font-medium">{g.agent_id}</span>
                </div>
                <Badge variant="neutral">{g.level}</Badge>
              </div>
            ))}
          </div>
        </div>
      ) : null}

      {itemAudit.length > 0 ? (
        <div className="flex flex-col gap-2">
          <Label>Recent Activity</Label>
          <div className="bg-kumo-elevated/40 ring-kumo-hairline flex flex-col divide-y divide-neutral-800/60 rounded-lg ring">
            {itemAudit.map((ev, i) => (
              <div
                key={`${ev.time}-${i}`}
                className="flex items-center justify-between gap-3 px-4 py-2 text-xs"
              >
                <div className="flex items-center gap-2">
                  <Badge variant={ev.decision === "allow" ? "neutral" : "red"}>
                    {ev.action}
                  </Badge>
                  <span className="text-kumo-subtle font-mono">
                    {ev.agent_id}
                  </span>
                </div>
                <span className="text-kumo-subtle" title={ev.time}>
                  {fmtRelativeTime(ev.time)}
                </span>
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}
