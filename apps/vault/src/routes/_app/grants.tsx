import { Badge } from "@cloudflare/kumo/components/badge";
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
import { useEffect, useState } from "react";
import { CACHE_KEYS, readCache, writeCache } from "../../cache";
import { PageChrome } from "../../page-chrome";
import { formatGrantLevel, formatSubjectKind } from "../../ui";
import { addGrant, agents, grants, groups, items } from "../../origin";
import type { Agent, Grant, Group, Item } from "@vortex-api/veil";

export const Route = createFileRoute("/_app/grants")({
  component: Grants,
});

type Level = "level1" | "level2";

function isLevel(value: string): value is Level {
  return value === "level1" || value === "level2";
}

type TargetKind = "agent" | "group" | "human";

function isTargetKind(value: string): value is TargetKind {
  return value === "agent" || value === "group" || value === "human";
}

const EXPIRES_OPTIONS: { value: string; label: string }[] = [
  { value: "", label: "Forever" },
  { value: "1h", label: "1 hour" },
  { value: "24h", label: "24 hours" },
  { value: "168h", label: "7 days" },
  { value: "720h", label: "30 days" },
];

function Grants() {
  const [rows, setRows] = useState<Grant[] | null>(() =>
    readCache<Grant[]>(CACHE_KEYS.grants),
  );
  const [allItems, setAllItems] = useState<Item[]>(
    () => readCache<Item[]>(CACHE_KEYS.items) ?? [],
  );
  const [allAgents, setAllAgents] = useState<Agent[]>(
    () => readCache<Agent[]>(CACHE_KEYS.agents) ?? [],
  );
  const [allGroups, setAllGroups] = useState<Group[]>(
    () => readCache<Group[]>(CACHE_KEYS.groups) ?? [],
  );
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [level, setLevel] = useState<Level>("level2");
  const [item, setItem] = useState("");
  const [targetKind, setTargetKind] = useState<TargetKind>("agent");
  const [target, setTarget] = useState("");
  const [expires, setExpires] = useState("");

  const itemNameById: Record<string, string> = {};
  for (const it of allItems) {
    itemNameById[it.id] = it.name;
  }

  async function reload() {
    const res = await grants();
    if (res.error || !res.data) {
      setError("list failed");
      if (rows === null) setRows([]);
      return;
    }
    setError(null);
    writeCache(CACHE_KEYS.grants, res.data.grants);
    setRows(res.data.grants);
    const [ires, ares, gres] = await Promise.all([items(), agents(), groups()]);
    if (ires.data) {
      writeCache(CACHE_KEYS.items, ires.data.items);
      setAllItems(ires.data.items);
    }
    if (ares.data) {
      writeCache(CACHE_KEYS.agents, ares.data.agents);
      setAllAgents(ares.data.agents);
    }
    if (gres.data) {
      writeCache(CACHE_KEYS.groups, gres.data.groups);
      setAllGroups(gres.data.groups);
    }
  }

  useEffect(() => {
    void reload();
  }, []);

  const liveAgents = allAgents.filter((a) => !a.revoked_at);

  return (
    <PageChrome
      title="Grants"
      subtitle="Same grant object. Agent or Kratos human id. Not a family vault."
      actions={
        <>
          <Button type="button" variant="primary" onClick={() => setOpen(true)}>
            Grant
          </Button>
          <Dialog.Root
            open={open}
            onOpenChange={(next) => {
              setOpen(next);
              if (!next) {
                setLevel("level2");
                setItem("");
                setTargetKind("agent");
                setTarget("");
                setExpires("");
              }
            }}
          >
            <Dialog size="lg" className="w-[32rem] max-w-[92vw] p-6">
              <form
                className="flex flex-col gap-4"
                onSubmit={(e) => {
                  e.preventDefault();
                  void addGrant({
                    item,
                    level,
                    agent: targetKind === "agent" && target ? target : undefined,
                    human: targetKind === "human" && target ? target : undefined,
                    group: targetKind === "group" && target ? target : undefined,
                    expires: expires || undefined,
                  }).then((res) => {
                    if (res.error) {
                      setError("create failed");
                      return;
                    }
                    setOpen(false);
                    setLevel("level2");
                    setItem("");
                    setTargetKind("agent");
                    setTarget("");
                    setExpires("");
                    void reload();
                  });
                }}
              >
                <Dialog.Title>Grant Use</Dialog.Title>
                <Dialog.Description>XOR agent name, human identity id, or group name.</Dialog.Description>
                <Select
                  value={item}
                  label="Item"
                  hideLabel={false}
                  placeholder="Pick an item"
                  renderValue={(value) => value}
                  onValueChange={(value) => {
                    if (typeof value === "string") {
                      setItem(value);
                    }
                  }}
                >
                  {allItems.map((it) => (
                    <Select.Option key={it.id} value={it.name}>
                      {it.name}
                    </Select.Option>
                  ))}
                </Select>
                <Select
                  value={targetKind}
                  label="Target kind"
                  hideLabel={false}
                  renderValue={(value) => formatSubjectKind(String(value))}
                  onValueChange={(value) => {
                    if (typeof value === "string" && isTargetKind(value)) {
                      setTargetKind(value);
                      setTarget("");
                    }
                  }}
                >
                  <Select.Option value="agent">Agent</Select.Option>
                  <Select.Option value="group">Group</Select.Option>
                  <Select.Option value="human">Human</Select.Option>
                </Select>
                {targetKind === "agent" ? (
                  <Select
                    value={target}
                    label="Agent"
                    hideLabel={false}
                    placeholder="Pick an agent"
                    renderValue={(value) => value}
                    onValueChange={(value) => {
                      if (typeof value === "string") {
                        setTarget(value);
                      }
                    }}
                  >
                    {liveAgents.map((a) => (
                      <Select.Option key={a.id} value={a.id}>
                        {a.id}
                      </Select.Option>
                    ))}
                  </Select>
                ) : null}
                {targetKind === "group" ? (
                  <Select
                    value={target}
                    label="Group"
                    hideLabel={false}
                    placeholder="Pick a group"
                    renderValue={(value) => value}
                    onValueChange={(value) => {
                      if (typeof value === "string") {
                        setTarget(value);
                      }
                    }}
                  >
                    {allGroups.map((g) => (
                      <Select.Option key={g.name} value={g.name}>
                        {g.name}
                      </Select.Option>
                    ))}
                  </Select>
                ) : null}
                {targetKind === "human" ? (
                  <div className="flex flex-col gap-1">
                    <Label htmlFor="human">Kratos human identity ID</Label>
                    <Input
                      id="human"
                      name="human"
                      required
                      value={target}
                      onChange={(e) => setTarget(e.currentTarget.value)}
                    />
                  </div>
                ) : null}
                <Select
                  value={level}
                  label="Approval policy"
                  hideLabel={false}
                  renderValue={(value) => formatGrantLevel(String(value))}
                  onValueChange={(value) => {
                    if (typeof value === "string" && isLevel(value)) {
                      setLevel(value);
                    }
                  }}
                >
                  <Select.Option value="level1">Level 1 · Ask approval every use</Select.Option>
                  <Select.Option value="level2">Level 2 · Auto-approve</Select.Option>
                </Select>
                <Select
                  value={expires}
                  label="Expires (optional)"
                  hideLabel={false}
                  renderValue={(value) =>
                    EXPIRES_OPTIONS.find((o) => o.value === value)?.label ?? "Forever"
                  }
                  onValueChange={(value) => {
                    if (typeof value === "string") {
                      setExpires(value);
                    }
                  }}
                >
                  {EXPIRES_OPTIONS.map((opt) => (
                    <Select.Option key={opt.value} value={opt.value}>
                      {opt.label}
                    </Select.Option>
                  ))}
                </Select>
                <div className="flex justify-end">
                  <Button type="submit" variant="primary">
                    Save
                  </Button>
                </div>
              </form>
            </Dialog>
          </Dialog.Root>
        </>
      }
    >
      {error ? (
        <Text as="p" variant="error">
          {error}
        </Text>
      ) : null}
      <Input
        className="w-full"
        aria-label="Filter grants"
        placeholder="Search grants by item, agent, group, or level…"
        value={query}
        onChange={(e) => setQuery(e.currentTarget.value)}
      />
      <LayerCard>
        <LayerCard.Primary>
          {rows === null ? (
            <Text variant="secondary">Loading…</Text>
          ) : rows.length === 0 ? (
            <Empty title="No grants" description="Grant an agent or a human Use on an item." />
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>Item</Table.Head>
                  <Table.Head>Subject</Table.Head>
                  <Table.Head>Kind</Table.Head>
                  <Table.Head>Level</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {rows
                  .filter((g) => {
                    if (!query.trim()) return true;
                    const name = itemNameById[g.item_id] ?? g.item_id;
                    const hay = `${name} ${g.agent_id} ${g.subject_kind} ${g.level}`.toLowerCase();
                    return query
                      .trim()
                      .toLowerCase()
                      .split(/\s+/)
                      .every((t) => hay.includes(t));
                  })
                  .map((g) => (
                    <Table.Row key={g.id}>
                      <Table.Cell className="font-medium">
                        {itemNameById[g.item_id] ?? g.item_id}
                      </Table.Cell>
                      <Table.Cell className="font-mono text-sm">
                        {g.agent_id}
                      </Table.Cell>
                      <Table.Cell>
                        <Badge variant="neutral">
                          {formatSubjectKind(g.subject_kind)}
                        </Badge>
                      </Table.Cell>
                      <Table.Cell>
                        <Badge variant="neutral">
                          {formatGrantLevel(g.level)}
                        </Badge>
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
