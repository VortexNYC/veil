const STORAGE_KEY = "veil.agentApprovalAlerts";

export function agentApprovalAlertsEnabled(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  return window.localStorage.getItem(STORAGE_KEY) === "1";
}

export function setAgentApprovalAlertsEnabled(on: boolean): void {
  if (typeof window === "undefined") {
    return;
  }
  if (on) {
    window.localStorage.setItem(STORAGE_KEY, "1");
  } else {
    window.localStorage.removeItem(STORAGE_KEY);
  }
}

export async function ensureNotificationPermission(): Promise<
  NotificationPermission | "unsupported"
> {
  if (typeof window === "undefined" || !("Notification" in window)) {
    return "unsupported";
  }
  if (Notification.permission === "granted") {
    return "granted";
  }
  if (Notification.permission === "denied") {
    return "denied";
  }
  return Notification.requestPermission();
}

/** Notify when a new open agent approval request appears (owner org). */
export function notifyAgentApprovalRequest(
  requestId: string,
  agentId: string,
  itemId: string,
  action?: string,
): void {
  if (!agentApprovalAlertsEnabled()) {
    return;
  }
  if (typeof window === "undefined" || !("Notification" in window)) {
    return;
  }
  if (Notification.permission !== "granted") {
    return;
  }
  if (document.visibilityState === "visible" && window.location.pathname.startsWith("/requests")) {
    return;
  }
  const isFill = action === "fill";
  const title = isFill ? "Veil login · approve (3/4)" : "Veil — agent approval";
  const who = agentId ? `Agent ${agentId}` : "An agent";
  const item = itemId ? `item ${itemId}` : "a credential";
  const body = isFill
    ? `${who} → ${item} · ${requestId}`
    : itemId
      ? `${who} · ${item}`
      : "Open Veil → Requests to approve or deny.";
  const n = new Notification(title, {
    body,
    tag: requestId || "veil-agent-approval",
  });
  n.onclick = () => {
    window.focus();
    window.location.assign("/requests");
  };
}
