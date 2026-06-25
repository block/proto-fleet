import { useCallback, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { useAlertHistory } from "@/protoFleet/features/alerts/api/useAlertHistory";
import type { AlertHistoryEntry } from "@/protoFleet/features/alerts/types";
import { usePoll } from "@/shared/hooks/usePoll";

export interface UseActiveAlertsResult {
  alerts: AlertHistoryEntry[];
  loading: boolean;
  error: string | null;
  // A site-scoped alert:read grant clears the dashboard's flat permission gate but is denied this
  // org-scoped RPC; callers suppress the card on denial rather than surfacing an error.
  denied: boolean;
  hasMore: boolean;
}

// Owns the always-on active-alert poll for the dashboard card: the server returns the current firing
// set (latest row per alert), so there is no paging here.
export function useActiveAlerts(): UseActiveAlertsResult {
  const { history, historyHasMore, historyLoading, refreshHistory } = useAlertHistory(true);

  const [error, setError] = useState<string | null>(null);
  const [denied, setDenied] = useState(false);

  const fetchData = useCallback(async () => {
    try {
      await refreshHistory();
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.PermissionDenied) {
        setDenied(true);
        return;
      }
      setError(getErrorMessage(err, "Failed to load active alerts"));
    }
  }, [refreshHistory]);

  // Stop polling once denied: the card unmounts to null but this hook stays mounted, so the poll
  // would otherwise keep hitting the org-scoped RPC the grant can't reach.
  usePoll({ fetchData, poll: true, pollIntervalMs: POLL_INTERVAL_MS, enabled: !denied });

  return { alerts: history, loading: historyLoading, error, denied, hasMore: historyHasMore };
}
