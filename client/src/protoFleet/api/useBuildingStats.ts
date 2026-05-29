import { useCallback, useRef, useState } from "react";

import { buildingsClient } from "@/protoFleet/api/clients";
import { type GetBuildingStatsResponse } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { useAuthErrors } from "@/protoFleet/store";
import { usePoll } from "@/shared/hooks/usePoll";

interface UseBuildingStatsOptions {
  buildingId: bigint;
  enabled?: boolean;
  pollIntervalMs?: number;
}

interface UseBuildingStatsReturn {
  stats: GetBuildingStatsResponse | undefined;
  isLoading: boolean;
  hasLoaded: boolean;
  error: string | null;
  refetch: () => void;
}

/**
 * Wraps BuildingService.GetBuildingStats. The response carries both the
 * building-level rollup and a `rackHealth` list keyed by rack_id +
 * rack_label so the BuildingCard floor plan can paint per-cell state
 * without a separate listBuildingRacks fetch.
 */
export const useBuildingStats = ({
  buildingId,
  enabled = true,
  pollIntervalMs,
}: UseBuildingStatsOptions): UseBuildingStatsReturn => {
  const { handleAuthErrors } = useAuthErrors();
  const [stats, setStats] = useState<GetBuildingStatsResponse | undefined>(undefined);
  const [isLoading, setIsLoading] = useState(false);
  const [hasLoaded, setHasLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const requestIdRef = useRef(0);
  const hasLoadedRef = useRef(false);

  const scopeKey = `${buildingId.toString()}|${enabled ? "on" : "off"}`;
  const prevScopeRef = useRef(scopeKey);
  if (prevScopeRef.current !== scopeKey) {
    prevScopeRef.current = scopeKey;
    ++requestIdRef.current;
    hasLoadedRef.current = false;
    setHasLoaded(false);
    setStats(undefined);
    // Reset error too — otherwise an error banner from a previous building
    // stays visible until the new fetch lands.
    setError(null);
  }

  const fetchStats = useCallback(async () => {
    if (!enabled || buildingId === 0n) {
      ++requestIdRef.current;
      setIsLoading(false);
      return;
    }

    const thisRequestId = ++requestIdRef.current;
    if (!hasLoadedRef.current) {
      setIsLoading(true);
    }
    setError(null);

    try {
      const response = await buildingsClient.getBuildingStats({ buildingId });
      if (thisRequestId !== requestIdRef.current) return;
      setStats(response);
      hasLoadedRef.current = true;
      setHasLoaded(true);
    } catch (err) {
      if (thisRequestId !== requestIdRef.current) return;
      handleAuthErrors({
        error: err,
        onError: () => {
          const message = err instanceof Error ? err.message : String(err);
          setError(message);
          console.error("[useBuildingStats] failed to fetch building stats", {
            buildingId: buildingId.toString(),
            error: message,
          });
        },
      });
    } finally {
      if (thisRequestId === requestIdRef.current) {
        setIsLoading(false);
      }
    }
  }, [buildingId, enabled, handleAuthErrors]);

  // `usePoll` runs an initial fetch when enabled and schedules follow-ups
  // when `poll` is true. Keying on scopeKey forces a re-run when the
  // caller flips the building or toggles polling.
  usePoll({
    fetchData: fetchStats,
    params: scopeKey,
    poll: pollIntervalMs !== undefined,
    pollIntervalMs,
    enabled,
  });

  return { stats, isLoading, hasLoaded, error, refetch: fetchStats };
};
