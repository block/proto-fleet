import { useState } from "react";
import { containersClient } from "./clients";
import type { Container } from "./generated/containers/v1/containers_pb";
import { getErrorMessage } from "./getErrorMessage";
import { usePoll } from "@/shared/hooks/usePoll";

export function useContainers(siteIds: bigint[], includeUnassigned: boolean, enabled = true) {
  const key = `${siteIds.join(",")}:${includeUnassigned}`;
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
    ? { ...result, loading: false }
    : { containers: [], error: undefined, loading: enabled };
}
