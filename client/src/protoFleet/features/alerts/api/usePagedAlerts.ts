import { useCallback, useEffect, useState } from "react";

import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import * as api from "@/protoFleet/features/alerts/api/alertsApi";
import type { AlertHistoryEntry } from "@/protoFleet/features/alerts/types";

const PAGE_SIZE = 50;

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
  hasMore: boolean;
  loadMore: () => void;
}

// Keyset-paged because an outage's affected set is as large as the fleet, with one cursor per hook instance so
// tables page independently. A read, not a poll: rows that start firing behind the cursor land on remount.
export function usePagedAlerts(filter: PagedAlertsFilter, errorFallback: string): UsePagedAlertsResult {
  const [items, setItems] = useState<AlertHistoryEntry[]>([]);
  const [cursor, setCursor] = useState("");
  // Starts true for the first page; loadMore raises it again from its own click handler.
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Destructured to primitives so a caller's inline filter object doesn't refetch on every render.
  const { active_only: activeOnly, alert_name: alertName, rule_group: ruleGroup } = filter;

  // A cursor appends, no cursor replaces: the first page of a newly mounted table.
  const loadPage = useCallback(
    async (pageCursor?: string) => {
      try {
        const page = await api.listHistory({
          active_only: activeOnly,
          alert_name: alertName,
          rule_group: ruleGroup,
          page_size: PAGE_SIZE,
          before_id: pageCursor,
        });
        setItems((current) => (pageCursor ? [...current, ...page.alerts] : page.alerts));
        setCursor(page.next_cursor);
        // Clear a previous page's failure so a successful retry doesn't leave the callout up.
        setError(null);
      } catch (err) {
        setError(getErrorMessage(err, errorFallback));
      } finally {
        setLoading(false);
      }
    },
    [activeOnly, alertName, errorFallback, ruleGroup],
  );

  useEffect(() => {
    // Awaited in a wrapper rather than called bare, which react-hooks reads as setState during the effect.
    const loadFirstPage = async () => {
      await loadPage();
    };
    void loadFirstPage();
  }, [loadPage]);

  const loadMore = useCallback(() => {
    if (!cursor || loading) return;
    setLoading(true);
    void loadPage(cursor);
  }, [cursor, loadPage, loading]);

  // The server issues a cursor only while rows remain, so it is the one signal for both paging and the button.
  return { items, loading, error, hasMore: cursor !== "", loadMore };
}
