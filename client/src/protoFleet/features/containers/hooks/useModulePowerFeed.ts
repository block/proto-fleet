import { useState } from "react";
import { containersClient } from "@/protoFleet/api/clients";
import type { GetModulePowerFeedResponse } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";
import { useMountedCallback } from "@/shared/hooks/useMountedCallback";
import { usePoll } from "@/shared/hooks/usePoll";

/**
 * Reads one module's feed when enabled and again on refresh. It never polls:
 * each read holds the container's binding lock until the controller answers,
 * which delays writes to the container. A failed read replaces the last reading.
 */
export function useModulePowerFeed(moduleId: bigint, enabled: boolean) {
  const { handleAuthErrors } = useAuthErrors();
  const [revision, setRevision] = useState(0);
  const key = String(moduleId);
  const [result, setResult] = useState<{
    key: string;
    revision: number;
    data?: GetModulePowerFeedResponse;
    error?: string;
  }>();
  usePoll({
    enabled,
    poll: false,
    params: `${key}:${revision}`,
    fetchData: async (signal) => {
      try {
        const data = await containersClient.getModulePowerFeed({ moduleId }, { signal, timeoutMs: 20_000 });
        if (!signal.aborted) setResult({ key, revision, data });
      } catch (error) {
        if (!signal.aborted)
          handleAuthErrors({ error, onError: () => setResult({ key, revision, error: getErrorMessage(error) }) });
      }
    },
  });
  const current = enabled && result?.key === key ? result : undefined;
  return {
    data: current?.data,
    error: current?.error,
    loading: enabled && !current,
    refreshing: !!current && current.revision !== revision,
    refresh: useMountedCallback(() => setRevision((value) => value + 1)),
  };
}
