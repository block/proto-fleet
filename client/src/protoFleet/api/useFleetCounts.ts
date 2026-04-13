import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import { GetMinerStateCountsRequestSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { MinerStateCounts } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useAuthErrors } from "@/protoFleet/store";

interface UseFleetCountsOptions {
  pollIntervalMs?: number;
}

type UseFleetCountsReturn = {
  /** Total number of miners */
  totalMiners: number;
  /** Counts of miners in different states */
  stateCounts: MinerStateCounts | undefined;
  /** Whether the hook is currently loading data */
  isLoading: boolean;
  /** Whether at least one successful fetch has completed */
  hasLoaded: boolean;
  /** Refetch the counts */
  refetch: () => void;
};

/**
 * Hook for fetching miner state counts without loading full miner data.
 * More efficient than useFleet when only counts are needed (e.g., Dashboard).
 * Supports optional polling for periodic refresh.
 *
 * @example
 * ```tsx
 * const { totalMiners, stateCounts, isLoading } = useFleetCounts({ pollIntervalMs: 60000 });
 *
 * // Display counts
 * <div>Total: {totalMiners}</div>
 * <div>Hashing: {stateCounts?.hashingCount ?? 0}</div>
 * <div>Offline: {stateCounts?.offlineCount ?? 0}</div>
 * ```
 */
const useFleetCounts = (options?: UseFleetCountsOptions): UseFleetCountsReturn => {
  const { handleAuthErrors } = useAuthErrors();

  const [totalMiners, setTotalMiners] = useState(0);
  const [stateCounts, setStateCounts] = useState<MinerStateCounts | undefined>(undefined);
  const [isLoading, setIsLoading] = useState(false);
  const [hasLoaded, setHasLoaded] = useState(false);

  // Monotonic counter to discard stale responses from overlapping requests
  const requestIdRef = useRef(0);
  // Track whether we've loaded at least once to suppress loading flash on poll refreshes
  const hasLoadedRef = useRef(false);

  const fetchCounts = useCallback(async () => {
    const thisRequestId = ++requestIdRef.current;

    // Only show loading spinner on first fetch, not subsequent poll refreshes
    if (!hasLoadedRef.current) {
      setIsLoading(true);
    }

    try {
      const request = create(GetMinerStateCountsRequestSchema, {});
      const response = await fleetManagementClient.getMinerStateCounts(request);

      // Discard stale response if a newer request was issued
      if (thisRequestId !== requestIdRef.current) return;

      setTotalMiners(response.totalMiners);
      setStateCounts(response.stateCounts);
    } catch (error) {
      if (thisRequestId !== requestIdRef.current) return;

      handleAuthErrors({
        error: error,
        onError: (err) => {
          console.error("Error fetching miner state counts:", err);
        },
      });
    } finally {
      if (thisRequestId === requestIdRef.current) {
        setIsLoading(false);
        hasLoadedRef.current = true;
        setHasLoaded(true);
      }
    }
  }, [handleAuthErrors]);

  // Fetch on mount only — polling handles subsequent refreshes
  const hasFetchedRef = useRef(false);
  useEffect(() => {
    if (hasFetchedRef.current) return;
    hasFetchedRef.current = true;
    void fetchCounts();
  }, [fetchCounts]);

  // Polling
  useEffect(() => {
    if (!options?.pollIntervalMs) return;

    const intervalId = setInterval(() => {
      void fetchCounts();
    }, options.pollIntervalMs);

    return () => clearInterval(intervalId);
  }, [options?.pollIntervalMs, fetchCounts]);

  const refetch = useCallback(() => {
    void fetchCounts();
  }, [fetchCounts]);

  return {
    totalMiners,
    stateCounts,
    isLoading,
    hasLoaded,
    refetch,
  };
};

export default useFleetCounts;
