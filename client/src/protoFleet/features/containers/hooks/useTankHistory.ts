import { useState } from "react";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { fleetInventoryClient } from "@/protoFleet/api/clients";
import type { Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { getGranularityForDuration } from "@/protoFleet/features/dashboard/utils/granularity";
import { useAuthErrors } from "@/protoFleet/store";
import { type FleetDuration, getFleetDurationMs } from "@/shared/components/DurationSelector";
import { usePoll } from "@/shared/hooks/usePoll";

export function useTankHistory(containerIdentifier: string, tankId: bigint, duration: FleetDuration, enabled = true) {
  const { handleAuthErrors } = useAuthErrors();
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState<{ key: string; metrics: Metric[]; error?: string }>();
  const key = JSON.stringify([containerIdentifier, String(tankId), duration]);
  usePoll({
    enabled,
    params: `${key}:${revision}`,
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
    fetchData: async (signal) => {
      try {
        const end = new Date();
        const start = new Date(end.getTime() - getFleetDurationMs(duration));
        const response = await fleetInventoryClient.getTankHistory(
          {
            containerIdentifier,
            tankId,
            startTime: timestampFromDate(start),
            endTime: timestampFromDate(end),
            granularitySeconds: getGranularityForDuration(duration),
          },
          { signal },
        );
        if (!signal.aborted) setResult({ key, metrics: response.metrics });
      } catch (error) {
        if (!signal.aborted)
          handleAuthErrors({ error, onError: () => setResult({ key, metrics: [], error: getErrorMessage(error) }) });
      }
    },
  });
  const current = result?.key === key ? result : undefined;
  return {
    metrics: !enabled ? [] : current?.metrics,
    error: current?.error,
    retry: () => setRevision((value) => value + 1),
  };
}
