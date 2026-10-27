import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Table } from "@cloudflare/kumo/components/table";
import { Text } from "@cloudflare/kumo/components/text";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { CACHE_KEYS, readCache, writeCache } from "../../cache";
import { PageChrome } from "../../page-chrome";
import { revokeShareLink, shares } from "../../origin";
import type { Share } from "@vortex-api/veil";

export const Route = createFileRoute("/_app/shares")({
  component: Shares,
});

// One-time link lifecycle: active until redeemed, revoked, or past expiry —
// the three terminal states the origin records as columns.
type ShareState = "active" | "redeemed" | "revoked" | "expired";

function shareState(s: Share, now: number): ShareState {
  if (s.revoked_at) {
    return "revoked";
  }
  if (s.consumed_at) {
    return "redeemed";
  }
  const exp = Date.parse(s.expires_at);
  if (Number.isFinite(exp) && exp <= now) {
    return "expired";
  }
  return "active";
}

function fmtWhen(iso: string | undefined): string {
  if (!iso) {
    return "—";
  }
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) {
    return "—";
  }
  return new Date(t).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function ttlLeft(iso: string, now: number): string {
  const ms = Date.parse(iso) - now;
  if (!Number.isFinite(ms) || ms <= 0) {
    return "—";
  }
  const mins = Math.floor(ms / 60_000);
  const days = Math.floor(mins / 1440);
  const hours = Math.floor((mins % 1440) / 60);
  if (days > 0) {
    return `${days}d ${hours}h`;
  }
  if (hours > 0) {
    return `${hours}h ${mins % 60}m`;
  }
  return `${Math.max(mins, 1)}m`;
}

function Shares() {
  const [rows, setRows] = useState<Share[] | null>(() =>
    readCache<Share[]>(CACHE_KEYS.shares),
  );
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  async function reload() {
    const res = await shares();
    if (res.error || !res.data) {
      setError("list failed");
      if (rows === null) setRows([]);
      return;
    }
    setError(null);
    writeCache(CACHE_KEYS.shares, res.data.shares);
    setRows(res.data.shares);
  }

  useEffect(() => {
    void reload();
  }, []);

  const now = Date.now();
  const active = rows?.filter((s) => shareState(s, now) === "active") ?? [];

  return (
    <PageChrome
      title="Shares"
      subtitle="One-time sealed links. The key lives in the URL fragment — revoking kills the link instantly."
    >
      {error ? (
        <Text as="p" variant="error">
          {error}
        </Text>
      ) : null}
      <LayerCard>
        <LayerCard.Primary>
          {rows === null ? (
            <Text variant="secondary">Loading…</Text>
          ) : rows.length === 0 ? (
            <Empty
              title="No shares"
              description="Share an item from the Items page — the sealed link opens exactly once."
            />
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>Item</Table.Head>
                  <Table.Head>Created</Table.Head>
                  <Table.Head>Status</Table.Head>
                  <Table.Head>Time left</Table.Head>
                  <Table.Head></Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {rows.map((s) => {
                  const state = shareState(s, now);
                  return (
                    <Table.Row key={s.id}>
                      <Table.Cell>{s.name ?? s.id}</Table.Cell>
                      <Table.Cell>{fmtWhen(s.created_at)}</Table.Cell>
                      <Table.Cell>
                        {state === "active" ? (
                          <Badge variant="neutral">Active</Badge>
                        ) : state === "redeemed" ? (
                          <span className="text-xs opacity-75">
                            Redeemed {fmtWhen(s.consumed_at)}
                          </span>
                        ) : state === "revoked" ? (
                          <span className="text-xs opacity-75">
                            Revoked {fmtWhen(s.revoked_at)}
                          </span>
                        ) : (
                          <Badge variant="neutral">Expired</Badge>
                        )}
                      </Table.Cell>
                      <Table.Cell>
                        {state === "active" ? ttlLeft(s.expires_at, now) : "—"}
                      </Table.Cell>
                      <Table.Cell>
                        {state === "active" ? (
                          <Button
                            type="button"
                            variant="secondary"
                            disabled={busy === s.id}
                            onClick={() => {
                              setBusy(s.id);
                              void revokeShareLink(s.id).then((res) => {
                                setBusy(null);
                                if (res.error) {
                                  setError(`revoke failed for ${s.name ?? s.id}`);
                                  return;
                                }
                                void reload();
                              });
                            }}
                          >
                            {busy === s.id ? "Revoking…" : "Revoke"}
                          </Button>
                        ) : null}
                      </Table.Cell>
                    </Table.Row>
                  );
                })}
              </Table.Body>
            </Table>
          )}
        </LayerCard.Primary>
      </LayerCard>
      {active.length > 0 ? (
        <Text variant="secondary" size="sm">
          {active.length} active {active.length === 1 ? "link" : "links"} — each
          opens exactly once, then dies.
        </Text>
      ) : null}
    </PageChrome>
  );
}
