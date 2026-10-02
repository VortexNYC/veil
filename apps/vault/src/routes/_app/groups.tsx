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
import { addGroup, addGroupMemberTo, groupMembers, groups, removeGroupMemberFrom } from "../../origin";
import type { Group, GroupMember } from "@vortex-api/veil";

export const Route = createFileRoute("/_app/groups")({
  component: Groups,
});

type MemberKind = "agent" | "human";

function isMemberKind(value: string): value is MemberKind {
  return value === "agent" || value === "human";
}

function Groups() {
  const [rows, setRows] = useState<Group[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [manage, setManage] = useState<Group | null>(null);

  async function reload() {
    const res = await groups();
    if (res.error || !res.data) {
      setError("list failed");
      setRows([]);
      return;
    }
    setError(null);
    setRows(res.data.groups);
  }

  useEffect(() => {
    void reload();
  }, []);

  return (
    <PageChrome
      title="Groups"
      subtitle="Org-scoped principal sets. A grant to a group is the shared vault."
      actions={
        <>
          <Button type="button" variant="primary" onClick={() => setOpen(true)}>
            New group
          </Button>
          <Dialog.Root open={open} onOpenChange={setOpen}>
            <Dialog size="sm" className="p-6">
              <form
                className="flex flex-col gap-4"
                onSubmit={(e) => {
                  e.preventDefault();
                  const name = String(new FormData(e.currentTarget).get("name") ?? "");
                  void addGroup(name).then((res) => {
                    if (res.error) {
                      setError("create failed");
                      return;
                    }
                    setOpen(false);
                    void reload();
                  });
                }}
              >
                <Dialog.Title>New group</Dialog.Title>
                <Dialog.Description>Name it like a team — eng, ops, finance.</Dialog.Description>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="name">Name</Label>
                  <Input id="name" name="name" required />
                </div>
                <div className="flex justify-end">
                  <Button type="submit" variant="primary">
                    Create
                  </Button>
                </div>
              </form>
            </Dialog>
          </Dialog.Root>
          <MembersDialog group={manage} onClose={() => setManage(null)} onChanged={() => void reload()} />
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
            <Empty title="No groups" description="Create a group, grant it Use on items — every member inherits." />
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>Name</Table.Head>
                  <Table.Head>Group id</Table.Head>
                  <Table.Head></Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {rows.map((g) => (
                  <Table.Row key={g.id}>
                    <Table.Cell>{g.name}</Table.Cell>
                    <Table.Cell>{g.id}</Table.Cell>
                    <Table.Cell>
                      <Button type="button" variant="secondary" onClick={() => setManage(g)}>
                        Members
                      </Button>
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

function MembersDialog({
  group,
  onClose,
  onChanged,
}: {
  group: Group | null;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [members, setMembers] = useState<GroupMember[] | null>(null);
  const [kind, setKind] = useState<MemberKind>("agent");
  const [error, setError] = useState<string | null>(null);

  async function reload(name: string) {
    const res = await groupMembers(name);
    if (res.error || !res.data) {
      setError("list failed");
      setMembers([]);
      return;
    }
    setError(null);
    setMembers(res.data.members);
  }

  useEffect(() => {
    if (group) {
      setMembers(null);
      void reload(group.name);
    }
  }, [group]);

  return (
    <Dialog.Root
      open={group !== null}
      onOpenChange={(next) => {
        if (!next) {
          setKind("agent");
          setError(null);
          onClose();
        }
      }}
    >
      <Dialog size="sm" className="p-6">
        <Dialog.Title>{group?.name} members</Dialog.Title>
        <Dialog.Description>Agents by id; humans by Kratos identity id.</Dialog.Description>
        {error ? (
          <Text as="p" variant="error">
            {error}
          </Text>
        ) : null}
        {members === null ? (
          <Text variant="secondary">Loading…</Text>
        ) : members.length === 0 ? (
          <Text variant="secondary">No members.</Text>
        ) : (
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.Head>Kind</Table.Head>
                <Table.Head>Member</Table.Head>
                <Table.Head></Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {members.map((m) => (
                <Table.Row key={`${m.member_kind}:${m.member_id}`}>
                  <Table.Cell>{m.member_kind}</Table.Cell>
                  <Table.Cell>{m.member_id}</Table.Cell>
                  <Table.Cell>
                    <Button
                      type="button"
                      variant="secondary"
                      onClick={() => {
                        if (!group) {
                          return;
                        }
                        void removeGroupMemberFrom(group.name, m.member_kind, m.member_id).then((res) => {
                          if (res.error) {
                            setError("remove failed");
                            return;
                          }
                          void reload(group.name);
                          onChanged();
                        });
                      }}
                    >
                      Remove
                    </Button>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        )}
        <form
          className="mt-4 flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (!group) {
              return;
            }
            const fd = new FormData(e.currentTarget);
            const member = String(fd.get("member") ?? "");
            void addGroupMemberTo(group.name, kind, member).then((res) => {
              if (res.error) {
                setError("add failed");
                return;
              }
              void reload(group.name);
              onChanged();
            });
          }}
        >
          <div className="flex flex-col gap-1">
            <Label htmlFor="member">Member id</Label>
            <Input id="member" name="member" required />
          </div>
          <Select
            value={kind}
            label="Kind"
            hideLabel={false}
            onValueChange={(value) => {
              if (typeof value === "string" && isMemberKind(value)) {
                setKind(value);
              }
            }}
          >
            <Select.Option value="agent">agent</Select.Option>
            <Select.Option value="human">human</Select.Option>
          </Select>
          <div className="flex justify-end">
            <Button type="submit" variant="primary">
              Add
            </Button>
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  );
}
