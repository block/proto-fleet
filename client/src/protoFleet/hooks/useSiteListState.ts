import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { sitesClient } from "@/protoFleet/api/clients";
import { type GetSiteStatsResponse, type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isPermissionDeniedError } from "@/protoFleet/api/requestErrors";
import { fetchStatsWithConcurrency } from "@/protoFleet/hooks/fetchStatsWithConcurrency";
import { useAuthErrors } from "@/protoFleet/store";

type UseSiteListStateOptions = {
  enabled?: boolean;
};

export function useSiteListState(
  sites: SiteWithCounts[] | undefined,
  { enabled = true }: UseSiteListStateOptions = {},
) {
  const { handleAuthErrors } = useAuthErrors();
  const [statsMap, setStatsMap] = useState<Map<bigint, GetSiteStatsResponse>>(new Map());
  const [statsError, setStatsError] = useState<string | null>(null);
  const requestIdRef = useRef(0);

  const siteIds = useMemo(
    () => sites?.map((row) => row.site?.id).filter((id): id is bigint => !!id && id > 0n) ?? [],
    [sites],
  );
  const refetchStats = useCallback(async () => {
    const requestId = ++requestIdRef.current;
    if (!enabled || siteIds.length === 0) {
      setStatsMap(new Map());
      setStatsError(null);
      return;
    }

    const results = await fetchStatsWithConcurrency(siteIds, (siteId) => sitesClient.getSiteStats({ siteId }));
    if (requestId !== requestIdRef.current) return;

    const next = new Map<bigint, GetSiteStatsResponse>();
    let firstError: unknown;
    for (const result of results) {
      if (result.status === "fulfilled") {
        next.set(result.value.siteId, result.value);
      } else if (!isPermissionDeniedError(result.reason) && firstError === undefined) {
        firstError = result.reason;
      }
    }
    setStatsMap(next);

    if (firstError === undefined) {
      setStatsError(null);
      return;
    }
    handleAuthErrors({
      error: firstError,
      onError: () => setStatsError(getErrorMessage(firstError)),
    });
  }, [enabled, handleAuthErrors, siteIds]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- external stats sync after the visible site list changes
    void refetchStats();
  }, [refetchStats]);

  return { statsMap, statsError, refetchStats };
}
