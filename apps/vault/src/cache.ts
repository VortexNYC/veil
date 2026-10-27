import {
  agents,
  billing,
  events,
  grants,
  groups,
  items,
  recoveryShare,
  report,
  requests,
  shares,
  travelMode,
} from "./origin";
import type {
  Agent,
  ApprovalRequest,
  AuditEvent,
  BillingView,
  Grant,
  Group,
  Item,
  RecoveryShare,
  Share,
  VaultReport,
} from "@vortex-api/veil";

const mem = new Map<string, unknown>();

export function readCache<T>(key: string): T | null {
  if (mem.has(key)) {
    return mem.get(key) as T;
  }
  try {
    const raw = sessionStorage.getItem(key);
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw) as T;
    mem.set(key, parsed);
    return parsed;
  } catch {
    return null;
  }
}

export function writeCache<T>(key: string, value: T): void {
  mem.set(key, value);
  try {
    sessionStorage.setItem(key, JSON.stringify(value));
  } catch {
    // ignore storage quota errors
  }
}

export const CACHE_KEYS = {
  items: "veil.items.cache",
  shares: "veil.shares.cache",
  report: "veil.report.cache",
  grants: "veil.grants.cache",
  groups: "veil.groups.cache",
  requestsOpen: "veil.requests.open.cache",
  agents: "veil.agents.cache",
  audit: "veil.audit.cache",
  billing: "veil.billing.cache",
  travelMode: "veil.travel.cache",
  recoveryShare: "veil.recovery.cache",
  selectItemId: "veil.select_item_id",
} as const;

let preloading = false;

export function preloadVaultCache(): void {
  if (preloading) {
    return;
  }
  preloading = true;
  void Promise.allSettled([
    items().then((r) => {
      if (r.data) writeCache<Item[]>(CACHE_KEYS.items, r.data.items);
    }),
    shares().then((r) => {
      if (r.data) writeCache<Share[]>(CACHE_KEYS.shares, r.data.shares);
    }),
    report(false).then((r) => {
      if (r.data) writeCache<VaultReport>(CACHE_KEYS.report, r.data);
    }),
    grants().then((r) => {
      if (r.data) writeCache<Grant[]>(CACHE_KEYS.grants, r.data.grants);
    }),
    groups().then((r) => {
      if (r.data) writeCache<Group[]>(CACHE_KEYS.groups, r.data.groups);
    }),
    requests("open").then((r) => {
      if (r.data)
        writeCache<ApprovalRequest[]>(CACHE_KEYS.requestsOpen, r.data.requests);
    }),
    agents().then((r) => {
      if (r.data) writeCache<Agent[]>(CACHE_KEYS.agents, r.data.agents);
    }),
    events().then((r) => {
      if (r.data)
        writeCache<AuditEvent[]>(
          CACHE_KEYS.audit,
          [...r.data.events].reverse(),
        );
    }),
    billing().then((r) => {
      if (r.data) writeCache<BillingView>(CACHE_KEYS.billing, r.data);
    }),
    travelMode().then((r) => {
      if (r.data) writeCache<boolean>(CACHE_KEYS.travelMode, r.data.enabled);
    }),
    recoveryShare().then((r) => {
      if (r.data) writeCache<RecoveryShare>(CACHE_KEYS.recoveryShare, r.data);
    }),
  ]).finally(() => {
    preloading = false;
  });
}

export function fmtRelativeTime(iso: string | undefined): string {
  if (!iso) {
    return "—";
  }
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) {
    return iso;
  }
  const diffSec = Math.round((Date.now() - t) / 1000);
  if (diffSec < 45) {
    return "just now";
  }
  const mins = Math.floor(diffSec / 60);
  if (mins < 60) {
    return `${mins}m ago`;
  }
  const hours = Math.floor(mins / 60);
  if (hours < 24) {
    return `${hours}h ago`;
  }
  const days = Math.floor(hours / 24);
  if (days < 30) {
    return `${days}d ago`;
  }
  return new Date(t).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}
