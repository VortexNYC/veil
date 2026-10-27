import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Input, Textarea } from "@cloudflare/kumo/components/input";
import { Label } from "@cloudflare/kumo/components/label";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { Table } from "@cloudflare/kumo/components/table";
import { Text } from "@cloudflare/kumo/components/text";
import {
  CreditCard,
  File,
  FileText,
  Fingerprint,
  IdentificationCard,
  Key,
  LockKey,
  Terminal,
} from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { PageChrome } from "../../page-chrome";
import {
  addItem,
  archiveVaultItem,
  deleteVaultItem,
  importItems,
  items,
  rotateProviders,
  rotateVaultItem,
  updateVaultItem,
  verifyVaultItem,
} from "../../origin";
import type { Item } from "@vortex-api/veil";

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

type KindFilter = "all" | Item["kind"];

function isKindFilter(value: string): value is KindFilter {
  return (
    value === "all" ||
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
  { value: "all", label: "All kinds" },
  { value: "api_key", label: "Passwords / API keys" },
  { value: "passkey", label: "Passkeys" },
  { value: "card", label: "Cards" },
  { value: "identity", label: "Identities" },
  { value: "ssh", label: "SSH keys" },
  { value: "oauth", label: "OAuth / WIF" },
  { value: "file", label: "Files" },
  { value: "secure_note", label: "Secure notes" },
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
  if (!query) {
    return true;
  }
  const haystack = [
    item.name,
    item.login ?? "",
    ...(item.uris ?? []),
    ...(item.tags ?? []),
  ]
    .join("\n")
    .toLowerCase();
  return haystack.includes(query.toLowerCase());
}

function Items() {
  const [rows, setRows] = useState<Item[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [rotateTarget, setRotateTarget] = useState<string | null>(null);
  const [editTarget, setEditTarget] = useState<Item | null>(null);
  const [zeroTouchHosts, setZeroTouchHosts] = useState<string[]>([]);
  const [kind, setKind] = useState<Kind>("api_key");
  const [query, setQuery] = useState("");
  const [kindFilter, setKindFilter] = useState<KindFilter>("all");
  const [iconsOn, setIconsOn] = useState(false);
  const [addSecret, setAddSecret] = useState("");
  const [addSecretVisible, setAddSecretVisible] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  const filtered = (rows ?? []).filter(
    (item) =>
      (kindFilter === "all" || item.kind === kindFilter) &&
      itemMatches(item, query),
  );

  async function reload() {
    const res = await items();
    if (res.error || !res.data) {
      setError("list failed");
      setRows([]);
      return;
    }
    setError(null);
    setRows(res.data.items);
    const pres = await rotateProviders();
    if (pres.data) {
      setZeroTouchHosts(pres.data.zero_touch_hosts);
    }
  }

  useEffect(() => {
    void reload();
  }, []);

  return (
    <PageChrome
      title="Items"
      subtitle="Secrets go in. They do not come back out. Fill is the read path."
      actions={
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
                  const body =
                    kind === "wif"
                      ? {
                          name,
                          kind: "oauth" as const,
                          uri: String(fd.get("uri") ?? "").trim() || "https://api.anthropic.com",
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
                            uri: String(fd.get("uri") ?? "").trim() || undefined,
                            secret: String(fd.get("secret") ?? "") || undefined,
                            ...(tags.length > 0 ? { tags } : {}),
                          }
                        : kind === "secure_note"
                          ? {
                              name,
                              kind,
                              secret: String(fd.get("secret") ?? "") || undefined,
                              ...(tags.length > 0 ? { tags } : {}),
                            }
                          : {
                            name,
                            kind,
                            uri: String(fd.get("uri") ?? "") || undefined,
                            login: String(fd.get("login") ?? "") || undefined,
                            secret: String(fd.get("secret") ?? "") || undefined,
                            totp_seed:
                              String(fd.get("totp_seed") ?? "").trim() || undefined,
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
                  The secret is sent once. This page will never show it.
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
                <div className="flex justify-end">
                  <Button type="submit" variant="primary">
                    Save
                  </Button>
                </div>
              </form>
            </Dialog>
          </Dialog.Root>
        </div>
      }
    >
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
      <div className="flex items-center gap-3">
        <div className="flex-1">
          <Input
            aria-label="Search items"
            placeholder="Search by name, login, URI, or tag…"
            value={query}
            onChange={(e) => setQuery(e.currentTarget.value)}
          />
        </div>
        <Select
          aria-label="Filter by kind"
          value={kindFilter}
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
          onClick={() => setIconsOn((v) => !v)}
        >
          Icons
        </Button>
      </div>
      {rows !== null ? (
        <Text as="p" variant="secondary" size="sm">
          Showing {filtered.length} of {rows.length} items
        </Text>
      ) : null}
      <LayerCard>
        <LayerCard.Primary>
          {rows === null ? (
            <Text variant="secondary">Loading…</Text>
          ) : rows.length === 0 ? (
            <Empty
              title="No items"
              description="Add a password, card, or identity. Import a 1Password export. Chrome fill reads it; this page does not."
            />
          ) : filtered.length === 0 ? (
            <Empty
              title="No matches"
              description="Nothing matches that search or filter."
            />
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>Name</Table.Head>
                  <Table.Head>Login</Table.Head>
                  <Table.Head>Kind</Table.Head>
                  <Table.Head>URIs</Table.Head>
                  <Table.Head>Actions</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {filtered.map((item) => (
                  <Table.Row key={item.id}>
                    <Table.Cell>
                      <div className="flex flex-wrap items-center gap-1.5">
                        <ItemIcon item={item} enabled={iconsOn} />
                        <Text as="span">{item.name}</Text>
                        {(item.tags ?? []).map((tag) => (
                          <Badge key={tag} variant="neutral">
                            {tag}
                          </Badge>
                        ))}
                      </div>
                    </Table.Cell>
                    <Table.Cell>
                      <Text as="span" variant="secondary" size="sm">
                        {item.login || "—"}
                      </Text>
                    </Table.Cell>
                    <Table.Cell>
                      {item.kind}
                      {item.has_totp ? " · 2FA" : ""}
                    </Table.Cell>
                    <Table.Cell>
                      <Text as="span" variant="secondary" size="sm">
                        {(item.uris ?? []).join(" · ") || "—"}
                      </Text>
                    </Table.Cell>
                    <Table.Cell>
                      <div className="flex items-center gap-2">
                        <Button
                          type="button"
                          variant="secondary"
                          onClick={() => setEditTarget(item)}
                        >
                          Edit
                        </Button>
                        {item.kind === "api_key" ? (
                          <>
                            <Button
                              type="button"
                              variant="secondary"
                              onClick={() => {
                                void verifyVaultItem(item.name).then((res) => {
                                  if (res.error || !res.data) {
                                    setError(`verify failed for ${item.name}`);
                                    return;
                                  }
                                  setError(null);
                                  setNotice(`Verified ${item.name} (${res.data.mode})`);
                                });
                              }}
                            >
                              Verify
                            </Button>
                            <Button
                              type="button"
                              variant="secondary"
                              onClick={() => setRotateTarget(item.name)}
                            >
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
                                setError(`archive failed for ${item.name}`);
                                return;
                              }
                              setError(null);
                              setNotice(`Archived ${item.name}`);
                              void reload();
                            });
                          }}
                        >
                          Archive
                        </Button>
                        <Button
                          type="button"
                          variant="secondary"
                          onClick={() => {
                            void deleteVaultItem(item.id).then((res) => {
                              if (res.error) {
                                setError(`delete failed for ${item.name}`);
                                return;
                              }
                              setError(null);
                              setNotice(`Deleted ${item.name}`);
                              void reload();
                            });
                          }}
                        >
                          Delete
                        </Button>
                      </div>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          )}
        </LayerCard.Primary>
      </LayerCard>
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
    </PageChrome>
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
