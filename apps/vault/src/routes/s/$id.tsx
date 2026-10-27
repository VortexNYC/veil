import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Label } from "@cloudflare/kumo/components/label";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Text } from "@cloudflare/kumo/components/text";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { redeemShareLink } from "../../origin";
import { openShare, type ShareMeta } from "../../share";

export const Route = createFileRoute("/s/$id")({
  component: ShareRedeem,
});

type Phase =
  | "loading"
  | "gone"
  | "limited"
  | "nokey"
  | "needpass"
  | "done"
  | "badpass";

// ShareRedeem is the public link target — no auth. The redeem POST burns
// the share exactly once; the sealed blob stays in memory so a wrong
// passphrase never costs the recipient a second (already-404) fetch. The
// key comes from the URL fragment, which browsers never send to a server.
function ShareRedeem() {
  const { id } = Route.useParams();
  const [phase, setPhase] = useState<Phase>("loading");
  const [secret, setSecret] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [envelope, setEnvelope] = useState<{
    blobB64: string;
    meta?: ShareMeta;
    key: string;
  } | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    const m = /(?:^|[#&])k=([A-Za-z0-9_-]+)/.exec(window.location.hash);
    if (!m) {
      setPhase("nokey");
      return;
    }
    const key = m[1];
    void redeemShareLink(id).then(async (res) => {
      if (res.status === 404 || res.status === 410) {
        setPhase("gone");
        return;
      }
      if (res.status === 429) {
        setPhase("limited");
        return;
      }
      if (!res.data) {
        setPhase("gone");
        return;
      }
      const env = { blobB64: res.data.blob_b64, meta: res.data.meta as ShareMeta | undefined, key };
      setName(res.data.name ?? "");
      if (env.meta?.pw) {
        setEnvelope(env);
        setPhase("needpass");
        return;
      }
      const plain = await openShare(env.blobB64, env.meta, key, "");
      if (plain === null) {
        setPhase("gone");
        return;
      }
      setSecret(plain);
      setPhase("done");
    });
  }, [id]);

  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <div className="w-full max-w-lg">
        <LayerCard>
          <LayerCard.Primary>
            <div className="flex flex-col gap-4">
              <Text as="h1" variant="heading">
                Shared secret
              </Text>
              {phase === "loading" ? (
                <Text variant="secondary">Redeeming one-time link…</Text>
              ) : null}
              {phase === "nokey" ? (
                <Text variant="secondary">
                  This link is missing its decryption key. Ask the sender for the
                  complete URL — the part after <code>#</code> never leaves the
                  browser.
                </Text>
              ) : null}
              {phase === "gone" ? (
                <Text variant="secondary">
                  This link has already been viewed, revoked, or expired. Ask the
                  sender for a new one.
                </Text>
              ) : null}
              {phase === "limited" ? (
                <Text variant="secondary">
                  Too many attempts on this link. Try again in a minute.
                </Text>
              ) : null}
              {phase === "needpass" || phase === "badpass" ? (
                <form
                  className="flex flex-col gap-3"
                  onSubmit={(e) => {
                    e.preventDefault();
                    if (!envelope) {
                      return;
                    }
                    const pass = String(new FormData(e.currentTarget).get("passphrase") ?? "");
                    void openShare(envelope.blobB64, envelope.meta, envelope.key, pass).then(
                      (plain) => {
                        if (plain === null) {
                          setPhase("badpass");
                          return;
                        }
                        setSecret(plain);
                        setPhase("done");
                      },
                    );
                  }}
                >
                  <div className="flex flex-col gap-1">
                    <Label htmlFor="passphrase">Passphrase</Label>
                    <Input
                      id="passphrase"
                      name="passphrase"
                      type="password"
                      autoComplete="off"
                      autoFocus
                      required
                    />
                  </div>
                  {phase === "badpass" ? (
                    <Text as="p" variant="error" size="sm">
                      Wrong passphrase — try again.
                    </Text>
                  ) : null}
                  <div className="flex justify-end">
                    <Button type="submit" variant="primary">
                      Reveal
                    </Button>
                  </div>
                </form>
              ) : null}
              {phase === "done" && secret !== null ? (
                <>
                  {name ? (
                    <Text variant="secondary" size="sm">
                      {name}
                    </Text>
                  ) : null}
                  <div className="flex flex-col gap-1">
                    <Label htmlFor="revealed">Secret</Label>
                    <Input id="revealed" readOnly value={secret} />
                  </div>
                  <div className="flex items-center gap-2">
                    <Button
                      type="button"
                      variant="primary"
                      onClick={() => {
                        void navigator.clipboard?.writeText(secret).then(() => setCopied(true));
                      }}
                    >
                      {copied ? "Copied" : "Copy"}
                    </Button>
                    <Text variant="secondary" size="sm">
                      This link is consumed — it will not open again.
                    </Text>
                  </div>
                </>
              ) : null}
            </div>
          </LayerCard.Primary>
        </LayerCard>
      </div>
    </div>
  );
}
