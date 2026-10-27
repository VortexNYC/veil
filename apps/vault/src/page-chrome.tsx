import { Text } from "@cloudflare/kumo/components/text";
import type { ReactNode } from "react";

export function PageChrome({
  title,
  subtitle,
  actions,
  children,
}: {
  title: string;
  subtitle?: string;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-4 p-6">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <Text as="h1" variant="heading">
            {title}
          </Text>
          {subtitle ? (
            <Text variant="secondary" size="sm">
              {subtitle}
            </Text>
          ) : null}
        </div>
        {actions}
      </div>
      {children}
    </div>
  );
}
