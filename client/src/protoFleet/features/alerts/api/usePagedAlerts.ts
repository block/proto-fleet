import { useCallback, useEffect, useRef, useState } from "react";

import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isAuthOrPermissionError, isPermissionDeniedError } from "@/protoFleet/api/requestErrors";
import * as api from "@/protoFleet/features/alerts/api/alertsApi";
import type { AlertHistoryEntry } from "@/protoFleet/features/alerts/types";
import { useAuthErrors } from "@/protoFleet/store";

const PAGE_SIZE = 50;

// A failed first page leaves no cursor for Load more to retry with, so it self-heals with capped backoff.
const FIRST_PAGE_RETRY_MS = 2_000;
const FIRST_PAGE_RETRY_MAX_MS = 60_000;

// Which alerts the pages cover: no filter is the whole history feed, an alert name with active_only is one
// alert's firing instances.
export interface PagedAlertsFilter {
  active_only?: boolean;
  alert_name?: string;
  // Presence matters: undefined matches any group, "" matches the rows whose rule label is absent.
  rule_group?: string;
}

export interface UsePagedAlertsResult {
  items: AlertHistoryEntry[];
  loading: boolean;
  error: string | null;
  // Denial sets error too; this flag lets a caller that expects it (the alert:read grant was revoked
  // after login, so the client's cached permission gate still passes) degrade instead of erroring.
  denied: boolean;
  hasMore: boolean;
  loadMore: () => void;
}

export interface UsePagedAlertsOptions {
  // Off where the caller hides the alerts surface, so a gated table costs no request.
  enabled?: boolean;
}

// The zero-value result, for callers that swap the live feed out when it is gated off.
export const EMPTY_PAGED_ALERTS: UsePagedAlertsResult = {
  items: [],
  loading: false,
  error: null,
  denied: false,
  hasMore: false,
  loadMore: () => {},
};

// Keyset-paged because an outage's affected set is as large as the fleet, with one cursor per hook instance so
// tables page independently. A read, not a poll: rows that start firing behind the cursor land on remount.
export function usePagedAlerts(
  filter: PagedAlertsFilter,
  errorFallback: string,
  { enabled = true }: UsePagedAlertsOptions = {},
): UsePagedAlertsResult {
  const [items, setItems] = useState<AlertHistoryEntry[]>([]);
  const [cursor, setCursor] = useState("");
  // Starts true for the first page; loadMore raises it again from its own click handler.
  const [loading, setLoading] = useState(enabled);
  const [error, setError] = useState<string | null>(null);
  const [denied, setDenied] = useState(false);
  const { handleAuthErrors } = useAuthErrors();

  // Destructured to primitives so a caller's inline filter object doesn't refetch on every render.
  const { active_only: activeOnly, alert_name: alertName, rule_group: ruleGroup } = filter;

  // Bumped on disable and on every new fetch, so a stale response cannot overwrite newer rows or cursor.
  const requestIdRef = useRef(0);
  const retryDelayRef = useRef(FIRST_PAGE_RETRY_MS);
  const retryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const clearRetry = useCallback(() => {
    if (retryTimerRef.current !== null) {
      clearTimeout(retryTimerRef.current);
      retryTimerRef.current = null;
    }
  }, []);
  // Run when the fetch surface goes away (disable, unmount, re-fetch): a late response cannot
  // repopulate the feed and a late failure cannot schedule a fresh retry.
  const cancelPending = useCallback(() => {
    requestIdRef.current++;
    clearRetry();
  }, [clearRetry]);
  // The scheduled retry dereferences this at fire time (useActivity's fetchRef pattern), so it calls the
  // current loadPage without the callback closing over itself.
  const loadPageRef = useRef<(pageCursor?: string) => Promise<void>>(async () => {});

  // A cursor appends, no cursor replaces: the first page of a newly mounted table.
  const loadPage = useCallback(
    async (pageCursor?: string) => {
      const requestId = ++requestIdRef.current;
      // A new request supersedes any scheduled first-page retry.
      clearRetry();
      try {
        const page = await api.listHistory({
          active_only: activeOnly,
          alert_name: alertName,
          rule_group: ruleGroup,
          page_size: PAGE_SIZE,
          before_id: pageCursor,
        });
        if (requestId !== requestIdRef.current) return;
        retryDelayRef.current = FIRST_PAGE_RETRY_MS;
        setItems((current) => (pageCursor ? [...current, ...page.alerts] : page.alerts));
        setCursor(page.next_cursor);
        // Clear a previous page's failure so a successful retry doesn't leave the callout up.
        setError(null);
        setDenied(false);
      } catch (err) {
        if (requestId !== requestIdRef.current) return;
        // An expired session routes through the app's auth handler (logout); rows fetched under a
        // now-invalid session or revoked grant must not stay visible or resumable.
        handleAuthErrors({
          error: err,
          onError: (e) => {
            if (isAuthOrPermissionError(e)) {
              setItems([]);
              setCursor("");
              setDenied(isPermissionDeniedError(e));
            } else if (pageCursor === undefined) {
              // Retried in the background without raising loading, so the error stays visible and the
              // merged feed keeps treating the empty feed as exhausted rather than re-blocking on it.
              const delay = retryDelayRef.current;
              retryDelayRef.current = Math.min(delay * 2, FIRST_PAGE_RETRY_MAX_MS);
              retryTimerRef.current = setTimeout(() => void loadPageRef.current(), delay);
            }
            setError(getErrorMessage(e, errorFallback));
          },
        });
      } finally {
        if (requestId === requestIdRef.current) {
          setLoading(false);
        }
      }
    },
    [activeOnly, alertName, clearRetry, errorFallback, handleAuthErrors, ruleGroup],
  );

  useEffect(() => {
    loadPageRef.current = loadPage;
  }, [loadPage]);

  useEffect(() => {
    if (!enabled) {
      cancelPending();
      return;
    }
    // Awaited in a wrapper rather than called bare, which react-hooks reads as setState during the effect.
    const loadFirstPage = async () => {
      // Raised here for a re-enabled fetch; the initial state already covers the mount.
      setLoading(true);
      await loadPage();
    };
    void loadFirstPage();
    return cancelPending;
  }, [enabled, loadPage, cancelPending]);

  const loadMore = useCallback(() => {
    if (!cursor || loading) return;
    setLoading(true);
    void loadPage(cursor);
  }, [cursor, loadPage, loading]);

  // The server issues a cursor only while rows remain, so it is the one signal for both paging and the button.
  // An invalidated request's finally cannot settle loading, so a disabled hook reports it settled itself.
  return { items, loading: enabled && loading, error, denied, hasMore: cursor !== "", loadMore };
}
