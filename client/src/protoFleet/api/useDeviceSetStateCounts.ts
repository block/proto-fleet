import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { deviceSetClient } from "@/protoFleet/api/clients";
import { type DeviceSetStats } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useAuthErrors } from "@/protoFleet/store";

interface UseDeviceSetStateCountsOptions {
  deviceSetId: bigint | undefined;
  pollIntervalMs?: number;
}

interface StateCounts {
  hashingCount: number;
  brokenCount: number;
  offlineCount: number;
  sleepingCount: number;
}

interface UseDeviceSetStateCountsReturn {
  totalMiners: number;
  stateCounts: StateCounts | undefined;
  stats: DeviceSetStats | undefined;
  isLoading: boolean;
  hasLoaded: boolean;
  refetch: () => void;
}

export const useDeviceSetStateCounts = ({
  deviceSetId,
  pollIntervalMs,
}: UseDeviceSetStateCountsOptions): UseDeviceSetStateCountsReturn => {
  const { handleAuthErrors } = useAuthErrors();

  const [stats, setStats] = useState<DeviceSetStats | undefined>(undefined);
  const [isLoading, setIsLoading] = useState(false);
  const [hasLoaded, setHasLoaded] = useState(false);

  const requestIdRef = useRef(0);
  const hasLoadedRef = useRef(false);

  // Reset on deviceSetId change — invalidate in-flight requests so stale responses can't land.
  // useState "adjust during render" pattern resets visible state in the same pass that detects
  // the change.
  const [prevId, setPrevId] = useState(deviceSetId);
  if (prevId !== deviceSetId) {
    setPrevId(deviceSetId);
    setHasLoaded(false);
    setStats(undefined);
    // Ref writes must happen synchronously with the id-change detection: deferring to an
    // effect leaves a commit-to-effect window where an in-flight getDeviceSetStats request
    // from the old id still matches the current requestId and can overwrite stats.
    // eslint-disable-next-line react-hooks/refs -- intentional synchronous invalidation; see comment above
    ++requestIdRef.current;
    // eslint-disable-next-line react-hooks/refs -- intentional synchronous invalidation; see comment above
    hasLoadedRef.current = false;
  }

  const fetchStats = useCallback(async () => {
    if (deviceSetId === undefined) {
      ++requestIdRef.current;
      setStats(undefined);
      setIsLoading(false);
      return;
    }

    const thisRequestId = ++requestIdRef.current;

    if (!hasLoadedRef.current) {
      setIsLoading(true);
    }

    try {
      const response = await deviceSetClient.getDeviceSetStats({
        deviceSetIds: [deviceSetId],
      });

      if (thisRequestId !== requestIdRef.current) return;

      const deviceSetStats = response.stats[0];
      setStats(deviceSetStats);
    } catch (error) {
      if (thisRequestId !== requestIdRef.current) return;

      handleAuthErrors({
        error,
        onError: (err) => {
          console.error("Error fetching device set stats:", err);
        },
      });
    } finally {
      if (thisRequestId === requestIdRef.current) {
        setIsLoading(false);
        hasLoadedRef.current = true;
        setHasLoaded(true);
      }
    }
  }, [deviceSetId, handleAuthErrors]);

  // Initial fetch + refetch on deviceSetId change
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch + refetch on deviceSetId change; setState inside async fetch is the external-sync pattern
    fetchStats();
  }, [fetchStats]);

  // Polling
  useEffect(() => {
    if (!pollIntervalMs || deviceSetId === undefined) return;

    const intervalId = setInterval(() => {
      void fetchStats();
    }, pollIntervalMs);

    return () => clearInterval(intervalId);
  }, [pollIntervalMs, deviceSetId, fetchStats]);

  const stateCounts: StateCounts | undefined = useMemo(
    () =>
      stats
        ? {
            hashingCount: stats.hashingCount,
            brokenCount: stats.brokenCount,
            offlineCount: stats.offlineCount,
            sleepingCount: stats.sleepingCount,
          }
        : undefined,
    [stats],
  );

  const totalMiners = stats?.deviceCount ?? 0;

  return {
    totalMiners,
    stateCounts,
    stats,
    isLoading,
    hasLoaded,
    refetch: fetchStats,
  };
};
