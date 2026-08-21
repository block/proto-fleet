import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import { GetMinerStateCountsRequestSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { MinerStateCounts } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useAuthErrors } from "@/protoFleet/store";

// Stable empty default so an absent siteIds option doesn't change scopeKey
// identity between renders.
const EMPTY_SITE_IDS: bigint[] = [];

interface UseFleetCountsOptions {
  pollIntervalMs?: number;
  /** Gate the hook while a parent scope is still being validated. */
  enabled?: boolean;
  /** Scope counts to specific sites (OR). Empty = all sites. */
  siteIds?: bigint[];
  /** Include miners with no site assignment (site_id IS NULL). */
  includeUnassigned?: boolean;
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

  const enabled = options?.enabled ?? true;
  const siteIds = options?.siteIds ?? EMPTY_SITE_IDS;
  const includeUnassigned = options?.includeUnassigned ?? false;
  // Stable key for the requested site scope; changing it re-fetches and
  // discards in-flight responses from the previous scope.
  const scopeKey = `${enabled ? "on" : "off"}|${siteIds.map(String).join(",")}|${includeUnassigned}`;

  const [totalMiners, setTotalMiners] = useState(0);
  const [stateCounts, setStateCounts] = useState<MinerStateCounts | undefined>(undefined);
  const [isLoading, setIsLoading] = useState(false);
  const [hasLoaded, setHasLoaded] = useState(false);

  // Monotonic counter to discard stale responses from overlapping requests
  const requestIdRef = useRef(0);
  // Track whether we've loaded at least once to suppress loading flash on poll refreshes
  const hasLoadedRef = useRef(false);
  // Ref so fetchCounts reads the latest scope without being recreated each render.
  const scopeRef = useRef({ siteIds, includeUnassigned });
  scopeRef.current = { siteIds, includeUnassigned };

  // Reset on scope change so we never show the previous site's counts and
  // any in-flight request from the old scope is invalidated. Adjust-during-
  // render keeps the reset in the same pass that detects the change.
  const prevScopeRef = useRef(scopeKey);
  if (prevScopeRef.current !== scopeKey) {
    prevScopeRef.current = scopeKey;
    ++requestIdRef.current;
    hasLoadedRef.current = false;
    setHasLoaded(false);
    setStateCounts(undefined);
    // Reset totalMiners too: otherwise a telemetry refetch can land first and
    // panels compute "X of Y reporting" mixing the new scope's deviceCount
    // with the previous scope's total.
    setTotalMiners(0);
  }

  const fetchCounts = useCallback(async () => {
    if (!enabled) {
      ++requestIdRef.current;
      setIsLoading(false);
      return;
    }

    const thisRequestId = ++requestIdRef.current;

    // Only show loading spinner on first fetch, not subsequent poll refreshes
    if (!hasLoadedRef.current) {
      setIsLoading(true);
    }

    try {
      const request = create(GetMinerStateCountsRequestSchema, {
        siteIds: scopeRef.current.siteIds,
        includeUnassigned: scopeRef.current.includeUnassigned,
      });
      const response = await fleetManagementClient.getMinerStateCounts(request);

      // Discard stale response if a newer request was issued
      if (thisRequestId !== requestIdRef.current) return;

      setTotalMiners(response.totalMiners);
      setStateCounts(response.stateCounts);
      hasLoadedRef.current = true;
      setHasLoaded(true);
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
      }
    }
  }, [enabled, handleAuthErrors]);

  // Fetch on mount and whenever the site scope changes; polling handles
  // subsequent refreshes within a scope.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch + refetch on scope change; setState inside async fetch is the external-sync pattern
    void fetchCounts();
  }, [fetchCounts, scopeKey]);

  // Polling
  useEffect(() => {
    if (!options?.pollIntervalMs || !enabled) return;

    const intervalId = setInterval(() => {
      void fetchCounts();
    }, options.pollIntervalMs);

    return () => clearInterval(intervalId);
  }, [options?.pollIntervalMs, enabled, fetchCounts]);

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
