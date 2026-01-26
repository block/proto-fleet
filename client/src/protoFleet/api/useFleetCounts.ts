import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import { GetMinerStateCountsRequestSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { MinerStateCounts } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useAuthErrors } from "@/protoFleet/store";

type UseFleetCountsReturn = {
  /** Total number of miners */
  totalMiners: number;
  /** Counts of miners in different states */
  stateCounts: MinerStateCounts | undefined;
  /** Whether the hook is currently loading data */
  isLoading: boolean;
  /** Whether the initial load has completed */
  hasInitialLoadCompleted: boolean;
  /** Refetch the counts */
  refetch: () => void;
};

/**
 * Hook for fetching miner state counts without loading full miner data.
 * More efficient than useFleet when only counts are needed (e.g., Dashboard).
 *
 * @example
 * ```tsx
 * const { totalMiners, stateCounts, isLoading } = useFleetCounts();
 *
 * // Display counts
 * <div>Total: {totalMiners}</div>
 * <div>Hashing: {stateCounts?.hashingCount ?? 0}</div>
 * <div>Offline: {stateCounts?.offlineCount ?? 0}</div>
 * ```
 */
const useFleetCounts = (): UseFleetCountsReturn => {
  const { handleAuthErrors } = useAuthErrors();

  const [totalMiners, setTotalMiners] = useState(0);
  const [stateCounts, setStateCounts] = useState<MinerStateCounts | undefined>(undefined);
  const [isLoading, setIsLoading] = useState(false);
  const [hasInitialLoadCompleted, setHasInitialLoadCompleted] = useState(false);

  const fetchCounts = useCallback(async () => {
    setIsLoading(true);

    try {
      const request = create(GetMinerStateCountsRequestSchema, {});
      const response = await fleetManagementClient.getMinerStateCounts(request);

      setTotalMiners(response.totalMiners);
      setStateCounts(response.stateCounts);
    } catch (error) {
      handleAuthErrors({
        error: error,
        onError: (err) => {
          console.error("Error fetching miner state counts:", err);
        },
      });
    } finally {
      setIsLoading(false);
      setHasInitialLoadCompleted(true);
    }
  }, [handleAuthErrors]);

  // Track if this is the initial load
  const hasLoadedRef = useRef(false);

  // Fetch data on mount only - streaming provides real-time updates after that
  useEffect(() => {
    if (hasLoadedRef.current) {
      return;
    }
    hasLoadedRef.current = true;
    void fetchCounts();
  }, [fetchCounts]);

  const refetch = useCallback(() => {
    if (!isLoading) {
      void fetchCounts();
    }
  }, [isLoading, fetchCounts]);

  return {
    totalMiners,
    stateCounts,
    isLoading,
    hasInitialLoadCompleted,
    refetch,
  };
};

export default useFleetCounts;
