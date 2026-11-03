import { useEffect, useRef } from "react";
import { requests, watchRequests } from "./origin";
import { agentApprovalAlertsEnabled, notifyAgentApprovalRequest } from "./agent-approval-alerts";

/**
 * Watches open approval requests and fires browser notifications when the
 * count increases — for Grok/cloud agents on level1 grants while the human
 * is on another tab or app.
 */
export function useAgentApprovalAlerts(enabled: boolean): void {
  const prevOpen = useRef<number | null>(null);

  useEffect(() => {
    if (!enabled || !agentApprovalAlertsEnabled()) {
      prevOpen.current = null;
      return;
    }

    let live = true;

    const sync = async () => {
      const res = await requests("open");
      if (!live) {
        return;
      }
      if (res.response?.status === 403) {
        prevOpen.current = null;
        return;
      }
      if (res.error || !res.data) {
        return;
      }
      const n = res.data.requests.length;
      if (prevOpen.current !== null && n > prevOpen.current) {
        const newest = res.data.requests[0];
        if (newest) {
          notifyAgentApprovalRequest(newest.agent_id, newest.item_id);
        }
      }
      prevOpen.current = n;
    };

    void sync();
    const stop = watchRequests(() => {
      void sync();
    });

    return () => {
      live = false;
      stop();
    };
  }, [enabled]);
}
