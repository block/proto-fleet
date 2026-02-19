import { useCallback, useRef } from "react";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  DataMode,
  MeasurementConfig_MeasurementType,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useAuthErrors, useFleetStore } from "@/protoFleet/store";

const MAX_BATCH_SIZE = 100;

type UseBatchTelemetryOptions = {
  /**
   * Callback to track loading state for telemetry fetching
   */
  onLoadingChange?: (loading: boolean) => void;
};

/**
 * Hook for fetching telemetry data for multiple miners in a single batch request.
 * This is optimized for loading telemetry after an initial metadata-only list load.
 *
 * The hook provides a function that can be called to fetch telemetry for a set of
 * visible miner IDs. It automatically handles:
 * - Batching requests (max 100 miners per request)
 * - Deduplication of requests for the same miners
 * - Updating the global store with fetched telemetry
 *
 * @example
 * ```tsx
 * const { fetchBatchTelemetry } = useBatchTelemetry();
 *
 * // When visible miners change
 * useEffect(() => {
 *   if (visibleMinerIds.size > 0) {
 *     fetchBatchTelemetry(visibleMinerIds);
 *   }
 * }, [visibleMinerIds, fetchBatchTelemetry]);
 * ```
 */
const useBatchTelemetry = (options: UseBatchTelemetryOptions = {}) => {
  const { onLoadingChange } = options;
  const { handleAuthErrors } = useAuthErrors();

  // Track which device IDs we've already fetched telemetry for
  const fetchedIdsRef = useRef<Set<string>>(new Set());
  const pendingRequestRef = useRef<AbortController | null>(null);

  const fetchBatchTelemetry = useCallback(
    async (deviceIds: Set<string> | string[]) => {
      const idsArray = Array.isArray(deviceIds) ? deviceIds : Array.from(deviceIds);

      const newIds = idsArray.filter((id) => !fetchedIdsRef.current.has(id));

      if (newIds.length === 0) {
        return;
      }

      if (pendingRequestRef.current) {
        pendingRequestRef.current.abort();
      }

      pendingRequestRef.current = new AbortController();
      onLoadingChange?.(true);

      try {
        const batches: string[][] = [];
        for (let i = 0; i < newIds.length; i += MAX_BATCH_SIZE) {
          batches.push(newIds.slice(i, i + MAX_BATCH_SIZE));
        }

        // Process all batches with per-batch error handling
        // Successfully fetched batches are cached even if later batches fail
        for (const batch of batches) {
          try {
            const response = await fleetManagementClient.getBatchMinerTelemetry(
              {
                deviceIdentifiers: batch,
                dataMode: DataMode.SNAPSHOT,
                measurementConfigs: [
                  {
                    measurementType: MeasurementConfig_MeasurementType.HASHRATE,
                    dataMode: DataMode.SNAPSHOT,
                  },
                  {
                    measurementType: MeasurementConfig_MeasurementType.POWER_USAGE,
                    dataMode: DataMode.SNAPSHOT,
                  },
                  {
                    measurementType: MeasurementConfig_MeasurementType.TEMPERATURE,
                    dataMode: DataMode.SNAPSHOT,
                  },
                  {
                    measurementType: MeasurementConfig_MeasurementType.EFFICIENCY,
                    dataMode: DataMode.SNAPSHOT,
                  },
                ],
              },
              { signal: pendingRequestRef.current?.signal },
            );

            if (response.miners.length > 0) {
              useFleetStore.getState().fleet.updateBatchTelemetry(response.miners);
            }

            // Only mark as fetched after successful completion
            batch.forEach((id) => fetchedIdsRef.current.add(id));
          } catch (batchError) {
            const errorMessage = String(batchError);

            // Re-throw abort errors to stop processing remaining batches
            if (errorMessage.includes("[canceled]") || errorMessage.includes("AbortError")) {
              throw batchError;
            }

            // Log per-batch errors but continue with remaining batches
            // Failed batch IDs are NOT added to fetchedIdsRef, so they'll be retried
            console.error(`Error fetching telemetry for batch:`, batchError);
          }
        }
      } catch (error) {
        const errorMessage = String(error);

        // Ignore abort errors
        if (errorMessage.includes("[canceled]") || errorMessage.includes("AbortError")) {
          return;
        }

        handleAuthErrors({
          error: error,
          onError: (err) => {
            console.error("Error fetching batch telemetry:", err);
          },
        });
      } finally {
        onLoadingChange?.(false);
        pendingRequestRef.current = null;
      }
    },
    [handleAuthErrors, onLoadingChange],
  );

  /**
   * Reset the cache of fetched IDs. Call this when the miner list is refreshed
   * to allow re-fetching telemetry for all miners.
   */
  const resetFetchedIds = useCallback(() => {
    fetchedIdsRef.current.clear();
  }, []);

  return {
    fetchBatchTelemetry,
    resetFetchedIds,
  };
};

export default useBatchTelemetry;
