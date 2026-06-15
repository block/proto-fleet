import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { buildingsClient } from "@/protoFleet/api/clients";
import {
  type BuildingWithCounts,
  type GetBuildingStatsResponse,
} from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isPermissionDeniedError } from "@/protoFleet/api/requestErrors";
import { fetchStatsWithConcurrency } from "@/protoFleet/hooks/fetchStatsWithConcurrency";
import { useAuthErrors } from "@/protoFleet/store";

type UseBuildingListStateOptions = {
  enabled?: boolean;
};

export function useBuildingListState(
  buildings: BuildingWithCounts[] | undefined,
  { enabled = true }: UseBuildingListStateOptions = {},
) {
  const { handleAuthErrors } = useAuthErrors();
  const [statsMap, setStatsMap] = useState<Map<bigint, GetBuildingStatsResponse>>(new Map());
  const [statsError, setStatsError] = useState<string | null>(null);
  const requestIdRef = useRef(0);

  const buildingIds = useMemo(
    () => buildings?.map((row) => row.building?.id).filter((id): id is bigint => !!id && id > 0n) ?? [],
    [buildings],
  );
  const refetchStats = useCallback(async () => {
    const requestId = ++requestIdRef.current;
    if (!enabled || buildingIds.length === 0) {
      setStatsMap(new Map());
      setStatsError(null);
      return;
    }

    const results = await fetchStatsWithConcurrency(buildingIds, (buildingId) =>
      buildingsClient.getBuildingStats({ buildingId }),
    );
    if (requestId !== requestIdRef.current) return;

    const next = new Map<bigint, GetBuildingStatsResponse>();
    let firstError: unknown;
    for (const result of results) {
      if (result.status === "fulfilled") {
        next.set(result.value.buildingId, result.value);
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
  }, [buildingIds, enabled, handleAuthErrors]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- external stats sync after the visible building list changes
    void refetchStats();
  }, [refetchStats]);

  return { statsMap, statsError, refetchStats };
}
