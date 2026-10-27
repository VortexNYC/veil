import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Table } from "@cloudflare/kumo/components/table";
import { Text } from "@cloudflare/kumo/components/text";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import {
  CACHE_KEYS,
  fmtRelativeTime,
  readCache,
  writeCache,
} from "../../cache";
import { PageChrome } from "../../page-chrome";
import { events } from "../../origin";
import type { AuditEvent } from "@vortex-api/veil";

export const Route = createFileRoute("/_app/audit")({
  component: Audit,
});

function Audit() {
  const [rows, setRows] = useState<AuditEvent[] | null>(() =>
    readCache<AuditEvent[]>(CACHE_KEYS.audit),
  );
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [decisionFilter, setDecisionFilter] = useState<"all" | "allow" | "deny">(
    "all",
  );

  function reload() {
    void events().then((res) => {
      if (res.error || !res.data) {
        setError("list failed");
        if (rows === null) setRows([]);
        return;
      }
      setError(null);
      const rev = [...res.data.events].reverse();
      writeCache(CACHE_KEYS.audit, rev);
      setRows(rev);
    });
  }

  useEffect(() => {
    reload();
  }, []);

  const filtered = (rows ?? []).filter((e) => {
    if (decisionFilter !== "all" && e.decision !== decisionFilter) {
      return false;
    }
    if (!query.trim()) {
      return true;
    }
    const hay = `${e.decision} ${e.action} ${e.item_id ?? ""} ${e.agent_id ?? ""} ${e.reason ?? ""}`.toLowerCase();
    return query
      .trim()
      .toLowerCase()
      .split(/\s+/)
      .every((t) => hay.includes(t));
  });

  return (
    <PageChrome
      title="Audit"
      subtitle="Every credential disclosure, agent Use, and administration event. Never secrets."
      actions={
        <Button type="button" variant="secondary" onClick={reload}>
          Refresh
        </Button>
      }
    >
      {error ? (
        <Text as="p" variant="error">
          {error}
        </Text>
      ) : null}
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex-1">
          <Input
            className="w-full"
            aria-label="Filter audit events"
            placeholder="Search by item, principal, action (reveal, fill, use), or reason…"
            value={query}
            onChange={(e) => setQuery(e.currentTarget.value)}
          />
        </div>
        <div className="flex items-center gap-1.5">
          {(["all", "allow", "deny"] as const).map((d) => (
            <button
              key={d}
              type="button"
              onClick={() => setDecisionFilter(d)}
              className={`cursor-pointer rounded-full px-3 py-1 text-xs font-medium ${
                decisionFilter === d
                  ? "bg-kumo-tint text-kumo-default ring-kumo-focus/40 ring"
                  : "bg-kumo-elevated/60 text-kumo-subtle"
              }`}
            >
              {d === "all" ? "All decisions" : d}
            </button>
          ))}
        </div>
      </div>
      <LayerCard>
        <LayerCard.Primary>
          {rows === null ? (
            <Text variant="secondary">Loading…</Text>
          ) : filtered.length === 0 ? (
            <Empty
              title="No matching events"
              description="Try clearing the search or decision filter."
            />
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>Event</Table.Head>
                  <Table.Head>Item</Table.Head>
                  <Table.Head>Principal</Table.Head>
                  <Table.Head>When</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {filtered.map((e, i) => (
                  <Table.Row key={`${e.time}-${e.item_id}-${i}`}>
                    <Table.Cell>
                      <div className="flex items-center gap-2">
                        <Badge
                          variant={e.decision === "allow" ? "neutral" : "red"}
                        >
                          {e.decision}
                        </Badge>
                        <span className="font-mono text-sm font-medium">
                          {e.action}
                        </span>
                      </div>
                    </Table.Cell>
                    <Table.Cell>
                      <span className="font-mono text-sm">
                        {e.item_id || "—"}
                      </span>
                    </Table.Cell>
                    <Table.Cell>
                      <span className="font-mono text-xs opacity-80">
                        {e.agent_id || "—"}
                      </span>
                    </Table.Cell>
                    <Table.Cell>
                      <span
                        className="text-kumo-subtle text-xs"
                        title={e.time}
                      >
                        {fmtRelativeTime(e.time)}
                      </span>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          )}
        </LayerCard.Primary>
      </LayerCard>
    </PageChrome>
  );
}
