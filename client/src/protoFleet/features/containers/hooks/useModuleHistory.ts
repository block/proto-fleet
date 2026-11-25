import { useState } from "react";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { fleetInventoryClient } from "@/protoFleet/api/clients";
import type { ModuleSample } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";
import { type FleetDuration, getFleetDurationMs } from "@/shared/components/DurationSelector";
import { useMountedCallback } from "@/shared/hooks/useMountedCallback";
import { usePoll } from "@/shared/hooks/usePoll";

export type ModuleHistorySample = ModuleSample;

export function useModuleHistory(moduleId: bigint, duration: FleetDuration, enabled: boolean) {
  const { handleAuthErrors } = useAuthErrors();
  const [revision, setRevision] = useState(0);
  const key = `${moduleId}:${duration}`;
  const [result, setResult] = useState<{
    key: string;
    samples: ModuleHistorySample[];
    endTime?: number;
    error?: string;
  }>();
  usePoll({
    enabled,
    poll: true,
    pollIntervalMs: 60_000,
    params: `${key}:${revision}`,
    fetchData: async (signal) => {
      try {
        const end = new Date();
        const window = {
          startTime: timestampFromDate(new Date(end.getTime() - getFleetDurationMs(duration))),
          endTime: timestampFromDate(end),
          pageSize: 1000,
        };
        const samples: ModuleHistorySample[] = [];
        const seen = new Set<string>();
        let pageToken = "";
        do {
          signal.throwIfAborted();
          const options = { signal, timeoutMs: 10_000 };
          const request = { ...window, pageToken };
          const response = await fleetInventoryClient.listModuleHistory({ ...request, moduleId }, options);
          samples.push(...response.samples);
          pageToken = response.nextPageToken;
          // Never render a silently truncated window; bound even malformed cursor streams.
          if (pageToken && (seen.has(pageToken) || seen.size >= 19))
            throw new Error("History could not be loaded completely. Try a shorter time range.");
          seen.add(pageToken);
        } while (pageToken);
        if (!signal.aborted) setResult({ key, samples, endTime: end.getTime() });
      } catch (error) {
        if (!signal.aborted)
          handleAuthErrors({ error, onError: () => setResult({ key, samples: [], error: getErrorMessage(error) }) });
      }
    },
  });
  const current = result?.key === key ? result : undefined;
  return {
    samples: enabled ? current?.samples : [],
    endTime: enabled ? current?.endTime : undefined,
    error: enabled ? current?.error : undefined,
    retry: useMountedCallback(() => setRevision((value) => value + 1)),
  };
}
