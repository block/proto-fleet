import { useCallback, useRef, useState } from "react";

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

// A grant can be restored while the shell stays mounted, and no route change remounts this hook to notice. So a
// denial slows the poll rather than ending it: a grant that can't reach the RPC costs a request every few
// minutes instead of one every interval, and a restored one is picked up without a reload.
const DENIED_RETRY_MS = 5 * 60 * 1000;

// Owns the active-alert poll behind the header pill. The server rolls the firing set up per rule, so the
// response stays a handful of rows however many miners an outage covers.
export function useActiveAlertGroups({ enabled = true }: UseActiveAlertGroupsOptions = {}): UseActiveAlertGroupsResult {
  const [groups, setGroups] = useState<ActiveAlertGroup[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [denied, setDenied] = useState(false);

  // Disabling the poll stops it scheduling but can't recall the request already in flight, so leaving a
  // headerless route and coming straight back can overlap two. Whichever was issued last is the one that
  // describes the fleet now, so an earlier reply that lands after it is dropped rather than committed.
  const latestRequest = useRef(0);

  const fetchData = useCallback(async () => {
    const request = ++latestRequest.current;
    const superseded = () => request !== latestRequest.current;
    try {
      const page = await api.listActiveAlertGroups();
      if (superseded()) return;
      setGroups(page.groups);
      setHasMore(page.has_more);
      setError(null);
      setDenied(false);
    } catch (err) {
      // Before the denial check too: a stale denial would stop the poll for good on a grant already replaced.
      if (superseded()) return;
      if (isPermissionDeniedError(err)) {
        setDenied(true);
        return;
      }
      // Any other failure says nothing about the grant, so an earlier denial must not outlive it: the pill hides
      // itself while denied, which would swallow this error and hold the slow retry interval until a response
      // succeeds. A grant that really is denied re-asserts that on the next tick.
      setDenied(false);
      setError(getErrorMessage(err, "Failed to load active alerts"));
    }
  }, []);

  // Back off once denied: the pill hides itself but this hook stays mounted, so the poll
  // would otherwise keep hitting the org-scoped RPC the grant can't reach at header speed.
  usePoll({ fetchData, poll: true, pollIntervalMs: denied ? DENIED_RETRY_MS : POLL_INTERVAL_MS, enabled });

  return { groups, error, denied, hasMore };
}
