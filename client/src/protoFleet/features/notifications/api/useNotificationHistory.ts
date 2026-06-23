import { useCallback, useMemo, useState } from "react";

import * as api from "@/protoFleet/features/notifications/api/notificationsApi";
import type { NotificationHistoryEntry } from "@/protoFleet/features/notifications/types";

const HISTORY_PAGE_SIZE = 50;

export interface UseNotificationHistoryResult {
  history: NotificationHistoryEntry[];
  historyHasMore: boolean;
  historyLoading: boolean;
  refreshHistory: () => Promise<void>;
  loadMoreHistory: () => Promise<void>;
}

// Feature-scoped history hook: each table instance owns its keyset cursor, so the dashboard card and the page list fetch independently.
// In `activeOnly` mode the server returns the current firing set (latest row per alert), so there is no client-side paging.
export function useNotificationHistory(activeOnly = false): UseNotificationHistoryResult {
  const [history, setHistory] = useState<NotificationHistoryEntry[]>([]);
  const [historyHasMore, setHistoryHasMore] = useState(false);
  const [historyLoading, setHistoryLoading] = useState(false);

  const refreshHistory = useCallback(async () => {
    setHistoryLoading(true);
    try {
      const page = activeOnly
        ? await api.listHistory({ active_only: true })
        : await api.listHistory({ page_size: HISTORY_PAGE_SIZE });
      setHistory(page.notifications);
      setHistoryHasMore(page.has_more);
    } finally {
      setHistoryLoading(false);
    }
  }, [activeOnly]);

  const loadMoreHistory = useCallback(async () => {
    if (activeOnly || !historyHasMore || historyLoading || history.length === 0) return;
    setHistoryLoading(true);
    try {
      const page = await api.listHistory({
        before_id: history[history.length - 1].id,
        page_size: HISTORY_PAGE_SIZE,
      });
      setHistory((current) => [...current, ...page.notifications]);
      setHistoryHasMore(page.has_more);
    } finally {
      setHistoryLoading(false);
    }
  }, [activeOnly, history, historyHasMore, historyLoading]);

  return useMemo(
    () => ({ history, historyHasMore, historyLoading, refreshHistory, loadMoreHistory }),
    [history, historyHasMore, historyLoading, refreshHistory, loadMoreHistory],
  );
}
