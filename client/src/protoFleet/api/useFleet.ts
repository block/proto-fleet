import { useCallback, useEffect, useRef, useState } from "react";
import { equals } from "@bufbuild/protobuf";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  MinerListFilter,
  MinerListFilterSchema,
  MinerSortConfig,
  MinerSortConfigSchema,
  MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useAuthErrors, useFleetStore, useMinerIds, useTotalMiners } from "@/protoFleet/store";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";

type UseFleetOptions = {
  filter?: MinerListFilter;
  /**
   * Sort configuration for ordering miners.
   * When undefined, uses default server-side ordering (discovery order).
   */
  sort?: MinerSortConfig;
  pageSize?: number;
  pairingStatuses?: PairingStatus[];
  /**
   * Scope determines where the fetched data is stored:
   * - 'global': Updates the global Zustand store. Should only be used by MinerList.
   * - 'local': Stores data in component-local state. Use for secondary views like
   *            CompleteSetup or AuthenticateMiners that need to fetch filtered data
   *            without affecting the main fleet view.
   * @default 'global'
   */
  scope?: "global" | "local";
};

// Constants to prevent re-renders from unstable default values
const DEFAULT_PAIRING_STATUSES: PairingStatus[] = [];

/**
 * Hook for managing fleet data with automatic loading, filtering, and pagination.
 *
 * @param options - Configuration options for the hook
 * @param options.filter - Optional filter to apply
 * @param options.pageSize - Number of miners to fetch per page (default: 20)
 *
 * @example
 * ```tsx
 * // Global scope - for main fleet view (MinerList)
 * const { minerIds, totalMiners, hasMore, isLoading, loadMore, refetch } = useFleet({
 *   scope: 'global'
 * });
 *
 * // Local scope - for secondary views that shouldn't affect global state
 * const { minerIds, miners, totalMiners, hasMore, isLoading, loadMore, refetch } = useFleet({
 *   scope: 'local',
 *   filter: { status: [ComponentStatus.OK] }
 * });
 *
 * // With custom page size
 * const { minerIds, totalMiners, hasMore, isLoading, loadMore, refetch } = useFleet({
 *   scope: 'global',
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
    filter,
    sort,
    pageSize = 20,
    pairingStatuses = DEFAULT_PAIRING_STATUSES, // Use stable reference to prevent re-renders
    scope = "local",
  } = options;
  const { handleAuthErrors } = useAuthErrors();

  // Local state for 'local' scope
  const [localMinerIds, setLocalMinerIds] = useState<string[]>([]);
  const [localMiners, setLocalMiners] = useState<Record<string, MinerStateSnapshot>>({});
  const [localTotalMiners, setLocalTotalMiners] = useState(0);
  const [availableModels, setAvailableModels] = useState<string[]>([]);

  // Choose state source based on scope
  const globalMinerIds = useMinerIds();
  const globalTotalMiners = useTotalMiners();

  const minerIds = scope === "global" ? globalMinerIds : localMinerIds;
  const totalMiners = scope === "global" ? globalTotalMiners : localTotalMiners;

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

  // Fetch initial list using one-time query
  const fetchMinerList = useCallback(
    async (
      filter: MinerListFilter | undefined,
      sort: MinerSortConfig | undefined,
      pageCursor?: string,
      fetchedPage?: number,
    ) => {
      setIsLoading(true);

      // Reset initial load flag when fetching page 0
      if (!pageCursor) {
        setHasInitialLoadCompleted(false);
      }

      try {
        // Merge pairing statuses into the filter
        const filterWithPairingStatuses = filter ? { ...filter, pairingStatuses } : { pairingStatuses };

        const response = await fleetManagementClient.listMinerStateSnapshots({
          pageSize,
          cursor: pageCursor,
          filter: filterWithPairingStatuses,
          sort: sort ? [sort] : undefined,
        });

        const { miners, cursor: newCursor, totalMiners: responseTotalMiners, totalStateCounts, models } = response;

        // Update state based on scope — always replace (never append) for page-based pagination
        if (scope === "global") {
          const store = useFleetStore.getState();
          store.fleet.setMiners(miners);
          store.fleet.setCursor(newCursor);
          store.fleet.setTotalMiners(responseTotalMiners);
          if (totalStateCounts) {
            store.fleet.setDeviceStatusCounts(totalStateCounts);
          }

          // Update available models for filter dropdown
          if (models && models.length > 0) {
            setAvailableModels(models);
          }
        } else {
          // Local scope: always replace
          const ids = miners.map((miner) => miner.deviceIdentifier);
          const minersMap: Record<string, MinerStateSnapshot> = {};
          miners.forEach((miner) => {
            minersMap[miner.deviceIdentifier] = miner;
          });
          setLocalMinerIds(ids);
          setLocalMiners(minersMap);
          setLocalTotalMiners(responseTotalMiners);
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
        setIsLoading(false);

        // Mark initial load as completed when fetching page 0 (success or error)
        // This ensures UI doesn't get stuck in permanent loading state on error
        if (!pageCursor) {
          setHasInitialLoadCompleted(true);
        }
      }
    },
    [pairingStatuses, pageSize, scope, handleAuthErrors],
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
    if (hasMoreRef.current && !isLoadingRef.current) {
      // Fetch next page - use refs to get current values
      fetchMinerListRef.current(filterRef.current, sortRef.current, cursorRef.current);
    }
  }, []);

  const goToPage = useCallback((targetPage: number) => {
    if (isLoadingRef.current) return;
    const cursor = cursorHistoryRef.current[targetPage];
    setCurrentPage(targetPage);
    fetchMinerListRef.current(filterRef.current, sortRef.current, cursor, targetPage);
  }, []);

  const goToNextPage = useCallback(() => {
    if (!hasMoreRef.current) return;
    goToPage(currentPageRef.current + 1);
  }, [goToPage]);

  const goToPrevPage = useCallback(() => {
    if (currentPageRef.current === 0) return;
    goToPage(currentPageRef.current - 1);
  }, [goToPage]);

  // Stable refetch callback - uses refs to avoid recreating on state changes
  const refetch = useCallback(() => {
    if (!isLoadingRef.current) {
      // Reset pagination and start fresh
      setCurrentPage(0);
      setCursorHistory([undefined]);
      fetchMinerListRef.current(filterRef.current, sortRef.current, undefined, 0);
    }
  }, []);

  // Set up refetch callback for the store (only for global scope)
  useEffect(() => {
    if (scope !== "global") {
      return;
    }

    useFleetStore.getState().fleet.setRefetchCallback(refetch);

    return () => {
      useFleetStore.getState().fleet.setRefetchCallback(undefined);
    };
  }, [refetch, scope]);

  // Track if this is the initial load and previous filter/sort
  const hasLoadedRef = useRef(false);
  const previousFilterRef = useRef<MinerListFilter | undefined>(undefined);
  const previousSortRef = useRef<MinerSortConfig | undefined>(undefined);

  // Fetch data when filter or sort changes
  useEffect(() => {
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
        equals(MinerSortConfigSchema, previousSortRef.current, sort));

    const filterChanged = !filtersEqual;
    const sortChanged = !sortsEqual;

    if (hasLoadedRef.current && !filterChanged && !sortChanged) {
      return; // Skip if not first load and neither filter nor sort has changed
    }

    // Update refs
    previousFilterRef.current = filter;
    previousSortRef.current = sort;
    hasLoadedRef.current = true;

    // Reset cursor and pagination for new filter or sort
    if (filterChanged || sortChanged) {
      setCursor(undefined);
      setCurrentPage(0);
      setCursorHistory([undefined]);
    }

    // Fetch with filter and sort
    void fetchMinerListRef.current(filter, sort, undefined, 0);
  }, [filter, sort]);

  return {
    minerIds,
    totalMiners,
    hasMore,
    isLoading,
    hasInitialLoadCompleted,
    loadMore,
    currentPage,
    hasPreviousPage: currentPage > 0,
    goToNextPage,
    goToPrevPage,
    // Only return miners map for local scope (global scope uses store)
    ...(scope === "local" && { miners: localMiners }),
    refetch,
    availableModels,
  };
};

export default useFleet;
