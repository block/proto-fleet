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
};

/**
 * Hook for managing fleet data with automatic loading, filtering, and pagination.
 *
 * @param options - Configuration options for the hook
 * @param options.initialFilter - Optional filter to apply on initial load
 * @param options.pageSize - Number of miners to fetch per page (default: 100)
 *
 * @example
 * ```tsx
 * // Basic usage - loads all miners on mount
 * const { minerIds, hasMore, isLoading, setFilter, loadMore } = useFleet();
 *
 * // With initial filter - loads only hashing miners on mount
 * const { minerIds, hasMore, isLoading, setFilter, loadMore } = useFleet({
 *   initialFilter: { status: [ComponentStatus.OK] }
 * });
 *
 * // With custom page size
 * const { minerIds, hasMore, isLoading, setFilter, loadMore } = useFleet({
 *   pageSize: 50
 * });
 *
 * // With both initial filter and custom page size
 * const { minerIds, hasMore, isLoading, setFilter, loadMore } = useFleet({
 *   initialFilter: { status: [ComponentStatus.OK] },
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
  const { initialFilter, pageSize = 100 } = options;
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  const minerIds = useMinerIds();
  const totalMiners = useTotalMiners();
  const telemetryStreamAbortController = useRef<AbortController | null>(null);

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
        const response = await fleetManagementClient.listMinerStateSnapshots(
          {
            pageSize,
            cursor: pageCursor,
            filter,
            dataMode: DataMode.METADATA,
            measurementConfigs: [
              {
                measurementType: MeasurementConfig_MeasurementType.HASHRATE,
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
            ],
          },
          authHeader,
        );

        const {
          miners,
          cursor: newCursor,
          totalMiners,
          totalStateCounts,
        } = response;

        // Update store
        useFleetStore.getState().fleet.setMiners(miners);

        const store = useFleetStore.getState();
        store.fleet.setCursor(newCursor);
        store.fleet.setTotalMiners(totalMiners);
        if (totalStateCounts) {
          store.fleet.setDeviceStatusCounts(totalStateCounts);
        }

        // Update internal state
        setCursor(newCursor || undefined);
        setHasMore(!!newCursor);

        // Start telemetry streaming for these miners
        if (miners.length > 0) {
          const deviceIds = miners.map((miner) => miner.deviceIdentifier);
          startStreamingTelemetry(deviceIds);
        }
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
    [authHeader, pageSize, startStreamingTelemetry, handleAuthErrors],
  );

  // Debounced version of fetchMinerList for internal use
  const fetchMiners = useMemo(() => {
    return debounce(fetchMinerList, 300);
  }, [fetchMinerList]);

  const setFilter = useCallback(
    (filter: MinerListFilter) => {
      setCurrentFilter(filter);
      useFleetStore.getState().fleet.setCurrentFilter(filter);
      setCursor(undefined); // Reset cursor when filter changes

      // Fetch immediately with debounce
      fetchMiners(filter, undefined);
    },
    [fetchMiners],
  );

  const loadMore = useCallback(() => {
    if (hasMore && !isLoading) {
      // Fetch next page with debounce
      fetchMiners(currentFilter, cursor);
    }
  }, [hasMore, isLoading, currentFilter, cursor, fetchMiners]);

  // Set up refetch callback for the store
  useEffect(() => {
    const refetchCallback = () => {
      if (!isLoading) {
        fetchMiners(currentFilter, cursor);
      }
    };

    useFleetStore.getState().fleet.setRefetchCallback(refetchCallback);

    return () => {
      useFleetStore.getState().fleet.setRefetchCallback(undefined);
    };
  }, [fetchMiners, currentFilter, cursor, isLoading]);

  // Initial load on mount
  useEffect(() => {
    useFleetStore.getState().fleet.setCurrentFilter(initialFilter);

    // Fetch immediately with debounce
    fetchMiners(initialFilter, undefined);

    // Cleanup streaming on unmount to prevent memory leaks
    return () => {
      if (telemetryStreamAbortController.current) {
        telemetryStreamAbortController.current.abort();
        telemetryStreamAbortController.current = null;
      }
    };
  }, [fetchMiners, initialFilter]); // Only run on mount

  return {
    minerIds,
    totalMiners,
    hasMore,
    isLoading,
    setFilter,
    loadMore,
  };
};

export default useFleet;
