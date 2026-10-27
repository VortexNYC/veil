import type { ReactNode } from "react";

const KIND_LABELS: Record<string, string> = {
  all: "All accounts",
  pinned: "Pinned",
  "2fa": "2FA enabled",
  archived: "Archived",
  api_key: "Login",
  passkey: "Passkey",
  card: "Card",
  identity: "Identity",
  ssh: "SSH Key",
  oauth: "OAuth / WIF",
  wif: "OAuth / WIF",
  file: "File",
  secure_note: "Secure Note",
};

export function formatKind(kind: string): string {
  return KIND_LABELS[kind] ?? kind;
}

const AUDIT_ACTION_LABELS: Record<string, string> = {
  reveal: "Reveal",
  fill: "Autofill",
  totp_mint: "2FA code",
  fill_sync: "Device sync",
  use: "Agent Use",
  item_created: "Item created",
  item_updated: "Item updated",
  item_deleted: "Item deleted",
  item_archived: "Item archived",
  item_rotated: "Item rotated",
  share_created: "Share created",
  share_redeemed: "Share opened",
  share_revoked: "Share revoked",
  grant_created: "Grant created",
  agent_created: "Agent created",
  agent_revoked: "Agent revoked",
};

export function formatAuditAction(action: string): string {
  return AUDIT_ACTION_LABELS[action] ?? action.replaceAll("_", " ");
}

const GRANT_LEVEL_LABELS: Record<string, string> = {
  level1: "Level 1 · Ask approval",
  level2: "Level 2 · Auto-approve",
};

export function formatGrantLevel(level: string): string {
  return GRANT_LEVEL_LABELS[level] ?? level;
}

const SUBJECT_KIND_LABELS: Record<string, string> = {
  agent: "Agent",
  group: "Group",
  human: "Human",
};

export function formatSubjectKind(kind: string): string {
  return SUBJECT_KIND_LABELS[kind] ?? kind;
}

export function FilterPill({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`cursor-pointer rounded-full px-3 py-1 text-xs font-medium transition-colors ${
        active
          ? "bg-kumo-tint text-kumo-default ring-kumo-focus/40 ring"
          : "bg-kumo-elevated/60 text-kumo-subtle hover:bg-kumo-elevated hover:text-kumo-default"
      }`}
    >
      {children}
    </button>
  );
}

export function FieldCard({ children }: { children: ReactNode }) {
  return (
    <div className="bg-kumo-elevated/40 ring-kumo-hairline divide-kumo-hairline flex flex-col divide-y rounded-lg ring">
      {children}
    </div>
  );
}

export function FieldRow({
  label,
  badge,
  value,
  actions,
  children,
}: {
  label: string;
  badge?: ReactNode;
  value?: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2 px-4 py-3">
      <div className="flex items-center justify-between gap-4">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="text-kumo-subtle text-xs font-medium uppercase tracking-wide">
              {label}
            </span>
            {badge}
          </div>
          {value !== undefined ? <div className="mt-0.5">{value}</div> : null}
        </div>
        {actions ? (
          <div className="flex shrink-0 items-center gap-2">{actions}</div>
        ) : null}
      </div>
      {children}
    </div>
  );
}
