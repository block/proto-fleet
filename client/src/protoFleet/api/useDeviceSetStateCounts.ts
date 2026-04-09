import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  ListMinerStateSnapshotsRequestSchema,
  MinerListFilterSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { MinerStateCounts } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useAuthErrors } from "@/protoFleet/store";

interface DeviceSetFilter {
  groupIds?: bigint[];
  rackIds?: bigint[];
}

interface UseDeviceSetStateCountsReturn {
  totalMiners: number;
  stateCounts: MinerStateCounts | undefined;
  hasInitialLoadCompleted: boolean;
}

/**
 * Hook for fetching miner state counts scoped to a group or rack.
 * Uses ListMinerStateSnapshots with pageSize=1 and a group/rack filter
 * to efficiently retrieve just the totalStateCounts.
 *
 * Callers must memoize the filter object (e.g. via useMemo) so that
 * the effect only re-runs when the filter actually changes.
 */
const useDeviceSetStateCounts = (filter: DeviceSetFilter | null): UseDeviceSetStateCountsReturn => {
  const { handleAuthErrors } = useAuthErrors();

  const [totalMiners, setTotalMiners] = useState(0);
  const [stateCounts, setStateCounts] = useState<MinerStateCounts | undefined>(undefined);
  const [hasInitialLoadCompleted, setHasInitialLoadCompleted] = useState(false);

  const requestIdRef = useRef(0);

  const fetchCounts = useCallback(
    async (currentFilter: DeviceSetFilter, requestId: number) => {
      try {
        const request = create(ListMinerStateSnapshotsRequestSchema, {
          pageSize: 1,
          filter: create(MinerListFilterSchema, {
            groupIds: currentFilter.groupIds ?? [],
            rackIds: currentFilter.rackIds ?? [],
          }),
        });
        const response = await fleetManagementClient.listMinerStateSnapshots(request);

        if (requestIdRef.current !== requestId) return;

        setTotalMiners(response.totalMiners);
        setStateCounts(response.totalStateCounts);
        setHasInitialLoadCompleted(true);
      } catch (error) {
        if (requestIdRef.current !== requestId) return;

        handleAuthErrors({
          error: error,
          onError: (err: unknown) => {
            console.error("Error fetching device set state counts:", err);
          },
        });
      }
    },
    [handleAuthErrors],
  );

  useEffect(() => {
    if (!filter) return;

    const requestId = ++requestIdRef.current;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- Clearing stale state before async fetch; no cascade risk since these state vars are not in the dep array
    setStateCounts(undefined);
    setTotalMiners(0);
    setHasInitialLoadCompleted(false);
    void fetchCounts(filter, requestId);
  }, [filter, fetchCounts]);

  return {
    totalMiners,
    stateCounts,
    hasInitialLoadCompleted,
  };
};

export default useDeviceSetStateCounts;
