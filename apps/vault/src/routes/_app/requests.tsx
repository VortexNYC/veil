import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { Table } from "@cloudflare/kumo/components/table";
import { Text } from "@cloudflare/kumo/components/text";
import { createFileRoute } from "@tanstack/react-router";
import { useCallback, useEffect, useState } from "react";
import { PageChrome } from "../../page-chrome";
import { approveReq, denyReq, requests, watchRequests } from "../../origin";
import type { ApprovalRequest } from "@vortex-api/veil";

export const Route = createFileRoute("/_app/requests")({
  component: Requests,
});

const statuses = ["open", "approved", "denied", "expired", "cancelled"] as const;
type Status = (typeof statuses)[number];

function isStatus(value: string): value is Status {
  return (statuses as readonly string[]).includes(value);
}

function until(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now();
  if (ms <= 0) {
    return "expired";
  }
  const m = Math.ceil(ms / 60_000);
  return m >= 60 ? `${Math.round(m / 60)}h` : `${m}m`;
}

function Requests() {
  const [rows, setRows] = useState<ApprovalRequest[] | null>(null);
  const [status, setStatus] = useState<Status>("open");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const reload = useCallback(async (s: Status) => {
    const res = await requests(s);
    if (res.error || !res.data) {
      setError("list failed");
      setRows([]);
      return;
    }
    setError(null);
    setRows(res.data.requests);
  }, []);

  useEffect(() => {
    setRows(null);
    void reload(status);
    if (status !== "open") {
      return;
    }
    // Open asks are the live surface — the server streams a tick on every
    // committed change (SSE; Postgres NOTIFY fans out across replicas), so a
    // fresh agent ask lands instantly. A slow poll stays as the safety net
    // for a dropped stream between reconnects.
    const stop = watchRequests(() => void reload("open"));
    const t = setInterval(() => void reload("open"), 30_000);
    return () => {
      stop();
      clearInterval(t);
    };
  }, [status, reload]);

  function resolve(id: string, act: "approve" | "deny") {
    setBusy(id);
    const call = act === "approve" ? approveReq(id) : denyReq(id);
    void call.then((res) => {
      setBusy(null);
      if (res.error) {
        // 409 — another owner or the TTL won first. Reload tells the truth.
        setError(res.response?.status === 409 ? "already resolved" : `${act} failed`);
      }
      void reload(status);
    });
  }

  return (
    <PageChrome
      title="Requests"
      subtitle="Level-1 asks. First write wins — approve or deny."
      actions={
        <Select
          value={status}
          label="Status"
          hideLabel
          onValueChange={(value) => {
            if (typeof value === "string" && isStatus(value)) {
              setStatus(value);
            }
          }}
        >
          {statuses.map((s) => (
            <Select.Option key={s} value={s}>
              {s}
            </Select.Option>
          ))}
        </Select>
      }
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
              title={status === "open" ? "Nothing waiting on you" : `No ${status} requests`}
              description={
                status === "open"
                  ? "Level-1 agents file asks here when they need a credential."
                  : undefined
              }
            />
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>Agent</Table.Head>
                  <Table.Head>Item</Table.Head>
                  <Table.Head>Action</Table.Head>
                  <Table.Head>Expires</Table.Head>
                  {status === "open" ? <Table.Head /> : <Table.Head>Resolved</Table.Head>}
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {rows.map((r) => (
                  <Table.Row key={r.id}>
                    <Table.Cell>{r.agent_id}</Table.Cell>
                    <Table.Cell>{r.item_id}</Table.Cell>
                    <Table.Cell>{r.action}</Table.Cell>
                    <Table.Cell>
                      <Text as="span" variant="mono-secondary">
                        {until(r.expires_at)}
                      </Text>
                    </Table.Cell>
                    <Table.Cell>
                      {status === "open" ? (
                        <div className="flex gap-2">
                          <Button
                            type="button"
                            variant="primary"
                            size="sm"
                            disabled={busy === r.id}
                            onClick={() => resolve(r.id, "approve")}
                          >
                            Approve
                          </Button>
                          <Button
                            type="button"
                            variant="secondary"
                            size="sm"
                            disabled={busy === r.id}
                            onClick={() => resolve(r.id, "deny")}
                          >
                            Deny
                          </Button>
                        </div>
                      ) : (
                        <Text as="span" variant="secondary" size="sm">
                          {r.resolved_at ?? "—"}
                        </Text>
                      )}
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
