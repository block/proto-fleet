import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import {
  fleetManagementClient,
  telemetryClient,
} from "@/protoFleet/api/clients";
import {
  DataMode,
  DeviceStatusUpdateSchema,
  MeasurementConfig_MeasurementType,
  MinerListFilter,
  MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  MeasurementType,
  StreamUpdatesRequestSchema,
  StreamUpdatesResponse,
  UpdateType,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import {
  useAuthErrors,
  useAuthHeader,
  useFleetStore,
  useMinerIds,
  useTotalMiners,
} from "@/protoFleet/store";
import { debounce } from "@/shared/utils/utility";

type UseFleetOptions = {
  initialFilter?: MinerListFilter;
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
  mode?: "snapshot" | "metadata" | "timeseries";
};

// Constants to prevent re-renders from unstable default values
const DEFAULT_PAIRING_STATUSES: PairingStatus[] = [];

const DataModeMapping = {
  snapshot: DataMode.SNAPSHOT,
  metadata: DataMode.METADATA,
  timeseries: DataMode.TIME_SERIES,
} as const;

/**
 * Hook for managing fleet data with automatic loading, filtering, and pagination.
 *
 * @param options - Configuration options for the hook
 * @param options.initialFilter - Optional filter to apply on initial load
 * @param options.pageSize - Number of miners to fetch per page (default: 100)
 *
 * @example
 * ```tsx
 * // Global scope - for main fleet view (MinerList)
 * const { minerIds, hasMore, isLoading, setFilter, loadMore } = useFleet({
 *   scope: 'global'
 * });
 *
 * // Local scope - for secondary views that shouldn't affect global state
 * const { minerIds, miners, hasMore, isLoading, setFilter, loadMore } = useFleet({
 *   scope: 'local',
 *   initialFilter: { status: [ComponentStatus.OK] }
 * });
 *
 * // With custom page size
 * const { minerIds, hasMore, isLoading, setFilter, loadMore } = useFleet({
 *   scope: 'global',
 *   pageSize: 50
 * });
 *
 * // With visible miners for optimized telemetry streaming
 * const { minerIds, hasMore, isLoading, setFilter, loadMore } = useFleet({
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
 * ```
 */
const useFleet = (options: UseFleetOptions = {}) => {
  const {
    initialFilter,
    pageSize = 100,
    pairingStatuses = DEFAULT_PAIRING_STATUSES, // Use stable reference to prevent re-renders
    scope = "global",
    visibleMinerIds,
    mode = "metadata",
  } = options;
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  // Local state for 'local' scope
  const [localMinerIds, setLocalMinerIds] = useState<string[]>([]);
  const [localMiners, setLocalMiners] = useState<
    Record<string, MinerStateSnapshot>
  >({});
  const [localTotalMiners, setLocalTotalMiners] = useState(0);

  // Choose state source based on scope
  const globalMinerIds = useMinerIds();
  const globalTotalMiners = useTotalMiners();

  const minerIds = scope === "global" ? globalMinerIds : localMinerIds;
  const totalMiners = scope === "global" ? globalTotalMiners : localTotalMiners;

  const telemetryStreamAbortController = useRef<AbortController | null>(null);
  const previousVisibleIdsRef = useRef<Set<string>>(new Set());
  const initialLoadDoneRef = useRef(false);

  // Internal state for the hook
  const [currentFilter, setCurrentFilter] = useState<
    MinerListFilter | undefined
  >(initialFilter);
  const [hasMore, setHasMore] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
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
    if (
      update.type === UpdateType.MINER_STATE_COUNTS &&
      update.minerStateCounts
    ) {
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
      useFleetStore
        .getState()
        .fleet.updateMinerTelemetry(update.deviceId, update);

      // Handle device status updates
    } else if (
      update.type === UpdateType.DEVICE_STATUS &&
      update.deviceStatus
    ) {
      useFleetStore.getState().fleet.updateMinerDeviceStatus(
        update.deviceId,
        create(DeviceStatusUpdateSchema, {
          status: update.deviceStatus,
        }),
      );
    }

    if (update.timestamp) {
      useFleetStore
        .getState()
        .fleet.updateMinerTimestamp(update.deviceId, update.timestamp);
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
            ...authHeader,
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
            (telemetryStreamAbortController.current &&
              telemetryStreamAbortController.current.signal.aborted)
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
    [authHeader, updateMinerState, handleAuthErrors],
  );

  // Fetch initial list using one-time query
  const fetchMinerList = useCallback(
    async (filter: MinerListFilter | undefined, pageCursor?: string) => {
      setIsLoading(true);
      try {
        // Merge pairing statuses into the filter
        const filterWithPairingStatuses = filter
          ? { ...filter, pairingStatuses }
          : { pairingStatuses };

        const dataMode = DataModeMapping[mode];

        const response = await fleetManagementClient.listMinerStateSnapshots(
          {
            pageSize,
            cursor: pageCursor,
            filter: filterWithPairingStatuses,
            dataMode,
            measurementConfigs:
              dataMode === DataMode.METADATA
                ? undefined
                : [
                    {
                      measurementType:
                        MeasurementConfig_MeasurementType.HASHRATE,
                      dataMode: DataMode.TIME_SERIES,
                      timeSeriesConfig: {
                        timeSelection: {
                          case: "lookbackPeriod",
                          value: {
                            seconds: BigInt(600),
                            nanos: 0,
                          },
                        },
                        resolution: 100,
                      },
                    },
                    // Get snapshot values for other measurements
                    {
                      measurementType:
                        MeasurementConfig_MeasurementType.POWER_USAGE,
                      dataMode: DataMode.SNAPSHOT,
                    },
                    {
                      measurementType:
                        MeasurementConfig_MeasurementType.TEMPERATURE,
                      dataMode: DataMode.SNAPSHOT,
                    },
                    {
                      measurementType:
                        MeasurementConfig_MeasurementType.EFFICIENCY,
                      dataMode: DataMode.SNAPSHOT,
                    },
                  ],
          },
          authHeader,
        );

        const {
          miners,
          cursor: newCursor,
          totalMiners: responseTotalMiners,
          totalStateCounts,
        } = response;

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
          },
        });
      } finally {
        setIsLoading(false);
      }
    },
    [pairingStatuses, mode, pageSize, authHeader, scope, handleAuthErrors],
  );

  // Debounced version of fetchMinerList for internal use
  const fetchMiners = useMemo(() => {
    return debounce(fetchMinerList, 300);
  }, [fetchMinerList]);

  const setFilter = useCallback(
    (filter: MinerListFilter) => {
      setCurrentFilter(filter);
      // Only update global store if scope is global
      if (scope === "global") {
        useFleetStore.getState().fleet.setCurrentFilter(filter);
      }
      setCursor(undefined); // Reset cursor when filter changes

      // Fetch immediately with debounce
      fetchMiners(filter, undefined);
    },
    [fetchMiners, scope],
  );

  const loadMore = useCallback(() => {
    if (hasMore && !isLoading) {
      // Fetch next page with debounce
      fetchMiners(currentFilter, cursor);
    }
  }, [hasMore, isLoading, currentFilter, cursor, fetchMiners]);

  // Set up refetch callback for the store (only for global scope)
  useEffect(() => {
    if (scope !== "global") {
      return;
    }

    const refetchCallback = () => {
      if (!isLoading) {
        fetchMiners(currentFilter, cursor);
      }
    };

    useFleetStore.getState().fleet.setRefetchCallback(refetchCallback);

    return () => {
      useFleetStore.getState().fleet.setRefetchCallback(undefined);
    };
  }, [fetchMiners, currentFilter, cursor, isLoading, scope]);

  // Initial load on mount - only run once
  useEffect(() => {
    // Skip if already done initial load
    if (initialLoadDoneRef.current) {
      return;
    }

    initialLoadDoneRef.current = true;

    // Only update global store filter if scope is global
    if (scope === "global") {
      useFleetStore.getState().fleet.setCurrentFilter(initialFilter);
    }

    // Fetch immediately - we call fetchMinerList directly to avoid debounce on initial load
    void fetchMinerList(initialFilter, undefined);

    // Cleanup streaming on unmount to prevent memory leaks (only for global scope)
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
    const allMiners = allMinerIds
      .map((id) => useFleetStore.getState().fleet.miners[id])
      .filter(Boolean);

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
    setFilter,
    loadMore,
    // Only return miners map for local scope (global scope uses store)
    ...(scope === "local" && { miners: localMiners }),
  };
};

export default useFleet;
