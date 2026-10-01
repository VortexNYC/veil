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

function issueLabels(f: VaultReport["findings"][number]): string[] {
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

  return (
    <PageChrome title="Report" subtitle="Vault health. Metadata only — never secrets.">
      {error ? (
        <Text as="p" variant="error">
          {error}
        </Text>
      ) : null}
      <LayerCard>
        <LayerCard.Primary>
          {rep === null ? (
            <Text variant="secondary">Loading…</Text>
          ) : (
            <Text variant="secondary">
              {rep.items} items — {rep.weak} weak, {rep.reused} reused
              {rep.hibp === "ok"
                ? `, ${rep.pwned} breached`
                : rep.hibp === "error"
                  ? ", breach check failed"
                  : ""}
            </Text>
          )}
          <Button
            variant={hibp ? "primary" : "secondary"}
            onClick={() => setHibp((v) => !v)}
          >
            {hibp ? "Breach check on" : "Check breaches (HIBP)"}
          </Button>
        </LayerCard.Primary>
      </LayerCard>
      <LayerCard>
        <LayerCard.Primary>
          {rep === null ? null : rep.findings.length === 0 ? (
            <Empty title="No findings" />
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>Item</Table.Head>
                  <Table.Head>Kind</Table.Head>
                  <Table.Head>URI</Table.Head>
                  <Table.Head>Findings</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {rep.findings.map((f) => (
                  <Table.Row key={f.item_id}>
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
