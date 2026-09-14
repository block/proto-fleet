import { useCallback, useMemo, useState } from "react";

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
  /**
   * Gates the whole hook. When false, no fetch or poll runs — used to skip the
   * miner-level aggregate series on container module/tank views, which read
   * per-board only.
   * @default true
   */
  enabled?: boolean;
}

const useTimeSeries = ({
  duration,
  levels,
  poll = true,
  pollIntervalMs = 30 * 1000,
  enabled = true,
}: UseTimeSeriesProps) => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const data = useMinerStore((state) => state.telemetry);
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  // Get hashboard count from the hardware slice to trigger telemetry fetch
  // Using count instead of keys array to prevent infinite re-renders from array recreation
  const hashboardCount = useMinerStore((state) => state.hardware.hashboards.size);

  // Callers memoize `levels`, but its identity still churns whenever an unrelated
  // store write replaces the hardware slice the memo depends on. Serialize to the
  // query's contents so equivalent levels neither restart the poll (below) nor
  // rebuild the request object; content-identical levels keep a stable string.
  const levelScope = useMemo(() => JSON.stringify(levels), [levels]);
  const requestLevels = useMemo(() => JSON.parse(levelScope) as TimeSeriesLevel[], [levelScope]);

  const fetchData = useCallback(
    async (signal: AbortSignal) => {
      if (!adapter) {
        return;
      }

      const currentHashboards = Array.from(useMinerStore.getState().hardware.hashboards.keys());

      if (currentHashboards.length === 0) {
        return;
      }

      setPending(true);
      // Independent fields and board selections must not invalidate each other.
      // The telemetry store still preserves newer values for overlapping entities.
      const accept = beginStoreWrite(`telemetry:history:${duration}:${levelScope}`);
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
        levels: requestLevels,
        aggregation: "mean",
      };

      await authRetry({
        request: (params) => adapter.getTimeSeries(request, params),
        signal,
        onSuccess: (normalized) => {
          if (signal.aborted) return;
          // Only update if the duration hasn't changed since we started the request
          const currentDuration = useMinerStore.getState().ui.duration;
          if (requestDuration !== currentDuration) {
            return;
          }
          if (!accept()) return;

          useMinerStore.getState().telemetry.mergeTelemetry(normalized, accept.requestId);
        },
        onError: (err) => {
          setError(err?.error?.message ?? "Unknown error occurred");
        },
      }).finally(() => {
        // A superseded run is aborted by usePoll's cleanup before the newer run
        // starts; don't let its late finally clear the loading flag the newer
        // refetch just set.
        if (!signal.aborted) setPending(false);
      });
    },
    [duration, levelScope, requestLevels, adapter, authRetry],
  );

  // usePoll owns the single fetch path: it fires once on mount, refetches
  // whenever `params` changes, polls on the interval, and cancels each run via
  // its own AbortController. `hashboardCount` is folded into params so boards
  // discovered after mount trigger a refetch even when `levels` carries no
  // explicit indexes (rigs). A prior separate eager useEffect here duplicated
  // every fetch (two waves per mount) and re-fired the whole per-board fan-out
  // on hydration churn — removed in favor of this one path. Keyed on `levelScope`
  // (not the `levels` array) so a hardware-slice replacement that yields
  // content-identical levels doesn't restart the whole per-board fan-out.
  const pollParams = useMemo(
    () => ({ duration, levelScope, adapter, hashboardCount }),
    [duration, levelScope, adapter, hashboardCount],
  );

  usePoll({
    fetchData,
    params: pollParams,
    poll,
    pollIntervalMs,
    enabled: enabled && Boolean(adapter) && hashboardCount > 0,
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
