import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  DeviceStatus,
  ListMinerStateSnapshotsRequestSchema,
  MinerListFilterSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useAuthErrors } from "@/protoFleet/store";

type UsePoolNeededCountReturn = {
  /** Total number of miners that need pool configuration */
  poolNeededCount: number;
  /** Whether the hook is currently loading data */
  isLoading: boolean;
  /** Whether the initial load has completed */
  hasInitialLoadCompleted: boolean;
  /** Refetch the count */
  refetch: () => void;
};

/**
 * Hook for fetching the count of miners that need mining pool configuration.
 *
 * @example
 * ```tsx
 * const { poolNeededCount, isLoading } = usePoolNeededCount();
 *
 * // Display count
 * {poolNeededCount > 0 && <div>{poolNeededCount} miners need pools</div>}
 * ```
 */
const usePoolNeededCount = (): UsePoolNeededCountReturn => {
  const { handleAuthErrors } = useAuthErrors();

  const [poolNeededCount, setPoolNeededCount] = useState(0);
  const [isLoading, setIsLoading] = useState(false);
  const [hasInitialLoadCompleted, setHasInitialLoadCompleted] = useState(false);
  const isLoadingRef = useRef(false);
  const fetchCountRef = useRef<(() => Promise<void>) | null>(null);

  // Fetch only the count (lightweight, single page request)
  const fetchCount = useCallback(async () => {
    setIsLoading(true);
    isLoadingRef.current = true;

    try {
      // Create filter for NEEDS_MINING_POOL status with PAIRED pairing status
      const filter = create(MinerListFilterSchema, {
        deviceStatus: [DeviceStatus.NEEDS_MINING_POOL],
        pairingStatuses: [PairingStatus.PAIRED],
      });

      // Fetch only first page to get total count
      const request = create(ListMinerStateSnapshotsRequestSchema, {
        pageSize: 1, // Minimal page size since we only need the count
        cursor: "",
        filter,
      });

      const response = await fleetManagementClient.listMinerStateSnapshots(request);
      setPoolNeededCount(response.totalMiners);
    } catch (error) {
      handleAuthErrors({
        error: error,
        onError: (err) => {
          console.error("[usePoolNeededCount] Error fetching pool needed count:", err);
        },
      });
    } finally {
      setIsLoading(false);
      isLoadingRef.current = false;
      setHasInitialLoadCompleted(true);
    }
  }, [handleAuthErrors]);

  // Store fetchCount in a ref so refetch callback can access latest version without changing identity
  useEffect(() => {
    fetchCountRef.current = fetchCount;
  });

  // Track if this is the initial load
  const hasLoadedRef = useRef(false);

  // Fetch data on mount
  useEffect(() => {
    if (hasLoadedRef.current) {
      return;
    }
    hasLoadedRef.current = true;
    void fetchCount();
  }, [fetchCount]);

  // Use ref-based approach to keep callback stable while accessing latest fetchCount
  const refetch = useCallback(() => {
    if (!isLoadingRef.current && fetchCountRef.current) {
      void fetchCountRef.current();
    }
  }, []);

  return {
    poolNeededCount,
    isLoading,
    hasInitialLoadCompleted,
    refetch,
  };
};

export default usePoolNeededCount;
