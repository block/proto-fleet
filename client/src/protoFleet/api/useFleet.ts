import { useCallback, useEffect, useRef, useState } from "react";
import { create, equals } from "@bufbuild/protobuf";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import { SortConfig, SortConfigSchema } from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  MinerListFilter,
  MinerListFilterSchema,
  MinerStateSnapshot,
  MinerStateSnapshotSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useAuthErrors } from "@/protoFleet/store";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";

type UseFleetOptions = {
  /**
   * Enables data fetching and streaming.
   * When false, this hook stays idle.
   * @default true
   */
  enabled?: boolean;
  filter?: MinerListFilter;
  /**
   * Sort configuration for ordering miners.
   * When undefined, uses default server-side ordering (discovery order).
   */
  sort?: SortConfig;
  pageSize?: number;
  pairingStatuses?: PairingStatus[];
};

// Constants to prevent re-renders from unstable default values
const DEFAULT_PAIRING_STATUSES: PairingStatus[] = [];
type PendingFetchMode = "refetch" | "refresh";

/**
 * Hook for managing fleet data with automatic loading, filtering, and pagination.
 *
 * @param options - Configuration options for the hook
 * @param options.filter - Optional filter to apply
 * @param options.pageSize - Number of miners to fetch per page (default: 20)
 *
 * @example
 * ```tsx
 * const { minerIds, miners, totalMiners, hasMore, isLoading, loadMore, refetch } = useFleet({
 *   filter: { status: [ComponentStatus.OK] }
 * });
 *
 * // With custom page size
 * const { minerIds, miners, totalMiners, hasMore, isLoading, loadMore, refetch } = useFleet({
 *   pageSize: 50
 * });
 *
 * // Load the next page (replaces current data)
 * if (hasMore) {
 *   loadMore();
 * }
 *
 * // Refetch current filter from scratch
 * refetch();
 * ```
 */
const useFleet = (options: UseFleetOptions = {}) => {
  const {
    enabled = true,
    filter,
    sort,
    pageSize = 20,
    pairingStatuses = DEFAULT_PAIRING_STATUSES, // Use stable reference to prevent re-renders
  } = options;
  const { handleAuthErrors } = useAuthErrors();

  // All state is local to this hook instance
  const [minerIds, setMinerIds] = useState<string[]>([]);
  const [miners, setMiners] = useState<Record<string, MinerStateSnapshot>>({});
  const [totalMiners, setTotalMiners] = useState(0);
  const [availableModels, setAvailableModels] = useState<string[]>([]);
  const [availableFirmwareVersions, setAvailableFirmwareVersions] = useState<string[]>([]);

  // Pagination state
  const [currentPage, setCurrentPage] = useState(0);
  // cursorHistory[i] = cursor to pass when fetching page i
  // cursorHistory[0] = undefined (first page needs no cursor)
  const [cursorHistory, setCursorHistory] = useState<(string | undefined)[]>([undefined]);

  // Internal state for the hook
  const [hasMore, setHasMore] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [hasInitialLoadCompleted, setHasInitialLoadCompleted] = useState(false);
  const [cursor, setCursor] = useState<string | undefined>();
  const pendingFetchModeRef = useRef<PendingFetchMode | null>(null);
  const latestRequestIdRef = useRef(0);

  // Fetch initial list using one-time query
  const fetchMinerList = useCallback(
    async (
      filter: MinerListFilter | undefined,
      sort: SortConfig | undefined,
      pageCursor?: string,
      fetchedPage?: number,
      isRefresh: boolean = false,
    ) => {
      if (!enabled) {
        return;
      }

      const requestId = ++latestRequestIdRef.current;
      setIsLoading(true);

      try {
        // Merge pairing statuses into the filter
        const filterWithPairingStatuses = filter ? { ...filter, pairingStatuses } : { pairingStatuses };

        const response = await fleetManagementClient.listMinerStateSnapshots({
          pageSize,
          cursor: pageCursor,
          filter: filterWithPairingStatuses,
          sort: sort ? [sort] : undefined,
        });

        const { miners, cursor: newCursor, totalMiners: responseTotalMiners, models, firmwareVersions } = response;

        if (requestId !== latestRequestIdRef.current) {
          return;
        }

        // Always replace (never append) for page-based pagination
        const ids = miners.map((miner) => miner.deviceIdentifier);
        const minersMap: Record<string, MinerStateSnapshot> = {};
        miners.forEach((miner) => {
          minersMap[miner.deviceIdentifier] = miner;
        });

        // Only update state if data actually changed — avoids unnecessary
        // re-renders of MinerList/deviceItems on every poll when data is unchanged.
        setMinerIds((prev) => {
          if (prev.length !== ids.length) return ids;
          for (let i = 0; i < ids.length; i++) {
            if (prev[i] !== ids[i]) return ids;
          }
          return prev;
        });
        setMiners((prev) => {
          const prevKeys = Object.keys(prev);
          if (prevKeys.length !== ids.length) return minersMap;
          for (const miner of miners) {
            const prevMiner = prev[miner.deviceIdentifier];
            if (!prevMiner || !equals(MinerStateSnapshotSchema, prevMiner, miner)) return minersMap;
          }
          return prev;
        });
        setTotalMiners(responseTotalMiners);

        // Update available models for filter dropdown
        if (models && models.length > 0) {
          setAvailableModels(models);
        }

        // Update available firmware versions for filter dropdown
        if (firmwareVersions && firmwareVersions.length > 0) {
          setAvailableFirmwareVersions(firmwareVersions);
        }

        // Store the response cursor for the next page
        if (fetchedPage !== undefined) {
          setCursorHistory((prev) => {
            const next = [...prev];
            next[fetchedPage + 1] = newCursor || undefined;
            return next;
          });
        }

        // Update internal state (both scopes)
        setCursor(newCursor || undefined);
        setHasMore(!!newCursor);
      } catch (error) {
        if (requestId !== latestRequestIdRef.current) {
          return;
        }
        handleAuthErrors({
          error: error,
          onError: (err) => {
            console.error("Error fetching miner list:", err);

            // Show toast for page 0 fetch errors (not subsequent pages)
            if (!pageCursor) {
              pushToast({
                status: TOAST_STATUSES.error,
                message: "Failed to load miners. Please try again.",
              });
            }
          },
        });
      } finally {
        if (requestId === latestRequestIdRef.current) {
          setIsLoading(false);

          // Mark initial load as completed when fetching page 0 (but not for refreshes)
          // This ensures UI doesn't get stuck in permanent loading state on error
          if (!pageCursor && !isRefresh) {
            setHasInitialLoadCompleted(true);
          }

          const pendingFetchMode = pendingFetchModeRef.current;
          if (pendingFetchMode !== null) {
            pendingFetchModeRef.current = null;

            if (pendingFetchMode === "refetch") {
              setCurrentPage(0);
              setCursorHistory([undefined]);
              void fetchMinerListRef.current(filterRef.current, sortRef.current, undefined, 0);
            } else {
              const currentCursor = cursorHistoryRef.current[currentPageRef.current];
              void fetchMinerListRef.current(
                filterRef.current,
                sortRef.current,
                currentCursor,
                currentPageRef.current,
                true,
              );
            }
          }
        }
      }
    },
    [enabled, pairingStatuses, pageSize, handleAuthErrors],
  );

  // Store fetchMinerList in a ref to avoid dependency issues
  const fetchMinerListRef = useRef(fetchMinerList);
  useEffect(() => {
    fetchMinerListRef.current = fetchMinerList;
  }, [fetchMinerList]);

  // Store filter in a ref for stable callbacks (refetch, loadMore)
  // This prevents callback recreation when filter object reference changes
  const filterRef = useRef(filter);
  useEffect(() => {
    filterRef.current = filter;
  }, [filter]);

  // Store sort in a ref for stable callbacks
  const sortRef = useRef(sort);
  useEffect(() => {
    sortRef.current = sort;
  }, [sort]);

  // Store cursor in a ref for stable loadMore callback
  const cursorRef = useRef(cursor);
  useEffect(() => {
    cursorRef.current = cursor;
  }, [cursor]);

  // Store isLoading in a ref for stable callbacks
  const isLoadingRef = useRef(isLoading);
  useEffect(() => {
    isLoadingRef.current = isLoading;
  }, [isLoading]);

  // Store hasMore in a ref for stable loadMore callback
  const hasMoreRef = useRef(hasMore);
  useEffect(() => {
    hasMoreRef.current = hasMore;
  }, [hasMore]);

  // Store currentPage in a ref for stable pagination callbacks
  const currentPageRef = useRef(currentPage);
  useEffect(() => {
    currentPageRef.current = currentPage;
  }, [currentPage]);

  // Store cursorHistory in a ref for stable pagination callbacks
  const cursorHistoryRef = useRef(cursorHistory);
  useEffect(() => {
    cursorHistoryRef.current = cursorHistory;
  }, [cursorHistory]);

  // Stable loadMore callback - uses refs to avoid recreating on state changes
  const loadMore = useCallback(() => {
    if (!enabled) {
      return;
    }

    if (hasMoreRef.current && !isLoadingRef.current) {
      // Fetch next page - use refs to get current values
      fetchMinerListRef.current(filterRef.current, sortRef.current, cursorRef.current);
    }
  }, [enabled]);

  const goToPage = useCallback(
    (targetPage: number) => {
      if (!enabled || isLoadingRef.current) return;
      const cursor = cursorHistoryRef.current[targetPage];
      setCurrentPage(targetPage);
      fetchMinerListRef.current(filterRef.current, sortRef.current, cursor, targetPage);
    },
    [enabled],
  );

  const goToNextPage = useCallback(() => {
    if (!hasMoreRef.current) return;
    goToPage(currentPageRef.current + 1);
  }, [goToPage]);

  const goToPrevPage = useCallback(() => {
    if (currentPageRef.current === 0) return;
    goToPage(currentPageRef.current - 1);
  }, [goToPage]);

  // Stable refetch callback - uses refs to avoid recreating on state changes
  // This resets to page 0 - use for filter/sort changes
  const refetch = useCallback(() => {
    if (!enabled) {
      return;
    }

    if (isLoadingRef.current) {
      pendingFetchModeRef.current = "refetch";
      return;
    }

    // Reset pagination and start fresh
    setCurrentPage(0);
    setCursorHistory([undefined]);
    fetchMinerListRef.current(filterRef.current, sortRef.current, undefined, 0);
  }, [enabled]);

  // Refresh current page without resetting pagination - use for polling
  const refreshCurrentPage = useCallback(() => {
    if (isLoadingRef.current) {
      if (pendingFetchModeRef.current !== "refetch") {
        pendingFetchModeRef.current = "refresh";
      }
      return;
    }

    const currentCursor = cursorHistoryRef.current[currentPageRef.current];
    fetchMinerListRef.current(filterRef.current, sortRef.current, currentCursor, currentPageRef.current, true);
  }, []);

  const updateMinerWorkerName = useCallback((deviceIdentifier: string, workerName: string) => {
    setMiners((prev) => {
      const existingMiner = prev[deviceIdentifier];
      if (!existingMiner || existingMiner.workerName === workerName) {
        return prev;
      }

      return {
        ...prev,
        [deviceIdentifier]: create(MinerStateSnapshotSchema, {
          ...existingMiner,
          workerName,
        }),
      };
    });
  }, []);

  // Track if this is the initial load and previous filter/sort
  const hasLoadedRef = useRef(false);
  const wasEnabledRef = useRef(enabled);
  const previousFilterRef = useRef<MinerListFilter | undefined>(undefined);
  const previousSortRef = useRef<SortConfig | undefined>(undefined);

  // Fetch data when filter or sort changes
  useEffect(() => {
    if (!enabled) {
      wasEnabledRef.current = false;
      return;
    }

    const wasDisabled = !wasEnabledRef.current;

    // Check if filter actually changed using protobuf deep equality
    const filtersEqual =
      previousFilterRef.current === filter || // Both undefined or same reference
      (previousFilterRef.current !== undefined &&
        filter !== undefined &&
        equals(MinerListFilterSchema, previousFilterRef.current, filter));

    // Check if sort actually changed using protobuf deep equality
    const sortsEqual =
      previousSortRef.current === sort || // Both undefined or same reference
      (previousSortRef.current !== undefined &&
        sort !== undefined &&
        equals(SortConfigSchema, previousSortRef.current, sort));

    const filterChanged = !filtersEqual;
    const sortChanged = !sortsEqual;

    if (hasLoadedRef.current && !filterChanged && !sortChanged && !wasDisabled) {
      return; // Skip if not first load and neither filter nor sort has changed
    }

    // Update refs
    previousFilterRef.current = filter;
    previousSortRef.current = sort;
    hasLoadedRef.current = true;
    wasEnabledRef.current = true;

    // Reset cursor and pagination for new filter or sort
    if (filterChanged || sortChanged) {
      setCursor(undefined);
      setCurrentPage(0);
      setCursorHistory([undefined]);
    }

    // Fetch with filter and sort
    void fetchMinerListRef.current(filter, sort, undefined, 0);
  }, [enabled, filter, sort]);

  return {
    minerIds,
    miners,
    totalMiners,
    hasMore,
    isLoading,
    hasInitialLoadCompleted,
    loadMore,
    currentPage,
    hasPreviousPage: currentPage > 0,
    goToNextPage,
    goToPrevPage,
    refetch,
    refreshCurrentPage,
    updateMinerWorkerName,
    availableModels,
    availableFirmwareVersions,
  };
};

export default useFleet;
