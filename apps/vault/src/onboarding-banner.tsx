import { Banner } from "@cloudflare/kumo/components/banner";
import { Text } from "@cloudflare/kumo/components/text";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { onboarding, type OnboardingView } from "./onboarding";

export function OnboardingBanner() {
  const navigate = useNavigate();
  const [view, setView] = useState<OnboardingView | null>(null);

  useEffect(() => {
    let live = true;
    void onboarding().then((res) => {
      if (live && res.data) {
        setView(res.data);
      }
    });
    return () => {
      live = false;
    };
  }, []);

  if (!view || view.complete) {
    return null;
  }

  const open = view.steps.filter((s) => !s.done);
  const next = open[0];
  if (!next) {
    return null;
  }

  return (
    <Banner
      variant="alert"
      title="Finish setup"
      description={
        <>
          <Text as="span" size="sm">
            {next.title}: {next.human_action}
          </Text>
          {view.role === "owner" && view.item_count === 0 ? (
            <p className="mt-2 text-sm text-kumo-subtle">
              Import from 1Password, Bitwarden, Apple Passwords, and more — Items →
              Import. See{" "}
              <a
                className="underline text-kumo-strong"
                href="https://veil.nyc/docs/guides/migration-1password"
                rel="noopener noreferrer"
                target="_blank"
              >
                migrating from 1Password
              </a>
              . Agents cannot import for you.
            </p>
          ) : null}
          {view.role === "owner" && view.docs?.grok ? (
            <p className="mt-2 text-sm text-kumo-subtle">
              Wiring Grok?{" "}
              <a
                className="underline text-kumo-strong"
                href={view.docs.grok}
                rel="noopener noreferrer"
                target="_blank"
              >
                Custom MCP guide
              </a>{" "}
              (not 1Password Password Managers).
            </p>
          ) : null}
        </>
      }
      action={
        next.href ? (
          <Banner.Action
            onClick={() => {
              void navigate({ to: next.href as "/items" });
            }}
          >
            Continue
          </Banner.Action>
        ) : undefined
      }
    />
  );
}
