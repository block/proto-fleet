import { useCallback, useEffect, useRef, useState } from "react";
import { toTicketItem } from "../mappers";
import type { TicketItem, TicketStats } from "../types";
import type { TicketFilter } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { SortDirection, TicketSortField } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { type BulkTicketMutation, type TicketVersion, useMaintenanceApi } from "@/protoFleet/api/maintenance";

const PAGE_SIZE = 50;
export const useTicketQueue = (initialFilter: Partial<TicketFilter> = {}) => {
  const { listTickets, getStats, bulkUpdate: sendBulkUpdate, updateTicket } = useMaintenanceApi();
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
  const viewGeneration = useRef(0);
  const [renderedViewGeneration, setRenderedViewGeneration] = useState(0);

  const load = useCallback(
    async (page = 0, pageToken = "", append = false) => {
      controller.current?.abort();
      const current = new AbortController();
      controller.current = current;
      const request = ++sequence.current;
      setLoading(true);
      setError(null);
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
    },
    [filter, getStats, listTickets, sortDirection, sortField],
  );

  const resetPagination = useCallback(async () => {
    setCurrentPage(0);
    currentPageRef.current = 0;
    cursorHistoryRef.current = [""];
    setNextPageToken("");
    await load(0, "");
  }, [load]);

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

  const setFilter = useCallback((value: Partial<TicketFilter>) => {
    viewGeneration.current += 1;
    setRenderedViewGeneration(viewGeneration.current);
    setFilterState(value);
  }, []);

  const setUrgent = useCallback(
    async (ticketId: string, urgent: boolean) => {
      const mutationView = viewGeneration.current;
      let ok = false;
      const expectedUpdatedAt = data.find((ticket) => ticket.id === ticketId)?.updatedAtSnapshot;
      if (!expectedUpdatedAt) {
        setError("Ticket version is unavailable. Refresh before updating.");
        return false;
      }
      await updateTicket({
        id: BigInt(ticketId),
        expectedUpdatedAt,
        urgent,
        onSuccess: () => {
          ok = true;
        },
        onError: setError,
      });
      if (ok && mutationView === viewGeneration.current) await resetPagination();
      return ok;
    },
    [data, resetPagination, updateTicket],
  );

  const bulkUpdate = useCallback(
    async (ticketIds: string[], mutation: BulkTicketMutation, clearAssignee = false, versions?: TicketVersion[]) => {
      const mutationView = viewGeneration.current;
      const expectedVersions =
        versions ??
        ticketIds.flatMap((id) => {
          const updatedAt = data.find((ticket) => ticket.id === id)?.updatedAtSnapshot;
          return updatedAt ? [{ ticketId: BigInt(id), updatedAt }] : [];
        });
      if (expectedVersions.length !== ticketIds.length) {
        setError("Ticket version is unavailable. Refresh the queue before updating.");
        return false;
      }
      let ok = false;
      await sendBulkUpdate({
        ticketIds: ticketIds.map(BigInt),
        mutation,
        clearAssignee,
        expectedVersions,
        onSuccess: () => {
          ok = true;
        },
        onError: setError,
      });
      if (ok && mutationView === viewGeneration.current) await resetPagination();
      return ok;
    },
    [data, resetPagination, sendBulkUpdate],
  );

  const setSort = useCallback((field: TicketSortField, direction: SortDirection) => {
    viewGeneration.current += 1;
    setRenderedViewGeneration(viewGeneration.current);
    setSortField(field);
    setSortDirection(direction);
  }, []);

  const refresh = useCallback(async () => {
    if (renderedViewGeneration !== viewGeneration.current) return null;
    return resetPagination();
  }, [resetPagination, renderedViewGeneration]);

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
    setSort,
    refresh,
    resetPagination,
    nextPage: async () => {
      const token = cursorHistoryRef.current[currentPageRef.current + 1] ?? nextPageToken;
      if (token) await load(currentPageRef.current + 1, token);
    },
    previousPage: async () => {
      const previous = currentPageRef.current - 1;
      if (previous >= 0) await load(previous, cursorHistoryRef.current[previous]);
    },
    loadMore: async () => {
      if (!nextPageToken) return;
      await load(currentPageRef.current, nextPageToken, true);
    },
    setUrgent,
    bulkUpdate,
  };
};
