import { useCallback, useEffect, useRef, useState } from "react";
import { create, equals } from "@bufbuild/protobuf";
import { fleetManagementClient, telemetryClient } from "@/protoFleet/api/clients";
import {
  DeviceStatusUpdateSchema,
  MinerListFilter,
  MinerListFilterSchema,
  MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  MeasurementType,
  StreamUpdatesRequestSchema,
  StreamUpdatesResponse,
  UpdateType,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useAuthErrors, useFleetStore, useMinerIds, useTotalMiners } from "@/protoFleet/store";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";

type UseFleetOptions = {
  filter?: MinerListFilter;
  pageSize?: number;
  pairingStatuses?: PairingStatus[];
  /**
   * Scope determines where the fetched data is stored:
   * - 'global': Updates the global Zustand store. Should only be used by MinerList.
   *             Enables telemetry streaming and affects all components reading from global state.
   * - 'local': Stores data in component-local state. Use for secondary views like
   *            CompleteSetup or AuthenticateMiners that need to fetch filtered data
   *            without affecting the main fleet view.
   * @default 'global'
   */
  scope?: "global" | "local";
  /**
   * Set of miner IDs currently visible in viewport (for global scope only).
   * When provided, telemetry streaming will only subscribe to these visible miners
   * instead of all paired miners. Updates to this set will restart the stream
   * with the new subset of miners.
   */
  visibleMinerIds?: Set<string>;
};

// Constants to prevent re-renders from unstable default values
const DEFAULT_PAIRING_STATUSES: PairingStatus[] = [];

/**
 * Hook for managing fleet data with automatic loading, filtering, and pagination.
 *
 * @param options - Configuration options for the hook
 * @param options.filter - Optional filter to apply
 * @param options.pageSize - Number of miners to fetch per page (default: 100)
 *
 * @example
 * ```tsx
 * // Global scope - for main fleet view (MinerList)
 * const { minerIds, totalMiners, hasMore, isLoading, setFilter, loadMore, refetch } = useFleet({
 *   scope: 'global'
 * });
 *
 * // Local scope - for secondary views that shouldn't affect global state
 * const { minerIds, miners, totalMiners, hasMore, isLoading, setFilter, loadMore, refetch } = useFleet({
 *   scope: 'local',
 *   filter: { status: [ComponentStatus.OK] }
 * });
 *
 * // With custom page size
 * const { minerIds, totalMiners, hasMore, isLoading, setFilter, loadMore, refetch } = useFleet({
 *   scope: 'global',
 *   pageSize: 50
 * });
 *
 * // With visible miners for optimized telemetry streaming
 * const { minerIds, totalMiners, hasMore, isLoading, loadMore, refetch } = useFleet({
 *   scope: 'global',
 *   visibleMinerIds: myVisibleMinerIds,
 *   pageSize: 25
 * });
 *
 * // Filter to show only broken miners
 * setFilter({ status: [ComponentStatus.ERROR] });
 *
 * // Load more miners (appends to current list)
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
    pageSize = 20,
    pairingStatuses = DEFAULT_PAIRING_STATUSES, // Use stable reference to prevent re-renders
    scope = "local",
    visibleMinerIds,
  } = options;
  const { handleAuthErrors } = useAuthErrors();

  // Local state for 'local' scope
  const [localMinerIds, setLocalMinerIds] = useState<string[]>([]);
  const [localMiners, setLocalMiners] = useState<Record<string, MinerStateSnapshot>>({});
  const [localTotalMiners, setLocalTotalMiners] = useState(0);

  // Choose state source based on scope
  const globalMinerIds = useMinerIds();
  const globalTotalMiners = useTotalMiners();

  const minerIds = scope === "global" ? globalMinerIds : localMinerIds;
  const totalMiners = scope === "global" ? globalTotalMiners : localTotalMiners;

  const telemetryStreamAbortController = useRef<AbortController | null>(null);
  const previousVisibleIdsRef = useRef<Set<string>>(new Set());

  // Internal state for the hook
  const [hasMore, setHasMore] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [hasInitialLoadCompleted, setHasInitialLoadCompleted] = useState(false);
  const [cursor, setCursor] = useState<string | undefined>();

  const updateMinerState = useCallback((response: StreamUpdatesResponse) => {
    const update = response.update;

    if (!update) {
      return;
    }

    // Handle heartbeat updates
    if (update.type === UpdateType.HEARTBEAT) {
      return;
    }

    // Handle device status counts updates (fleet-wide, no deviceId required)
    if (update.type === UpdateType.MINER_STATE_COUNTS && update.minerStateCounts) {
      const store = useFleetStore.getState();
      store.fleet.setDeviceStatusCounts(update.minerStateCounts);
      return;
    }

    // For device-specific updates, deviceId is required
    if (!update.deviceId) {
      return;
    }

    // Handle telemetry data updates
    if (update.type === UpdateType.TELEMETRY && update.data) {
      useFleetStore.getState().fleet.updateMinerTelemetry(update.deviceId, update);

      // Handle device status updates
    } else if (update.type === UpdateType.DEVICE_STATUS && update.deviceStatus) {
      useFleetStore.getState().fleet.updateMinerDeviceStatus(
        update.deviceId,
        create(DeviceStatusUpdateSchema, {
          status: update.deviceStatus,
        }),
      );
    }

    if (update.timestamp) {
      useFleetStore.getState().fleet.updateMinerTimestamp(update.deviceId, update.timestamp);
    }
  }, []);

  const startStreamingTelemetry = useCallback(
    (deviceIdentifiers: string[]) => {
      if (!deviceIdentifiers || deviceIdentifiers.length === 0) {
        return;
      }

      if (telemetryStreamAbortController.current) {
        telemetryStreamAbortController.current.abort();
      }

      telemetryStreamAbortController.current = new AbortController();

      useFleetStore.getState().fleet.setStreaming(true);

      (async () => {
        try {
          const request = create(StreamUpdatesRequestSchema, {
            deviceIds: deviceIdentifiers,
            measurementTypes: [
              MeasurementType.HASHRATE,
              MeasurementType.POWER,
              MeasurementType.TEMPERATURE,
              MeasurementType.EFFICIENCY,
            ],
            includeHeartbeat: true,
            heartbeatInterval: {
              seconds: BigInt(30),
              nanos: 0,
            },
          });

          for await (const response of telemetryClient.streamUpdates(request, {
            signal: telemetryStreamAbortController.current?.signal,
          })) {
            updateMinerState(response);
          }
        } catch (error) {
          const errorMessage = String(error);

          // Check if the error is due to an aborted request
          // ConnectError with 'canceled' or AbortError means the request was intentionally aborted
          if (
            errorMessage.includes("[canceled]") ||
            errorMessage.includes("AbortError") ||
            (telemetryStreamAbortController.current && telemetryStreamAbortController.current.signal.aborted)
          ) {
            return;
          }

          handleAuthErrors({
            error: error,
            onError: (err) => {
              console.error("Error streaming telemetry updates:", err);
            },
          });
        } finally {
          useFleetStore.getState().fleet.setStreaming(false);
        }
      })();
    },
    [updateMinerState, handleAuthErrors],
  );

  // Fetch initial list using one-time query
  const fetchMinerList = useCallback(
    async (filter: MinerListFilter | undefined, pageCursor?: string) => {
      setIsLoading(true);

      // Reset initial load flag for non-pagination fetches (filter change or refetch)
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
        });

        const { miners, cursor: newCursor, totalMiners: responseTotalMiners, totalStateCounts } = response;

        // Update state based on scope
        if (scope === "global") {
          const store = useFleetStore.getState();

          // Use setMiners for initial load, appendMiners for pagination
          if (pageCursor) {
            // Pagination: append new miners to existing list
            store.fleet.appendMiners(miners);
          } else {
            // Initial load or filter change: replace list
            store.fleet.setMiners(miners);
          }

          store.fleet.setCursor(newCursor);
          store.fleet.setTotalMiners(responseTotalMiners);
          if (totalStateCounts) {
            store.fleet.setDeviceStatusCounts(totalStateCounts);
          }

          // Note: Telemetry streaming is handled by the separate useEffect
          // that watches visibleMinerIds changes (see below)
        } else {
          // Update local component state
          if (pageCursor) {
            // Pagination: append to existing local state
            const newIds = miners.map((miner) => miner.deviceIdentifier);
            setLocalMinerIds((prev) => [...prev, ...newIds]);
            setLocalMiners((prev) => {
              const newMinersMap = { ...prev };
              miners.forEach((miner) => {
                newMinersMap[miner.deviceIdentifier] = miner;
              });
              return newMinersMap;
            });
          } else {
            // Initial load: replace local state
            const ids = miners.map((miner) => miner.deviceIdentifier);
            const minersMap: Record<string, MinerStateSnapshot> = {};
            miners.forEach((miner) => {
              minersMap[miner.deviceIdentifier] = miner;
            });
            setLocalMinerIds(ids);
            setLocalMiners(minersMap);
          }
          setLocalTotalMiners(responseTotalMiners);
          // Note: Local scope doesn't stream telemetry or update device status counts
        }

        // Update internal state (both scopes)
        setCursor(newCursor || undefined);
        setHasMore(!!newCursor);
      } catch (error) {
        handleAuthErrors({
          error: error,
          onError: (err) => {
            console.error("Error fetching miner list:", err);

            // Show toast for initial fetch errors (not pagination)
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

        // Mark initial load as completed for non-pagination fetches (success or error)
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

  // Stable loadMore callback - uses refs to avoid recreating on state changes
  const loadMore = useCallback(() => {
    if (hasMoreRef.current && !isLoadingRef.current) {
      // Fetch next page - use refs to get current values
      fetchMinerListRef.current(filterRef.current, cursorRef.current);
    }
  }, []);

  // Stable refetch callback - uses refs to avoid recreating on state changes
  const refetch = useCallback(() => {
    if (!isLoadingRef.current) {
      // Reset cursor to start fresh - use ref for current filter
      fetchMinerListRef.current(filterRef.current, undefined);
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

  // Track if this is the initial load and previous filter
  const hasLoadedRef = useRef(false);
  const previousFilterRef = useRef<MinerListFilter | undefined>(undefined);

  // Fetch data when filter changes
  useEffect(() => {
    // Check if filter actually changed using protobuf deep equality
    const filtersEqual =
      previousFilterRef.current === filter || // Both undefined or same reference
      (previousFilterRef.current !== undefined &&
        filter !== undefined &&
        equals(MinerListFilterSchema, previousFilterRef.current, filter));

    const filterChanged = !filtersEqual;

    if (hasLoadedRef.current && !filterChanged) {
      return; // Skip if not first load and filter hasn't changed
    }

    // Update refs
    previousFilterRef.current = filter;
    hasLoadedRef.current = true;

    // Reset cursor for new filter
    if (filterChanged) {
      setCursor(undefined);
    }

    // Fetch with filter using ref to avoid dependency
    void fetchMinerListRef.current(filter, undefined);
  }, [filter]);

  // Cleanup streaming on unmount
  useEffect(() => {
    return () => {
      if (scope === "global" && telemetryStreamAbortController.current) {
        telemetryStreamAbortController.current.abort();
        telemetryStreamAbortController.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // Empty deps - truly only run on mount

  // Restart telemetry stream when visible miners or miner list changes (global scope only)
  useEffect(() => {
    if (scope !== "global" || !visibleMinerIds) {
      return;
    }

    // Get all paired miner IDs from store
    const allMinerIds = useFleetStore.getState().fleet.minerIds;
    const allMiners = allMinerIds.map((id) => useFleetStore.getState().fleet.miners[id]).filter(Boolean);

    const pairedDeviceIds = allMiners
      .filter((miner) => miner.pairingStatus === PairingStatus.PAIRED)
      .map((miner) => miner.deviceIdentifier)
      .filter((id) => visibleMinerIds.has(id));

    // Check if the streaming IDs actually changed (deep comparison)
    const previousIds = previousVisibleIdsRef.current;
    const currentStreamingIds = new Set(pairedDeviceIds);

    // Early exit if sizes differ - definitely changed
    let hasChanged = currentStreamingIds.size !== previousIds.size;

    // If sizes match, check if contents differ (iterate Set directly, no array allocation)
    if (!hasChanged) {
      for (const id of currentStreamingIds) {
        if (!previousIds.has(id)) {
          hasChanged = true;
          break;
        }
      }
    }

    if (!hasChanged) {
      return;
    }

    // Update ref with new streaming IDs
    previousVisibleIdsRef.current = currentStreamingIds;

    if (pairedDeviceIds.length > 0) {
      startStreamingTelemetry(pairedDeviceIds);
    } else if (telemetryStreamAbortController.current) {
      // No visible miners - stop streaming
      telemetryStreamAbortController.current.abort();
    }
  }, [visibleMinerIds, minerIds, scope, startStreamingTelemetry]);

  return {
    minerIds,
    totalMiners,
    hasMore,
    isLoading,
    hasInitialLoadCompleted,
    loadMore,
    // Only return miners map for local scope (global scope uses store)
    ...(scope === "local" && { miners: localMiners }),
    refetch,
  };
};

export default useFleet;
