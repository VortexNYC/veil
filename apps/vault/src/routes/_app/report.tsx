import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Table } from "@cloudflare/kumo/components/table";
import { Text } from "@cloudflare/kumo/components/text";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { PageChrome } from "../../page-chrome";
import { report } from "../../origin";
import type { VaultReport } from "@vortex-api/veil";

export const Route = createFileRoute("/_app/report")({
  component: Report,
});

type Finding = VaultReport["findings"][number];

function issueLabels(f: Finding): string[] {
  const out: string[] = []
  if (f.weak) {
    out.push(...f.weak)
  }
  if (f.reused && f.reused > 0) {
    out.push(`reused ×${f.reused}`)
  }
  if (f.pwned > 0) {
    out.push(`pwned ×${f.pwned}`)
  }
  return out
}

// Worst-first ordering: a known breach outranks password reuse, reuse
// outranks a merely weak secret. Same-severity rows order by reuse count
// then name so the list is stable across reloads.
type Severity = 1 | 2 | 3;

function severity(f: Finding): Severity {
  if (f.pwned > 0) {
    return 3;
  }
  if (f.reused && f.reused > 0) {
    return 2;
  }
  return 1;
}

const SEVERITY_BADGE = { 3: "red", 2: "orange", 1: "warning" } as const;
const SEVERITY_LABEL = { 3: "breached", 2: "reused", 1: "weak" } as const;

function sortedFindings(rep: VaultReport): Finding[] {
  return [...rep.findings].sort((a, b) => {
    const s = severity(b) - severity(a);
    if (s !== 0) {
      return s;
    }
    const r = (b.reused ?? 0) - (a.reused ?? 0);
    if (r !== 0) {
      return r;
    }
    return a.name.localeCompare(b.name);
  });
}

function StatCard({ label, value, tone }: { label: string; value: string; tone?: "red" | "orange" | "warning" }) {
  return (
    <LayerCard>
      <LayerCard.Secondary>{label}</LayerCard.Secondary>
      <LayerCard.Primary>
        <Text as="p" variant="heading">
          {tone ? <Badge variant={tone}>{value}</Badge> : value}
        </Text>
      </LayerCard.Primary>
    </LayerCard>
  );
}

function Report() {
  const [rep, setRep] = useState<VaultReport | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [hibp, setHibp] = useState(false)

  useEffect(() => {
    setRep(null)
    setError(null)
    void report(hibp).then((res) => {
      if (res.error || !res.data) {
        setError("report failed")
        return
      }
      setRep(res.data)
    })
  }, [hibp])

  const findings = rep ? sortedFindings(rep) : [];
  // A failed breach lookup is not "all clear" — pwned is unknown, not zero.
  const clean =
    rep !== null &&
    rep.weak === 0 &&
    rep.reused === 0 &&
    rep.pwned === 0 &&
    rep.hibp !== "error";

  return (
    <PageChrome title="Report" subtitle="Vault health. Metadata only — never secrets.">
      {error ? (
        <Text as="p" variant="error">
          {error}
        </Text>
      ) : null}
      {rep === null ? (
        <LayerCard>
          <LayerCard.Primary>
            <Text variant="secondary">Loading…</Text>
          </LayerCard.Primary>
        </LayerCard>
      ) : (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <StatCard label="Items scored" value={String(rep.items)} />
          <StatCard label="Weak" value={String(rep.weak)} tone={rep.weak > 0 ? "warning" : undefined} />
          <StatCard label="Reused" value={String(rep.reused)} tone={rep.reused > 0 ? "orange" : undefined} />
          <StatCard
            label="Breached"
            value={
              rep.hibp === "ok"
                ? String(rep.pwned)
                : rep.hibp === "error"
                  ? "check failed"
                  : "not checked"
            }
            tone={rep.hibp === "ok" && rep.pwned > 0 ? "red" : undefined}
          />
        </div>
      )}
      <LayerCard>
        <LayerCard.Primary>
          <div className="flex items-center gap-3">
            <Text variant="secondary">
              {rep === null
                ? "Scoring passwords offline; breach lookups are opt-in."
                : rep.hibp === "ok"
                  ? "Breach check on — secrets were k-anonymity hashed against HIBP."
                  : rep.hibp === "error"
                    ? "Breach lookup failed — the offline score still applies."
                    : "Breach lookup is off — no hash leaves this page until enabled."}
            </Text>
            <Button
              variant={hibp ? "primary" : "secondary"}
              onClick={() => setHibp((v) => !v)}
            >
              {hibp ? "Breach check on" : "Check breaches (HIBP)"}
            </Button>
          </div>
        </LayerCard.Primary>
      </LayerCard>
      <LayerCard>
        <LayerCard.Primary>
          {rep === null ? null : rep.findings.length === 0 ? (
            <Empty
              title={clean ? "All clear" : "No findings"}
              description={
                clean
                  ? rep.hibp === "ok"
                    ? "No weak, reused, or breached secrets found."
                    : "No weak or reused secrets found — breach lookup is off."
                  : rep.hibp === "error"
                    ? "No weak or reused secrets — the breach lookup failed, so pwned is unknown."
                    : "Nothing matched this report run."
              }
            />
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>Severity</Table.Head>
                  <Table.Head>Item</Table.Head>
                  <Table.Head>Kind</Table.Head>
                  <Table.Head>URI</Table.Head>
                  <Table.Head>Findings</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {findings.map((f) => (
                  <Table.Row key={f.item_id}>
                    <Table.Cell>
                      <Badge variant={SEVERITY_BADGE[severity(f)]}>
                        {SEVERITY_LABEL[severity(f)]}
                      </Badge>
                    </Table.Cell>
                    <Table.Cell>{f.name}</Table.Cell>
                    <Table.Cell>{f.kind}</Table.Cell>
                    <Table.Cell>
                      <Text as="span" variant="mono-secondary">
                        {f.uri ?? ""}
                      </Text>
                    </Table.Cell>
                    <Table.Cell>{issueLabels(f).join(", ")}</Table.Cell>
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
