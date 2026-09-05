import { useCallback, useEffect, useRef, useState } from "react";
import { toTicketItem } from "../mappers";
import type { TicketItem, TicketStats } from "../types";
import type { TicketFilter } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { SortDirection, TicketSortField } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { type BulkTicketMutation, useMaintenanceApi } from "@/protoFleet/api/maintenance";

const PAGE_SIZE = 50;
const POLL_INTERVAL_MS = 15_000;

export const useTicketQueue = (initialFilter: Partial<TicketFilter> = {}) => {
  const { listTickets, getStats, bulkUpdate: sendBulkUpdate, updateTicket, deleteTicket } = useMaintenanceApi();
  const [data, setData] = useState<TicketItem[]>([]);
  const [stats, setStats] = useState<TicketStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [total, setTotal] = useState(0);
  const [nextPageToken, setNextPageToken] = useState("");
  const [currentPage, setCurrentPage] = useState(0);
  const [filter, setFilterState] = useState(initialFilter);
  const [sortField, setSortField] = useState(TicketSortField.CREATED_AT);
  const [sortDirection, setSortDirection] = useState(SortDirection.DESC);
  const controller = useRef<AbortController | undefined>(undefined);
  const sequence = useRef(0);
  const currentPageRef = useRef(0);
  const cursorHistoryRef = useRef<string[]>([""]);
  const loadedBoardPageCountRef = useRef(1);

  const load = useCallback(
    async (page = 0, pageToken = "", append = false) => {
      controller.current?.abort();
      const current = new AbortController();
      controller.current = current;
      const request = ++sequence.current;
      setLoading(true);
      setError(null);
      let loadedRowCount: number | null = null;
      let loadedNextPageToken = "";
      await Promise.all([
        listTickets({
          filter,
          sortField,
          sortDirection,
          pageSize: PAGE_SIZE,
          pageToken,
          signal: current.signal,
          onSuccess: (response) => {
            if (request !== sequence.current) return;
            const mapped = response.tickets.map(toTicketItem);
            loadedRowCount = mapped.length;
            loadedNextPageToken = response.nextPageToken;
            setData((old) => (append ? [...old, ...mapped] : mapped));
            setTotal(response.totalCount);
            setNextPageToken(response.nextPageToken);
            if (!append) {
              setCurrentPage(page);
              currentPageRef.current = page;
              if (response.nextPageToken) {
                const cursors = [...cursorHistoryRef.current];
                cursors[page + 1] = response.nextPageToken;
                cursorHistoryRef.current = cursors;
              }
            }
          },
          onError: setError,
        }),
        append
          ? Promise.resolve()
          : getStats({
              filter,
              signal: current.signal,
              onSuccess: (value) => {
                if (request !== sequence.current) return;
                setStats({
                  openCount: value.openCount,
                  inProgressCount: value.inProgressCount,
                  onHoldCount: value.onHoldCount,
                  sentToVendorCount: value.sentToVendorCount,
                  overdueCount: value.overdueCount,
                  urgentCount: value.urgentCount,
                });
              },
              onError: setError,
            }),
      ]);
      if (request === sequence.current) setLoading(false);
      return loadedRowCount === null ? null : { rowCount: loadedRowCount, nextPageToken: loadedNextPageToken };
    },
    [filter, getStats, listTickets, sortDirection, sortField],
  );

  const resetPagination = useCallback(async () => {
    setCurrentPage(0);
    currentPageRef.current = 0;
    cursorHistoryRef.current = [""];
    loadedBoardPageCountRef.current = 1;
    setNextPageToken("");
    await load(0, "");
  }, [load]);

  const refreshCurrentView = useCallback(async () => {
    const boardPageCount = loadedBoardPageCountRef.current;
    if (boardPageCount <= 1) {
      const result = await load(currentPageRef.current, cursorHistoryRef.current[currentPageRef.current]);
      return result?.rowCount ?? null;
    }

    let pageToken = "";
    let refreshedPages = 0;
    let loadedRows = 0;
    for (let page = 0; page < boardPageCount; page += 1) {
      const result = await load(0, pageToken, page > 0);
      if (result === null) return null;
      refreshedPages += 1;
      loadedRows += result.rowCount;
      if (!result.nextPageToken) break;
      pageToken = result.nextPageToken;
    }
    loadedBoardPageCountRef.current = Math.max(1, refreshedPages);
    return loadedRows;
  }, [load]);

  const refreshValidView = useCallback(async () => {
    const page = currentPageRef.current;
    const loadedRowCount = await refreshCurrentView();
    if (loadedRowCount === 0 && page > 0 && currentPageRef.current === page) {
      const previous = page - 1;
      const previousToken = cursorHistoryRef.current[previous];
      cursorHistoryRef.current = cursorHistoryRef.current.slice(0, page);
      loadedBoardPageCountRef.current = 1;
      await load(previous, previousToken);
    }
    return loadedRowCount;
  }, [load, refreshCurrentView]);

  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (active) void resetPagination();
    });
    return () => {
      active = false;
      controller.current?.abort();
    };
  }, [resetPagination]);

  useEffect(() => {
    let active = true;
    let timeoutId: ReturnType<typeof setTimeout> | undefined;
    const scheduleNext = () => {
      timeoutId = setTimeout(async () => {
        if (!active) return;
        try {
          await refreshValidView();
        } catch {
          // RPC adapters report request failures through load's onError callbacks.
        } finally {
          if (active) scheduleNext();
        }
      }, POLL_INTERVAL_MS);
    };
    scheduleNext();
    return () => {
      active = false;
      if (timeoutId !== undefined) clearTimeout(timeoutId);
    };
  }, [refreshValidView]);

  const setFilter = useCallback((value: Partial<TicketFilter>) => {
    setFilterState(value);
  }, []);

  const setUrgent = useCallback(
    async (ticketId: string, urgent: boolean) => {
      let ok = false;
      await updateTicket({
        id: BigInt(ticketId),
        urgent,
        onSuccess: () => {
          ok = true;
        },
        onError: setError,
      });
      if (ok) await refreshValidView();
      return ok;
    },
    [refreshValidView, updateTicket],
  );

  const remove = useCallback(
    async (ticketId: string) => {
      let ok = false;
      await deleteTicket({
        id: BigInt(ticketId),
        onSuccess: () => {
          ok = true;
        },
        onError: setError,
      });
      if (ok) await refreshValidView();
      return ok;
    },
    [deleteTicket, refreshValidView],
  );

  const bulkUpdate = useCallback(
    async (ticketIds: string[], mutation: BulkTicketMutation, clearAssignee = false) => {
      let ok = false;
      await sendBulkUpdate({
        ticketIds: ticketIds.map(BigInt),
        mutation,
        clearAssignee,
        onSuccess: () => {
          ok = true;
        },
        onError: setError,
      });
      if (ok) await refreshValidView();
      return ok;
    },
    [refreshValidView, sendBulkUpdate],
  );

  return {
    data,
    stats,
    loading,
    error,
    total,
    nextPageToken,
    currentPage,
    hasPreviousPage: currentPage > 0,
    filter,
    setFilter,
    sortField,
    sortDirection,
    setSort: (field: TicketSortField, direction: SortDirection) => {
      setSortField(field);
      setSortDirection(direction);
    },
    refresh: refreshValidView,
    resetPagination,
    nextPage: async () => {
      const token = cursorHistoryRef.current[currentPageRef.current + 1] ?? nextPageToken;
      loadedBoardPageCountRef.current = 1;
      if (token) await load(currentPageRef.current + 1, token);
    },
    previousPage: async () => {
      const previous = currentPageRef.current - 1;
      loadedBoardPageCountRef.current = 1;
      if (previous >= 0) await load(previous, cursorHistoryRef.current[previous]);
    },
    loadMore: async () => {
      if (!nextPageToken) return;
      const result = await load(currentPageRef.current, nextPageToken, true);
      if (result !== null) loadedBoardPageCountRef.current += 1;
    },
    setUrgent,
    remove,
    bulkUpdate,
  };
};
