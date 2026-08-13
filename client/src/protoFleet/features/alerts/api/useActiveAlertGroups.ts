import { useCallback, useState } from "react";

import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isPermissionDeniedError } from "@/protoFleet/api/requestErrors";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import * as api from "@/protoFleet/features/alerts/api/alertsApi";
import type { ActiveAlertGroup } from "@/protoFleet/features/alerts/types";
import { usePoll } from "@/shared/hooks/usePoll";

export interface UseActiveAlertGroupsResult {
  groups: ActiveAlertGroup[];
  // True until the first response lands; poll ticks after that refresh in place rather than re-rendering the
  // card (and its list) through a loading state on every interval.
  loading: boolean;
  error: string | null;
  // A site-scoped alert:read grant clears the dashboard's flat permission gate but is denied this
  // org-scoped RPC; callers suppress the card on denial rather than surfacing an error.
  denied: boolean;
  hasMore: boolean;
}

// Owns the always-on active-alert poll for the dashboard card. The server rolls the firing set up per rule,
// so the response stays a handful of rows however many miners an outage covers.
export function useActiveAlertGroups(): UseActiveAlertGroupsResult {
  const [groups, setGroups] = useState<ActiveAlertGroup[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [denied, setDenied] = useState(false);

  const fetchData = useCallback(async () => {
    try {
      const page = await api.listActiveAlertGroups();
      setGroups(page.groups);
      setHasMore(page.has_more);
      setError(null);
    } catch (err) {
      if (isPermissionDeniedError(err)) {
        setDenied(true);
        return;
      }
      setError(getErrorMessage(err, "Failed to load active alerts"));
    } finally {
      setLoading(false);
    }
  }, []);

  // Stop polling once denied: the card unmounts to null but this hook stays mounted, so the poll
  // would otherwise keep hitting the org-scoped RPC the grant can't reach.
  usePoll({ fetchData, poll: true, pollIntervalMs: POLL_INTERVAL_MS, enabled: !denied });

  return { groups, loading, error, denied, hasMore };
}
