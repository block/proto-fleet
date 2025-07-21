import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import {
  fleetManagementClient,
  telemetryClient,
} from "@/protoFleet/api/clients";
import {
  DataMode,
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
  getAuthHeader,
  useAuthContext,
} from "@/protoFleet/features/auth/contexts/AuthContext";
import {
  useFleetStore,
  useMinerIds,
} from "@/protoFleet/features/fleetManagement/store/useFleetStore";
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
  const { authTokens } = useAuthContext();

  const minerIds = useMinerIds();
  const streamAbortController = useRef<AbortController | null>(null);

  // Internal state for the hook
  const [currentFilter, setCurrentFilter] = useState<
    MinerListFilter | undefined
  >(initialFilter);
  const [hasMore, setHasMore] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [cursor, setCursor] = useState<string | undefined>();

  const updateMinerState = useCallback((response: StreamUpdatesResponse) => {
    const update = response.update;

    if (!update || !update.deviceId) {
      return;
    }

    // Handle heartbeat updates
    if (update.type === UpdateType.HEARTBEAT) {
      return;
    }

    // Handle telemetry data updates
    if (update.type === UpdateType.TELEMETRY && update.data) {
      useFleetStore.getState().updateMinerTelemetry(update.deviceId, update);
    }

    // Handle device status updates - TODO: implement when needed

    if (update.timestamp) {
      useFleetStore
        .getState()
        .updateMinerTimestamp(update.deviceId, update.timestamp);
    }
  }, []);

  const startStreamingUpdates = useCallback(
    (deviceIdentifiers: string[]) => {
      if (!deviceIdentifiers || deviceIdentifiers.length === 0) {
        return;
      }

      if (streamAbortController.current) {
        streamAbortController.current.abort();
      }

      streamAbortController.current = new AbortController();

      useFleetStore.getState().setStreaming(true);

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
            ...getAuthHeader(authTokens),
            signal: streamAbortController.current?.signal,
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
            (streamAbortController.current &&
              streamAbortController.current.signal.aborted)
          ) {
            return;
          }

          console.error("Error streaming telemetry updates:", error);
        } finally {
          useFleetStore.getState().setStreaming(false);
        }
      })();
    },
    [authTokens, updateMinerState],
  );

  const doFetchMiners = useCallback(
    async (
      filter: MinerListFilter | undefined,
      pageCursor?: string,
      append = false,
    ) => {
      setIsLoading(true);
      try {
        const response = await fleetManagementClient.listMinerStateSnapshots(
          {
            pageSize,
            cursor: pageCursor,
            filter,
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
          getAuthHeader(authTokens),
        );

        const {
          miners,
          cursor: newCursor,
          totalMiners,
          totalStateCounts,
        } = response;

        // Update store based on append flag
        if (append) {
          useFleetStore.getState().appendMiners(miners);
        } else {
          useFleetStore.getState().setMiners(miners);
        }

        useFleetStore.getState().setCursor(newCursor);
        useFleetStore.getState().setTotalMiners(totalMiners);
        if (totalStateCounts) {
          useFleetStore.getState().setMinerStateCounts(totalStateCounts);
        }

        // Update internal state
        setCursor(newCursor || undefined);
        setHasMore(!!newCursor);

        // Start streaming updates for these miners
        if (miners.length > 0) {
          const deviceIds = miners.map((miner) => miner.deviceIdentifier);
          startStreamingUpdates(deviceIds);
        }

        return {
          miners,
          cursor: newCursor,
          totalMiners,
          totalStateCounts,
        };
      } catch (error) {
        console.error("Error fetching fleet data:", error);
        throw error;
      } finally {
        setIsLoading(false);
      }
    },
    [authTokens, startStreamingUpdates, pageSize],
  );

  const fetchMiners = useMemo(() => {
    return debounce(doFetchMiners, 300);
  }, [doFetchMiners]);

  const setFilter = useCallback(
    (filter: MinerListFilter) => {
      setCurrentFilter(filter);
      setCursor(undefined); // Reset cursor when filter changes
      fetchMiners(filter, undefined, false);
    },
    [fetchMiners],
  );

  const loadMore = useCallback(() => {
    if (hasMore && !isLoading) {
      fetchMiners(currentFilter, cursor, true);
    }
  }, [hasMore, isLoading, currentFilter, cursor, fetchMiners]);

  // Initial load on mount
  useEffect(() => {
    fetchMiners(initialFilter, undefined, false);

    // Cleanup streaming on unmount to prevent memory leaks
    return () => {
      fetchMiners.cancel();
      if (streamAbortController.current) {
        streamAbortController.current.abort();
        streamAbortController.current = null;
      }
    };
  }, [fetchMiners, initialFilter]); // Only run on mount

  return {
    minerIds,
    hasMore,
    isLoading,
    setFilter,
    loadMore,
  };
};

export default useFleet;
