import { useCallback, useEffect, useRef, useState } from "react";
import { toTicketItem } from "../mappers";
import type { TicketItem, TicketStats } from "../types";
import type { TicketFilter } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { SortDirection, TicketSortField } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { type BulkTicketMutation, useMaintenanceApi } from "@/protoFleet/api/maintenance";

export const useTicketQueue = (initialFilter: Partial<TicketFilter> = {}) => {
  const { listTickets, getStats, bulkUpdate: sendBulkUpdate } = useMaintenanceApi();
  const [data, setData] = useState<TicketItem[]>([]);
  const [stats, setStats] = useState<TicketStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [total, setTotal] = useState(0);
  const [nextPageToken, setNextPageToken] = useState("");
  const [filter, setFilterState] = useState(initialFilter);
  const controller = useRef<AbortController | undefined>(undefined);
  const sequence = useRef(0);
  const load = useCallback(
    async (append = false) => {
      controller.current?.abort();
      const current = new AbortController();
      controller.current = current;
      const request = ++sequence.current;
      setLoading(true);
      setError(null);
      const token = append ? nextPageToken : "";
      await Promise.all([
        listTickets({
          filter,
          sortField: TicketSortField.CREATED_AT,
          sortDirection: SortDirection.DESC,
          pageSize: 50,
          pageToken: token,
          signal: current.signal,
          onSuccess: (response) => {
            if (request !== sequence.current) return;
            setData((old) =>
              append ? [...old, ...response.tickets.map(toTicketItem)] : response.tickets.map(toTicketItem),
            );
            setTotal(response.totalCount);
            setNextPageToken(response.nextPageToken);
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
    [filter, getStats, listTickets, nextPageToken],
  );
  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (active) void load();
    });
    return () => {
      active = false;
      controller.current?.abort();
    };
  }, [filter]); // eslint-disable-line react-hooks/exhaustive-deps
  const setFilter = useCallback((value: Partial<TicketFilter>) => {
    setNextPageToken("");
    setFilterState(value);
  }, []);
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
      if (ok) await load();
      return ok;
    },
    [load, sendBulkUpdate],
  );
  return {
    data,
    stats,
    loading,
    error,
    total,
    nextPageToken,
    filter,
    setFilter,
    refresh: () => load(),
    loadMore: () => load(true),
    bulkUpdate,
  };
};
