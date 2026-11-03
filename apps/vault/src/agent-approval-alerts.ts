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
export function notifyAgentApprovalRequest(agentId: string, itemId: string): void {
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
  const body = itemId ? `Item ${itemId}` : "Open Veil → Requests to approve or deny.";
  const n = new Notification("Veil — agent approval", {
    body: agentId ? `Agent ${agentId} · ${body}` : body,
    tag: "veil-agent-approval",
  });
  n.onclick = () => {
    window.focus();
    window.location.assign("/requests");
  };
}
