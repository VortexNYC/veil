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
import {
  addAgent,
  agents,
  agentWorkloads,
  bindAgentWorkload,
  revokeVaultAgent,
  unbindAgentWorkload,
  verifyWorkloadIssuer,
} from "../../origin";
import type { Agent, BindWorkloadRequest, WorkloadView } from "@vortex-api/veil";

export const Route = createFileRoute("/_app/agents")({
  component: Agents,
});

type Preset =
  | "vercel"
  | "github-actions"
  | "gitlab"
  | "google"
  | "bitbucket"
  | "aws-eks"
  | "azure"
  | "fly"
  | "custom";

function isPreset(v: string): v is Preset {
  return (
    v === "vercel" ||
    v === "github-actions" ||
    v === "gitlab" ||
    v === "google" ||
    v === "bitbucket" ||
    v === "aws-eks" ||
    v === "azure" ||
    v === "fly" ||
    v === "custom"
  );
}

function compilePresetPolicy(
  preset: Preset,
  fd: FormData,
): BindWorkloadRequest {
  const claimLines = String(fd.get("claims") ?? "")
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean);
  const claims: Record<string, string[]> = {};
  for (const line of claimLines) {
    const idx = line.indexOf("=");
    if (idx > 0) {
      const k = line.slice(0, idx).trim();
      const v = line.slice(idx + 1).trim();
      if (k && v) {
        claims[k] = [...(claims[k] ?? []), v];
      }
    }
  }
  const hasClaims = Object.keys(claims).length > 0;
  if (preset === "vercel") {
    const team = String(fd.get("org") ?? "").trim();
    const proj = String(fd.get("app") ?? "").trim();
    const env = String(fd.get("env") ?? "production").trim() || "production";
    return {
      issuer: `https://oidc.vercel.com/${team}`,
      subject: `owner:${team}:project:${proj}:environment:${env}`,
      audience: String(fd.get("audience") ?? "").trim() || `https://vercel.com/${team}`,
      ...(hasClaims ? { claims } : {}),
    };
  }
  if (preset === "github-actions") {
    const repo = String(fd.get("repo") ?? "").trim();
    const ref = String(fd.get("ref") ?? "refs/heads/main").trim() || "refs/heads/main";
    return {
      issuer: "https://token.actions.githubusercontent.com",
      subject: ref === "*" ? `repo:${repo}:*` : `repo:${repo}:ref:${ref}`,
      audience: String(fd.get("audience") ?? "").trim() || "veil",
      ...(hasClaims ? { claims } : {}),
    };
  }
  if (preset === "fly") {
    const org = String(fd.get("org") ?? "").trim();
    const app = String(fd.get("app") ?? "").trim();
    return {
      issuer: `https://oidc.fly.io/${org}`,
      subject: `${org}:${app}`,
      audience: String(fd.get("audience") ?? "").trim() || "veil",
      ...(hasClaims ? { claims } : {}),
    };
  }
  if (preset === "gitlab") {
    const repo = String(fd.get("repo") ?? "").trim();
    const ref = String(fd.get("ref") ?? "main").trim() || "main";
    return {
      issuer: "https://gitlab.com",
      subject: `project_path:${repo}:ref_type:branch:ref:${ref}`,
      audience: String(fd.get("audience") ?? "").trim() || "veil",
      ...(hasClaims ? { claims } : {}),
    };
  }
  if (preset === "google") {
    const email = String(fd.get("org") ?? "").trim();
    const sub = String(fd.get("subject") ?? "*").trim() || "*";
    if (email) {
      claims.email = [...(claims.email ?? []), email];
    }
    return {
      issuer: "https://accounts.google.com",
      subject: sub,
      audience: String(fd.get("audience") ?? "").trim() || "veil",
      ...(Object.keys(claims).length > 0 ? { claims } : {}),
    };
  }
  if (preset === "bitbucket") {
    const ws = String(fd.get("org") ?? "").trim();
    const sub = String(fd.get("subject") ?? "").trim();
    return {
      issuer: `https://api.bitbucket.org/2.0/workspaces/${ws}/pipelines-config/identity/oidc`,
      subject: sub,
      audience:
        String(fd.get("audience") ?? "").trim() ||
        `ari:cloud:bitbucket::workspace/${ws}`,
      ...(hasClaims ? { claims } : {}),
    };
  }
  if (preset === "aws-eks") {
    const iss = String(fd.get("issuer") ?? "").trim();
    const ns = String(fd.get("org") ?? "").trim();
    const sa = String(fd.get("app") ?? "").trim();
    return {
      issuer: iss,
      subject: `system:serviceaccount:${ns}:${sa}`,
      audience: String(fd.get("audience") ?? "").trim() || "veil",
      ...(hasClaims ? { claims } : {}),
    };
  }
  if (preset === "azure") {
    const tenant = String(fd.get("org") ?? "").trim().toLowerCase();
    const oid = String(fd.get("app") ?? "").trim().toLowerCase();
    const sub = String(fd.get("subject") ?? "*").trim() || "*";
    if (oid) {
      claims.oid = [...(claims.oid ?? []), oid];
    }
    return {
      issuer: `https://login.microsoftonline.com/${tenant}/v2.0`,
      subject: sub,
      audience: String(fd.get("audience") ?? "").trim() || "veil",
      ...(Object.keys(claims).length > 0 ? { claims } : {}),
    };
  }
  return {
    issuer: String(fd.get("issuer") ?? "").trim(),
    subject: String(fd.get("subject") ?? "").trim(),
    audience: String(fd.get("audience") ?? "").trim() || "veil",
    ...(hasClaims ? { claims } : {}),
  };
}

function Agents() {
  const [rows, setRows] = useState<Agent[] | null>(null);
  const [byAgent, setByAgent] = useState<Record<string, WorkloadView[]>>({});
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [bindTarget, setBindTarget] = useState<string | null>(null);
  const [preset, setPreset] = useState<Preset>("vercel");
  const [verifyMsg, setVerifyMsg] = useState<string | null>(null);

  async function reload() {
    const res = await agents();
    if (res.error || !res.data) {
      setError("list failed");
      setRows([]);
      return;
    }
    setError(null);
    setRows(res.data.agents);
    const nextMap: Record<string, WorkloadView[]> = {};
    await Promise.all(
      res.data.agents.map(async (a) => {
        const wres = await agentWorkloads(a.id);
        if (wres.data) {
          nextMap[a.id] = wres.data.workloads;
        }
      }),
    );
    setByAgent(nextMap);
  }

  useEffect(() => {
    void reload();
  }, []);

  return (
    <PageChrome
      title="Agents"
      subtitle="Principals. Bind external OIDC workload trust policies or mint Hydra client secrets via CLI."
      actions={
        <>
          <Button type="button" variant="primary" onClick={() => setOpen(true)}>
            Add
          </Button>
          <Dialog.Root open={open} onOpenChange={setOpen}>
            <Dialog size="sm" className="p-6">
              <form
                className="flex flex-col gap-4"
                onSubmit={(e) => {
                  e.preventDefault();
                  const fd = new FormData(e.currentTarget);
                  const name = String(fd.get("name") ?? "");
                  void addAgent(name).then((res) => {
                    if (res.error) {
                      setError("create failed");
                      return;
                    }
                    setOpen(false);
                    void reload();
                  });
                }}
              >
                <Dialog.Title>Add agent</Dialog.Title>
                <Dialog.Description>Lowercase name. Not a token.</Dialog.Description>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="name">Name</Label>
                  <Input id="name" name="name" required />
                </div>
                <div className="flex justify-end">
                  <Button type="submit" variant="primary">
                    Save
                  </Button>
                </div>
              </form>
            </Dialog>
          </Dialog.Root>
          <Dialog.Root
            open={bindTarget !== null}
            onOpenChange={(next) => {
              if (!next) {
                setBindTarget(null);
                setVerifyMsg(null);
              }
            }}
          >
            <Dialog size="sm" className="p-6">
              <form
                className="flex flex-col gap-4"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (!bindTarget) {
                    return;
                  }
                  const fd = new FormData(e.currentTarget);
                  const policy = compilePresetPolicy(preset, fd);
                  void bindAgentWorkload(bindTarget, policy).then((res) => {
                    if (res.error) {
                      setError("bind failed");
                      return;
                    }
                    setBindTarget(null);
                    setVerifyMsg(null);
                    void reload();
                  });
                }}
              >
                <Dialog.Title>Bind OIDC policy — {bindTarget}</Dialog.Title>
                <Dialog.Description>
                  Trust short-lived workload OIDC tokens with zero static keys in env vars.
                </Dialog.Description>
                <Select
                  value={preset}
                  label="Provider"
                  hideLabel={false}
                  onValueChange={(value) => {
                    if (typeof value === "string" && isPreset(value)) {
                      setPreset(value);
                      setVerifyMsg(null);
                    }
                  }}
                >
                  <Select.Option value="vercel">Vercel</Select.Option>
                  <Select.Option value="github-actions">GitHub Actions</Select.Option>
                  <Select.Option value="gitlab">GitLab CI</Select.Option>
                  <Select.Option value="google">Google Cloud</Select.Option>
                  <Select.Option value="bitbucket">Bitbucket Pipelines</Select.Option>
                  <Select.Option value="aws-eks">AWS EKS</Select.Option>
                  <Select.Option value="azure">Azure Entra ID</Select.Option>
                  <Select.Option value="fly">Fly.io</Select.Option>
                  <Select.Option value="custom">Custom OIDC (Better Auth / Clerk / WorkOS)</Select.Option>
                </Select>
                {preset === "vercel" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="org">Team slug</Label>
                      <Input id="org" name="org" placeholder="acme" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="app">Project name</Label>
                      <Input id="app" name="app" placeholder="acme-web" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="env">Environment (production, preview, development, or *)</Label>
                      <Input id="env" name="env" defaultValue="production" required />
                    </div>
                  </>
                ) : null}
                {preset === "github-actions" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="repo">Repository (owner/name)</Label>
                      <Input id="repo" name="repo" placeholder="VortexNYC/veil" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="ref">Git ref (or * for all refs)</Label>
                      <Input id="ref" name="ref" defaultValue="refs/heads/main" required />
                    </div>
                  </>
                ) : null}
                {preset === "fly" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="org">Fly org slug</Label>
                      <Input id="org" name="org" placeholder="vortex" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="app">Fly app name</Label>
                      <Input id="app" name="app" placeholder="worker" required />
                    </div>
                  </>
                ) : null}
                {preset === "gitlab" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="repo">Project path (group/project)</Label>
                      <Input id="repo" name="repo" placeholder="acme/platform/web" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="ref">Branch</Label>
                      <Input id="ref" name="ref" defaultValue="main" required />
                    </div>
                  </>
                ) : null}
                {preset === "google" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="org">Service account email</Label>
                      <Input id="org" name="org" placeholder="bot@acme.iam.gserviceaccount.com" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="subject">Numeric unique ID (or *)</Label>
                      <Input id="subject" name="subject" defaultValue="*" required />
                    </div>
                  </>
                ) : null}
                {preset === "bitbucket" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="org">Workspace slug</Label>
                      <Input id="org" name="org" placeholder="acme" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="subject">Subject</Label>
                      <Input id="subject" name="subject" placeholder="{repo-uuid}" required />
                    </div>
                  </>
                ) : null}
                {preset === "aws-eks" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="issuer">Cluster OIDC issuer URL</Label>
                      <Input id="issuer" name="issuer" placeholder="https://oidc.eks.us-east-1.amazonaws.com/id/EX" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="org">Namespace</Label>
                      <Input id="org" name="org" defaultValue="default" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="app">Service account name</Label>
                      <Input id="app" name="app" placeholder="deploy-bot" required />
                    </div>
                  </>
                ) : null}
                {preset === "azure" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="org">Tenant ID (GUID)</Label>
                      <Input id="org" name="org" placeholder="aaaabbbb-0000-cccc-1111-dddd2222eeee" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="app">Object ID (oid GUID)</Label>
                      <Input id="app" name="app" placeholder="aaaaaaaa-0000-1111-2222-bbbbbbbbbbbb" required />
                    </div>
                  </>
                ) : null}
                {preset === "custom" ? (
                  <>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="issuer">Issuer URL (https://)</Label>
                      <Input id="issuer" name="issuer" placeholder="https://auth.example.com" required />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor="subject">Subject claim (or prefix*)</Label>
                      <Input id="subject" name="subject" placeholder="system:serviceaccount:prod:*" required />
                    </div>
                  </>
                ) : null}
                <div className="flex flex-col gap-1">
                  <Label htmlFor="audience">Audience (optional override)</Label>
                  <Input id="audience" name="audience" placeholder={preset === "vercel" ? "https://vercel.com/<team>" : "veil"} />
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="claims">Additional claims (optional key=value per line, e.g. owner_id=team_...)</Label>
                  <Input id="claims" name="claims" placeholder="owner_id=team_abc123" />
                </div>
                {verifyMsg ? <Text variant="secondary">{verifyMsg}</Text> : null}
                <div className="flex justify-end gap-2">
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={(e) => {
                      const form = e.currentTarget.form;
                      if (!form || !form.reportValidity()) {
                        return;
                      }
                      const policy = compilePresetPolicy(preset, new FormData(form));
                      void verifyWorkloadIssuer(policy.issuer).then((res) => {
                        if (res.error || !res.data) {
                          setVerifyMsg("verify request failed");
                          return;
                        }
                        setVerifyMsg(`${res.data.code}: ${res.data.message}`);
                      });
                    }}
                  >
                    Verify issuer
                  </Button>
                  <Button type="submit" variant="primary">
                    Bind OIDC
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
            <Empty title="No agents" description="Register a principal, then grant items." />
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>Name</Table.Head>
                  <Table.Head>Kind</Table.Head>
                  <Table.Head>OIDC Federation</Table.Head>
                  <Table.Head>Revoke</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {rows.map((a) => (
                  <Table.Row key={a.id}>
                    <Table.Cell>{a.id}</Table.Cell>
                    <Table.Cell>{a.kind}</Table.Cell>
                    <Table.Cell>
                      <div className="flex flex-col gap-2">
                        {(byAgent[a.id] ?? []).map((w) => (
                          <div key={`${w.issuer}|${w.subject}`} className="flex items-center gap-2">
                            <Text variant="secondary">
                              {w.issuer} → {w.subject}
                            </Text>
                            <Button
                              type="button"
                              variant="secondary"
                              onClick={() => {
                                void unbindAgentWorkload(a.id, w.issuer, w.subject).then(() => reload());
                              }}
                            >
                              Unbind
                            </Button>
                          </div>
                        ))}
                        <div>
                          <Button
                            type="button"
                            variant="secondary"
                            onClick={() => {
                              setBindTarget(a.id);
                              setVerifyMsg(null);
                            }}
                          >
                            Bind OIDC
                          </Button>
                        </div>
                      </div>
                    </Table.Cell>
                    <Table.Cell>
                      {a.revoked_at ? (
                        <Text as="span" variant="secondary" size="sm">
                          Revoked
                        </Text>
                      ) : (
                        <Button
                          type="button"
                          variant="secondary"
                          onClick={() => {
                            void revokeVaultAgent(a.id).then((res) => {
                              if (res.error) {
                                setError(`revoke failed for ${a.id}`);
                                return;
                              }
                              setError(null);
                              void reload();
                            });
                          }}
                        >
                          Revoke
                        </Button>
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
