import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Input } from "@cloudflare/kumo/components/input";
import { Label } from "@cloudflare/kumo/components/label";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { Table } from "@cloudflare/kumo/components/table";
import { Text } from "@cloudflare/kumo/components/text";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { PageChrome } from "../../page-chrome";
import {
  addItem,
  importItems,
  items,
  rotateProviders,
  rotateVaultItem,
  verifyVaultItem,
} from "../../origin";
import type { Item } from "@vortex-api/veil";

export const Route = createFileRoute("/_app/items")({
  component: Items,
});

type Kind = "api_key" | "wif" | "card" | "identity";

function isKind(value: string): value is Kind {
  return value === "api_key" || value === "wif" || value === "card" || value === "identity";
}

function Items() {
  const [rows, setRows] = useState<Item[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [rotateTarget, setRotateTarget] = useState<string | null>(null);
  const [zeroTouchHosts, setZeroTouchHosts] = useState<string[]>([]);
  const [kind, setKind] = useState<Kind>("api_key");
  const fileRef = useRef<HTMLInputElement>(null);

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
                        : {
                            name,
                            kind,
                            uri: String(fd.get("uri") ?? "") || undefined,
                            login: String(fd.get("login") ?? "") || undefined,
                            secret: String(fd.get("secret") ?? "") || undefined,
                          };
                  void addItem(body).then((res) => {
                    if (res.error) {
                      setError("create failed");
                      return;
                    }
                    setOpen(false);
                    setKind("api_key");
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
                      : value === "wif"
                        ? "Outbound WIF (Anthropic / RFC 7523)"
                        : value === "card"
                          ? "Card"
                          : "Identity"
                  }
                  onValueChange={(value) => {
                    if (typeof value === "string" && isKind(value)) {
                      setKind(value);
                    }
                  }}
                >
                  <Select.Option value="api_key">Password / API key</Select.Option>
                  <Select.Option value="wif">Outbound WIF (Anthropic / RFC 7523)</Select.Option>
                  <Select.Option value="card">Card</Select.Option>
                  <Select.Option value="identity">Identity</Select.Option>
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
                      <Input
                        id="secret"
                        name="secret"
                        type="password"
                        autoComplete="new-password"
                      />
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
      <LayerCard>
        <LayerCard.Primary>
          {rows === null ? (
            <Text variant="secondary">Loading…</Text>
          ) : rows.length === 0 ? (
            <Empty
              title="No items"
              description="Add a password, card, or identity. Import a 1Password export. Chrome fill reads it; this page does not."
            />
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>Name</Table.Head>
                  <Table.Head>Kind</Table.Head>
                  <Table.Head>URIs</Table.Head>
                  <Table.Head>Rotate</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {rows.map((item) => (
                  <Table.Row key={item.id}>
                    <Table.Cell>{item.name}</Table.Cell>
                    <Table.Cell>{item.has_totp ? "TOTP" : item.kind}</Table.Cell>
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
                        {item.kind === "api_key" ? (
                          <Button
                            type="button"
                            variant="secondary"
                            onClick={() => setRotateTarget(item.name)}
                          >
                            Rotate
                          </Button>
                        ) : null}
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
