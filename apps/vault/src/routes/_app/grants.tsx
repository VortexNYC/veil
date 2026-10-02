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
import { PageChrome } from "../../page-chrome";
import { addGrant, grants } from "../../origin";
import type { Grant } from "@vortex-api/veil";

export const Route = createFileRoute("/_app/grants")({
  component: Grants,
});

type Level = "level1" | "level2";

function isLevel(value: string): value is Level {
  return value === "level1" || value === "level2";
}

function Grants() {
  const [rows, setRows] = useState<Grant[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [level, setLevel] = useState<Level>("level2");

  async function reload() {
    const res = await grants();
    if (res.error || !res.data) {
      setError("list failed");
      setRows([]);
      return;
    }
    setError(null);
    setRows(res.data.grants);
  }

  useEffect(() => {
    void reload();
  }, []);

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
              }
            }}
          >
            <Dialog size="sm" className="p-6">
              <form
                className="flex flex-col gap-4"
                onSubmit={(e) => {
                  e.preventDefault();
                  const fd = new FormData(e.currentTarget);
                  const item = String(fd.get("item") ?? "");
                  const agent = String(fd.get("agent") ?? "");
                  const human = String(fd.get("human") ?? "");
                  const group = String(fd.get("group") ?? "");
                  void addGrant({
                    item,
                    level,
                    agent: agent || undefined,
                    human: human || undefined,
                    group: group || undefined,
                  }).then((res) => {
                    if (res.error) {
                      setError("create failed");
                      return;
                    }
                    setOpen(false);
                    setLevel("level2");
                    void reload();
                  });
                }}
              >
                <Dialog.Title>Grant Use</Dialog.Title>
                <Dialog.Description>XOR agent name, human identity id, or group name.</Dialog.Description>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="item">Item</Label>
                  <Input id="item" name="item" required />
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="agent">Agent</Label>
                  <Input id="agent" name="agent" />
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="human">Human id</Label>
                  <Input id="human" name="human" />
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="group">Group</Label>
                  <Input id="group" name="group" />
                </div>
                <Select
                  value={level}
                  label="Level"
                  hideLabel={false}
                  onValueChange={(value) => {
                    if (typeof value === "string" && isLevel(value)) {
                      setLevel(value);
                    }
                  }}
                >
                  <Select.Option value="level1">level1</Select.Option>
                  <Select.Option value="level2">level2</Select.Option>
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
                {rows.map((g) => (
                  <Table.Row key={g.id}>
                    <Table.Cell>{g.item_id}</Table.Cell>
                    <Table.Cell>{g.agent_id}</Table.Cell>
                    <Table.Cell>{g.subject_kind}</Table.Cell>
                    <Table.Cell>{g.level}</Table.Cell>
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
