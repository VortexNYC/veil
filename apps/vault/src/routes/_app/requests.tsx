import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { Table } from "@cloudflare/kumo/components/table";
import { Text } from "@cloudflare/kumo/components/text";
import { createFileRoute } from "@tanstack/react-router";
import { useCallback, useEffect, useState } from "react";
import { CACHE_KEYS, readCache, writeCache } from "../../cache";
import { PageChrome } from "../../page-chrome";
import {
  approveReq,
  completeRecovery,
  denyRecovery,
  denyReq,
  recoveries,
  requests,
  watchRequests,
} from "../../origin";
import type { ApprovalRequest, RecoveryRequest } from "@vortex-api/veil";

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

function ago(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  const m = Math.floor(ms / 60_000);
  if (m < 1) {
    return "just now";
  }
  return m >= 60 ? `${Math.round(m / 60)}h ago` : `${m}m ago`;
}

function Requests() {
  const [rows, setRows] = useState<ApprovalRequest[] | null>(() =>
    readCache<ApprovalRequest[]>(CACHE_KEYS.requestsOpen),
  );
  const [status, setStatus] = useState<Status>("open");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  // Recovery asks are owner-only — a member's 403 means "no section", not
  // an error. null = hidden, otherwise the open list.
  const [recRows, setRecRows] = useState<RecoveryRequest[] | null>(null);
  const [recBusy, setRecBusy] = useState<string | null>(null);
  const [recError, setRecError] = useState<string | null>(null);

  const reloadRecoveries = useCallback(async () => {
    const res = await recoveries("open");
    if (res.error || !res.data) {
      if (res.response?.status === 403) {
        setRecRows(null);
        return;
      }
      setRecError("recovery list failed");
      setRecRows([]);
      return;
    }
    setRecError(null);
    setRecRows(res.data.requests);
  }, []);

  const reload = useCallback(
    async (s: Status) => {
      const res = await requests(s);
      if (res.error || !res.data) {
        setError("list failed");
        setRows([]);
      } else {
        setError(null);
        if (s === "open") {
          writeCache(CACHE_KEYS.requestsOpen, res.data.requests);
        }
        setRows(res.data.requests);
      }
      await reloadRecoveries();
    },
    [reloadRecoveries],
  );

  useEffect(() => {
    if (status !== "open") {
      setRows(null);
    }
    void reload(status);
    if (status !== "open") {
      return;
    }
    // Open asks are the live surface — the server streams a tick on every
    // committed change (SSE; Postgres NOTIFY fans out across replicas —
    // recovery_requests notify on the same channel), so a fresh agent ask
    // or recovery filing lands instantly. A slow poll stays as the safety
    // net for a dropped stream between reconnects.
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

  function resolveRecovery(id: string, act: "approve" | "deny") {
    setRecBusy(id);
    const call = act === "approve" ? completeRecovery(id) : denyRecovery(id);
    void call.then((res) => {
      setRecBusy(null);
      if (res.error) {
        const code = res.response?.status;
        setRecError(
          code === 409
            ? "already resolved"
            : code === 423
              ? "still inside the timelock — unlock_at has not passed"
              : code === 403
                ? "not permitted — an owner cannot confirm a request they filed or that targets them"
                : `${act} failed`,
        );
      }
      void reloadRecoveries();
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
      {recRows !== null ? (
        <LayerCard>
          <LayerCard.Secondary>Account Recovery</LayerCard.Secondary>
          <LayerCard.Primary>
            {recError ? (
              <Text as="p" variant="error">
                {recError}
              </Text>
            ) : null}
            {recRows.length === 0 ? (
              <Empty
                title="No open recovery requests"
                description="When a member loses every device, an owner confirms their recovery here. Approving rotates the org key and, when flagged, resets their MFA."
              />
            ) : (
              <Table>
                <Table.Header>
                  <Table.Row>
                    <Table.Head>Member</Table.Head>
                    <Table.Head>Filed</Table.Head>
                    <Table.Head>Expires</Table.Head>
                    <Table.Head />
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {recRows.map((r) => {
                    const locked = new Date(r.unlock_at).getTime() > Date.now();
                    return (
                      <Table.Row key={r.id}>
                        <Table.Cell>
                          <div className="flex flex-col">
                            <Text as="span">{r.human_email ?? r.human_id}</Text>
                            <Text as="span" variant="mono-secondary">
                              {r.human_id.slice(0, 8)}
                            </Text>
                            {r.reset_mfa ? (
                              <span>
                                <Badge variant="warning">resets MFA</Badge>
                              </span>
                            ) : null}
                          </div>
                        </Table.Cell>
                        <Table.Cell>
                          <Text as="span" variant="mono-secondary">
                            {ago(r.created_at)}
                          </Text>
                          {locked ? (
                            <Text as="span" variant="secondary" size="sm">
                              {" "}
                              · unlocks in {until(r.unlock_at)}
                            </Text>
                          ) : null}
                        </Table.Cell>
                        <Table.Cell>
                          <Text as="span" variant="mono-secondary">
                            {until(r.expires_at)}
                          </Text>
                        </Table.Cell>
                        <Table.Cell>
                          <div className="flex gap-2">
                            <Button
                              type="button"
                              variant="primary"
                              size="sm"
                              disabled={recBusy === r.id || locked}
                              onClick={() => resolveRecovery(r.id, "approve")}
                            >
                              Approve Recovery
                            </Button>
                            <Button
                              type="button"
                              variant="secondary"
                              size="sm"
                              disabled={recBusy === r.id}
                              onClick={() => resolveRecovery(r.id, "deny")}
                            >
                              Deny
                            </Button>
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    );
                  })}
                </Table.Body>
              </Table>
            )}
          </LayerCard.Primary>
        </LayerCard>
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
