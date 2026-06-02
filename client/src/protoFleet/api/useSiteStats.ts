import { useCallback, useRef, useState } from "react";

import { sitesClient } from "@/protoFleet/api/clients";
import { type GetSiteStatsResponse } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useAuthErrors } from "@/protoFleet/store";
import { usePoll } from "@/shared/hooks/usePoll";

interface UseSiteStatsOptions {
  siteId: bigint;
  // When false the hook skips fetching and stays in the loading state.
  // Use to defer until the parent has a real site id.
  enabled?: boolean;
  pollIntervalMs?: number;
}

interface UseSiteStatsReturn {
  stats: GetSiteStatsResponse | undefined;
  isLoading: boolean;
  hasLoaded: boolean;
  error: string | null;
  refetch: () => void;
}

/**
 * Wraps SiteService.GetSiteStats with the standard
 * loading / hasLoaded / poll-without-spinner pattern. Schedules its
 * polling via the shared `usePoll` hook so the cadence matches other
 * polled hooks (useTelemetry, useFleetCounts, etc.).
 *
 * Scope is server-side: every device with site_id matching the request
 * (racked or directly site-attached).
 */
export const useSiteStats = ({ siteId, enabled = true, pollIntervalMs }: UseSiteStatsOptions): UseSiteStatsReturn => {
  const { handleAuthErrors } = useAuthErrors();
  const [stats, setStats] = useState<GetSiteStatsResponse | undefined>(undefined);
  const [isLoading, setIsLoading] = useState(false);
  const [hasLoaded, setHasLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const requestIdRef = useRef(0);
  const hasLoadedRef = useRef(false);

  // Reset on scope change so a stale response from a previous site can't
  // land. Sync-during-render mirrors useTelemetryMetrics.
  const scopeKey = `${siteId.toString()}|${enabled ? "on" : "off"}`;
  const prevScopeRef = useRef(scopeKey);
  if (prevScopeRef.current !== scopeKey) {
    prevScopeRef.current = scopeKey;
    ++requestIdRef.current;
    hasLoadedRef.current = false;
    setHasLoaded(false);
    setStats(undefined);
    // Reset error too — otherwise an error banner from a previous site
    // stays visible until the new fetch lands, even though it's stale.
    setError(null);
  }

  const fetchStats = useCallback(async () => {
    if (!enabled || siteId === 0n) {
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
      const response = await sitesClient.getSiteStats({ siteId });
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
          console.error("[useSiteStats] failed to fetch site stats", { siteId: siteId.toString(), error: message });
        },
      });
    } finally {
      if (thisRequestId === requestIdRef.current) {
        setIsLoading(false);
      }
    }
  }, [siteId, enabled, handleAuthErrors]);

  // `usePoll` runs an initial fetch when enabled and schedules follow-ups
  // when `poll` is true. Keying on scopeKey + pollIntervalMs forces a
  // re-run when the caller flips the site or toggles polling.
  usePoll({
    fetchData: fetchStats,
    params: scopeKey,
    poll: pollIntervalMs !== undefined,
    pollIntervalMs,
    enabled,
  });

  return { stats, isLoading, hasLoaded, error, refetch: fetchStats };
};
