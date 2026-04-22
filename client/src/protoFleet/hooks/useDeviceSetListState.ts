import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import {
  SortDirection as ProtoSortDirection,
  type SortConfig,
  SortConfigSchema,
  SortField,
} from "@/protoFleet/api/generated/common/v1/sort_pb";
import type { DeviceSet, DeviceSetStats } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import type { ListDeviceSetsProps } from "@/protoFleet/api/useDeviceSets";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import type { DeviceSetColumn } from "@/protoFleet/components/DeviceSetList";
import { SORT_ASC, type SortDirection } from "@/shared/components/List/types";

const SORT_FIELD_MAP: Partial<Record<DeviceSetColumn, SortField>> = {
  name: SortField.NAME,
  zone: SortField.LOCATION,
  miners: SortField.DEVICE_COUNT,
  issues: SortField.ISSUE_COUNT,
};

const SORT_COLUMN_MAP: Partial<Record<SortField, DeviceSetColumn>> = {
  [SortField.NAME]: "name",
  [SortField.LOCATION]: "zone",
  [SortField.DEVICE_COUNT]: "miners",
  [SortField.ISSUE_COUNT]: "issues",
};

function toProtoSort(field: DeviceSetColumn, direction: SortDirection): SortConfig {
  return create(SortConfigSchema, {
    field: SORT_FIELD_MAP[field] ?? SortField.NAME,
    direction: direction === SORT_ASC ? ProtoSortDirection.ASC : ProtoSortDirection.DESC,
  });
}

const DEFAULT_SORT = toProtoSort("name", SORT_ASC);

type ListFn = (props: ListDeviceSetsProps) => Promise<void>;

export function useDeviceSetListState(
  listFn: ListFn,
  pageSize: number,
  getErrorComponentTypes?: () => number[],
  getZones?: () => string[],
) {
  const { getDeviceSetStats } = useDeviceSets();
  const [deviceSets, setDeviceSets] = useState<DeviceSet[]>([]);
  const [statsMap, setStatsMap] = useState<Map<bigint, DeviceSetStats>>(new Map());
  const [isLoading, setIsLoading] = useState(true);
  const [hasEverLoaded, setHasEverLoaded] = useState(false);
  const [hasCompletedInitialFetch, setHasCompletedInitialFetch] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Pagination state
  const [currentPage, setCurrentPage] = useState(0);
  const [cursorHistory, setCursorHistory] = useState<(string | undefined)[]>([undefined]);
  const [hasNextPage, setHasNextPage] = useState(false);
  const [totalCount, setTotalCount] = useState(0);

  // Sort state
  const [sortConfig, setSortConfig] = useState<SortConfig>(DEFAULT_SORT);
  const sortRef = useRef(sortConfig);
  useEffect(() => {
    sortRef.current = sortConfig;
  }, [sortConfig]);

  const listRequestId = useRef(0);
  const statsRequestId = useRef(0);

  const fetchStats = useCallback(
    (items: DeviceSet[]) => {
      if (items.length === 0) return;
      const requestId = ++statsRequestId.current;
      const ids = items.map((c) => c.id);
      getDeviceSetStats({
        deviceSetIds: ids,
        onSuccess: (stats) => {
          if (requestId !== statsRequestId.current) return;
          const map = new Map<bigint, DeviceSetStats>();
          for (const s of stats) {
            map.set(s.deviceSetId, s);
          }
          setStatsMap(map);
        },
      });
    },
    [getDeviceSetStats],
  );

  const fetchPage = useCallback(
    (page: number, pageToken?: string) => {
      const requestId = ++listRequestId.current;
      setIsLoading(true);
      setError(null);
      listFn({
        pageSize,
        pageToken,
        sort: sortRef.current,
        errorComponentTypes: getErrorComponentTypes?.() ?? [],
        zones: getZones?.() ?? [],
        onSuccess: (items, nextPageToken, total) => {
          if (requestId !== listRequestId.current) return;
          if (total > 0) setHasEverLoaded(true);
          setHasCompletedInitialFetch(true);
          setDeviceSets(items);
          fetchStats(items);
          setCurrentPage(page);
          setHasNextPage(!!nextPageToken);
          setTotalCount(total);
          if (nextPageToken) {
            setCursorHistory((prev) => {
              const next = [...prev];
              next[page + 1] = nextPageToken;
              return next;
            });
          }
        },
        onError: (message) => {
          if (requestId !== listRequestId.current) return;
          setError(message);
        },
        onFinally: () => {
          if (requestId !== listRequestId.current) return;
          setIsLoading(false);
        },
      });
    },
    [listFn, pageSize, fetchStats, getErrorComponentTypes, getZones],
  );

  const resetAndFetch = useCallback(() => {
    setCurrentPage(0);
    setCursorHistory([undefined]);
    setHasNextPage(false);
    fetchPage(0, undefined);
  }, [fetchPage]);

  useEffect(() => {
    resetAndFetch();
  }, [resetAndFetch]);

  const handleSort = useCallback(
    (field: DeviceSetColumn, direction: SortDirection) => {
      const newSort = toProtoSort(field, direction);
      setSortConfig(newSort);
      sortRef.current = newSort;
      setCurrentPage(0);
      setCursorHistory([undefined]);
      setHasNextPage(false);
      fetchPage(0, undefined);
    },
    [fetchPage],
  );

  const handleNextPage = useCallback(() => {
    const nextCursor = cursorHistory[currentPage + 1];
    if (nextCursor) {
      fetchPage(currentPage + 1, nextCursor);
    }
  }, [cursorHistory, currentPage, fetchPage]);

  const handlePrevPage = useCallback(() => {
    if (currentPage > 0) {
      fetchPage(currentPage - 1, cursorHistory[currentPage - 1]);
    }
  }, [currentPage, cursorHistory, fetchPage]);

  // Keep refs for polling to avoid stale closures
  const currentPageRef = useRef(currentPage);
  const cursorHistoryRef = useRef(cursorHistory);
  useEffect(() => {
    currentPageRef.current = currentPage;
  }, [currentPage]);
  useEffect(() => {
    cursorHistoryRef.current = cursorHistory;
  }, [cursorHistory]);

  const refreshCurrentPage = useCallback(() => {
    fetchPage(currentPageRef.current, cursorHistoryRef.current[currentPageRef.current]);
  }, [fetchPage]);

  const currentSort = useMemo(() => {
    const field = SORT_COLUMN_MAP[sortConfig.field] ?? "name";
    const direction: SortDirection = sortConfig.direction === ProtoSortDirection.DESC ? "desc" : "asc";
    return { field, direction };
  }, [sortConfig]);

  return {
    deviceSets,
    statsMap,
    isLoading,
    hasEverLoaded,
    hasCompletedInitialFetch,
    error,
    currentSort,
    currentPage,
    hasNextPage,
    totalCount,
    handleSort,
    handleNextPage,
    handlePrevPage,
    resetAndFetch,
    refreshCurrentPage,
  };
}
