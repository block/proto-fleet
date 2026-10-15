import { useCallback, useState } from "react";
import { containersClient } from "./clients";
import type { Container } from "./generated/containers/v1/containers_pb";
import { getErrorMessage } from "./getErrorMessage";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { usePoll } from "@/shared/hooks/usePoll";

export function useContainers(siteIds: bigint[], includeUnassigned: boolean, enabled = true) {
  const [refreshKey, setRefreshKey] = useState(0);
  const refetch = useCallback(() => setRefreshKey((key) => key + 1), []);
  const revision = useFleetStore((state) => state.ui.sitesRevision);
  const key = `${siteIds.join(",")}:${includeUnassigned}:${revision}:${refreshKey}`;
  const [result, setResult] = useState<{ key: string; containers: Container[]; error?: string }>();
  usePoll({
    enabled,
    poll: true,
    params: key,
    fetchData: async (signal) => {
      try {
        const response = await containersClient.listContainers({ siteIds, includeUnassigned }, { signal });
        if (!signal.aborted) setResult({ key, containers: response.containers });
      } catch (error) {
        if (!signal.aborted) setResult({ key, containers: [], error: getErrorMessage(error) });
      }
    },
  });
  return enabled && result?.key === key
    ? { ...result, loading: false, refetch }
    : { containers: [], error: undefined, loading: enabled, refetch };
}
