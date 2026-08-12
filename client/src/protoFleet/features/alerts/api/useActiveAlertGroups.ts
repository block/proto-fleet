import { useCallback, useState } from "react";

import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isPermissionDeniedError } from "@/protoFleet/api/requestErrors";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import * as api from "@/protoFleet/features/alerts/api/alertsApi";
import type { ActiveAlertGroup } from "@/protoFleet/features/alerts/types";
import { usePoll } from "@/shared/hooks/usePoll";

export interface UseActiveAlertGroupsResult {
  groups: ActiveAlertGroup[];
  error: string | null;
  // A site-scoped alert:read grant clears the shell's flat permission gate but is denied this org-scoped RPC.
  denied: boolean;
  hasMore: boolean;
}

export interface UseActiveAlertGroupsOptions {
  // Off where the alerts surface is hidden or the viewer can't reach the RPC, since this polls on every route.
  enabled?: boolean;
}

// Owns the active-alert poll behind the header pill. The server rolls the firing set up per rule, so the
// response stays a handful of rows however many miners an outage covers.
export function useActiveAlertGroups({ enabled = true }: UseActiveAlertGroupsOptions = {}): UseActiveAlertGroupsResult {
  const [groups, setGroups] = useState<ActiveAlertGroup[]>([]);
  const [hasMore, setHasMore] = useState(false);
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
    }
  }, []);

  // Stop polling once denied: the pill hides itself but this hook stays mounted, so the poll
  // would otherwise keep hitting the org-scoped RPC the grant can't reach.
  usePoll({ fetchData, poll: true, pollIntervalMs: POLL_INTERVAL_MS, enabled: enabled && !denied });

  return { groups, error, denied, hasMore };
}
