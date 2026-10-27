import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Label } from "@cloudflare/kumo/components/label";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Text } from "@cloudflare/kumo/components/text";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { PageChrome } from "../../page-chrome";

export const Route = createFileRoute("/_app/invites")({
  component: Invites,
});

function Invites() {
  const [email, setEmail] = useState("");
  const [copied, setCopied] = useState(false);
  const cmd = `veil human invite ${email.trim() || "member@example.com"} --code-file ~/.veil/invite.code`;

  return (
    <PageChrome
      title="Invites"
      subtitle="Invite members into this organization via Kratos recovery."
    >
      <LayerCard>
        <LayerCard.Secondary>Invite a Member</LayerCard.Secondary>
        <LayerCard.Primary>
          <div className="flex flex-col gap-3">
            <Text as="p" variant="secondary" size="sm">
              Courier delivers the setup link directly from{" "}
              <code>noreply@veil.nyc</code>, and the one-time recovery code writes
              to a mode-600 local file (never over browser HTTP).
            </Text>
            <div className="flex flex-col gap-1">
              <Label htmlFor="invite-email">Member email</Label>
              <Input
                id="invite-email"
                type="email"
                placeholder="teammate@company.com"
                value={email}
                onChange={(e) => {
                  setCopied(false);
                  setEmail(e.currentTarget.value);
                }}
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label>CLI command</Label>
              <div className="flex items-center gap-2">
                <Input className="w-full font-mono text-xs" readOnly value={cmd} />
                <Button
                  type="button"
                  variant="primary"
                  onClick={() => {
                    void navigator.clipboard
                      ?.writeText(cmd)
                      .then(() => setCopied(true));
                  }}
                >
                  {copied ? "Copied" : "Copy command"}
                </Button>
              </div>
            </div>
          </div>
        </LayerCard.Primary>
      </LayerCard>
    </PageChrome>
  );
}
