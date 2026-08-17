import { useCallback, useEffect, useMemo, useState } from "react";

import type { TimeSeriesLevel, TimeSeriesQuery } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useMinerStore } from "@/protoOS/store";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import { type Duration, getDurationMs } from "@/shared/components/DurationSelector";
import { usePoll } from "@/shared/hooks/usePoll";

/**
 * Get appropriate data interval based on duration
 */
function getIntervalMinutes(duration: Duration): number {
  switch (duration) {
    case "1h":
      return 1;
    case "12h":
      return 5;
    case "24h":
      return 15;
    case "48h":
      return 30;
    case "5d":
      return 60;
    default:
      return 15;
  }
}

interface UseTimeSeriesProps {
  duration: Duration;
  levels: TimeSeriesLevel[];
  poll?: boolean;
  pollIntervalMs?: number;
}

const useTimeSeries = ({ duration, levels, poll = true, pollIntervalMs = 30 * 1000 }: UseTimeSeriesProps) => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const data = useMinerStore((state) => state.telemetry);
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  // Get hashboard count from the hardware slice to trigger telemetry fetch
  // Using count instead of keys array to prevent infinite re-renders from array recreation
  const hashboardCount = useMinerStore((state) => state.hardware.hashboards.size);

  const fetchData = useCallback(async () => {
    if (!adapter) {
      return;
    }

    const currentHashboards = Array.from(useMinerStore.getState().hardware.hashboards.keys());

    if (currentHashboards.length === 0) {
      return;
    }

    setPending(true);
    const accept = beginStoreWrite(`telemetry:history:${duration}:${levels.join(",")}`);
    setError(undefined);

    // Capture the current duration at the start of the request
    const requestDuration = duration;

    // Calculate time window on every fetch
    const timeRangeMs = getDurationMs(duration);
    const intervalMinutes = getIntervalMinutes(duration);
    const startTime = new Date(Date.now() - timeRangeMs);

    const request: TimeSeriesQuery = {
      startTime: startTime.toISOString(),
      duration: `PT${Math.floor(timeRangeMs / (60 * 1000))}M`,
      interval: `PT${intervalMinutes}M`,
      levels,
      aggregation: "mean",
    };

    await authRetry({
      request: (params) => adapter.getTimeSeries(request, params),
      onSuccess: (normalized) => {
        // Only update if the duration hasn't changed since we started the request
        const currentDuration = useMinerStore.getState().ui.duration;
        if (requestDuration !== currentDuration) {
          return;
        }
        if (!accept()) return;

        useMinerStore.getState().telemetry.mergeTelemetry(normalized);
      },
      onError: (err) => {
        setError(err?.error?.message ?? "Unknown error occurred");
      },
    }).finally(() => {
      setPending(false);
    });
  }, [duration, levels, adapter, authRetry]);

  // Trigger fetch when hashboards change or when request dependencies change.
  useEffect(() => {
    if (hashboardCount > 0) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- fetch on deps change; setState inside async fetch is the external-sync pattern
      fetchData();
    }
  }, [hashboardCount, fetchData]);

  // Memoize params to prevent recreating object on every render
  const pollParams = useMemo(() => ({ duration, levels }), [duration, levels]);

  usePoll({
    fetchData,
    params: pollParams,
    poll,
    pollIntervalMs,
  });

  return useMemo(
    () => ({
      pending,
      error,
      data,
    }),
    [pending, error, data],
  );
};

export { useTimeSeries };
