import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  DataMode,
  MeasurementConfig_MeasurementType,
  MinerListFilter,
  StreamMinerListUpdatesRequestSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useAuthErrors, useAuthHeader, useFleetStore } from "@/protoFleet/store";

type UseStreamMinerListUpdatesOptions = {
  filter?: MinerListFilter;
};

/**
 * Hook for streaming incremental miner list updates (additions/removals).
 * Only sends deltas when miners enter/exit filter criteria.
 *
 * Note: Initial data should be fetched via ListMinerStateSnapshots separately.
 * This stream only provides incremental updates.
 *
 * @param options - Configuration options for the hook
 * @param options.filter - Filter to apply to the miner list
 *
 * @example
 * ```tsx
 * // Stream updates for online miners
 * const { isLoading, error } = useStreamMinerListUpdates({
 *   filter: { deviceStatus: [DeviceStatus.ONLINE] }
 * });
 *
 * // Updates are automatically applied to the store
 * const miners = useMinerIds(); // Will reflect incremental updates
 * ```
 */
const useStreamMinerListUpdates = (options: UseStreamMinerListUpdatesOptions = {}) => {
  const { filter } = options;
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  const abortControllerRef = useRef<AbortController | null>(null);
  const [isLoading, setIsLoading] = useState(false); // No initial load
  const [error, setError] = useState<string | null>(null);

  // Start streaming miner list updates
  const startStream = useCallback(async () => {
    // Abort any existing stream
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }

    const controller = new AbortController();
    abortControllerRef.current = controller;

    setIsLoading(true);
    setError(null);

    try {
      const request = create(StreamMinerListUpdatesRequestSchema, {
        filter,
        dataMode: DataMode.METADATA,
        heartbeatIntervalSeconds: 30,
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
      });

      for await (const response of fleetManagementClient.streamMinerListUpdates(request, {
        ...authHeader,
        signal: controller.signal,
      })) {
        // Check if stream is still active
        if (abortControllerRef.current !== controller) {
          return;
        }

        const update = response.update;
        setIsLoading(false); // Stream is active

        if (update.case === "delta") {
          // Handle incremental updates
          const delta = update.value;
          const store = useFleetStore.getState();

          // Apply additions - position filtering is handled in the store
          if (delta.additions && delta.additions.length > 0) {
            store.fleet.addMiners(delta.additions);
          }

          // Apply removals - always remove regardless of position
          if (delta.removals && delta.removals.length > 0) {
            store.fleet.removeMiners(delta.removals);
          }

          // Update total count
          if (delta.totalMiners !== undefined) {
            store.fleet.setTotalMiners(delta.totalMiners);
          }
        }
      }
    } catch (err) {
      const errorMessage = String(err);

      // Check if the error is due to an aborted request
      if (errorMessage.includes("[canceled]") || errorMessage.includes("AbortError") || controller.signal.aborted) {
        return;
      }

      setError(errorMessage);

      handleAuthErrors({
        error: err,
        onError: (error) => {
          console.error("Error streaming miner list updates:", error);
        },
      });
    } finally {
      if (abortControllerRef.current === controller) {
        setIsLoading(false);
      }
    }
  }, [filter, authHeader, handleAuthErrors]);

  // Start stream on mount and when dependencies change
  useEffect(() => {
    startStream();

    // Cleanup on unmount
    return () => {
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
        abortControllerRef.current = null;
      }
    };
  }, [startStream]);

  return {
    isLoading,
    error,
    restart: startStream,
  };
};

export default useStreamMinerListUpdates;
