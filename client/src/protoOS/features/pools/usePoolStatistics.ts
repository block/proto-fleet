import { useMemo, useState } from "react";
import type { PoolHistoryResult } from "@/protoOS/api/adapters/poolHistory";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import type { PoolInfo } from "@/protoOS/store/contract";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { type Duration, durations, durationToMs } from "@/shared/components/DurationSelector/constants";
import { usePoll } from "@/shared/hooks/usePoll";

export const poolDurations = durations;
export type PoolDuration = Duration;
const intervals: Record<PoolDuration, number> = {
  "1h": 60_000,
  "12h": 300_000,
  "24h": 900_000,
  "48h": 900_000,
  "5d": 3_600_000,
};

interface Snapshot {
  adapter: ReturnType<typeof useMinerHosting>["adapter"];
  duration: PoolDuration;
  pools?: PoolInfo[];
  history?: PoolHistoryResult;
  pending: boolean;
  error?: string;
}

export function usePoolStatistics(duration: PoolDuration) {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const params = useMemo(() => ({ adapter, duration }), [adapter, duration]);
  const [snapshot, setSnapshot] = useState<Snapshot>();
  usePoll({
    params,
    enabled: Boolean(adapter),
    poll: true,
    pollIntervalMs: 30_000,
    fetchData: async (signal) => {
      if (!adapter) return;
      setSnapshot((previous) => ({
        adapter,
        duration,
        pending: true,
        pools: previous?.adapter === adapter ? previous.pools : undefined,
        history: previous?.adapter === adapter && previous.duration === duration ? previous.history : undefined,
        error: previous?.adapter === adapter && previous.duration === duration ? previous.error : undefined,
      }));
      await authRetry({
        signal,
        request: async (options) => {
          const pools = await adapter.getPools(options);
          signal.throwIfAborted();
          // Keep live identity/status available even if history is slow or fails.
          if (!signal.aborted) setSnapshot((previous) => ({ ...previous, adapter, duration, pools, pending: true }));
          const intervalMs = intervals[duration];
          const end = Math.floor(Date.now() / intervalMs) * intervalMs;
          const history = adapter.getPoolHistory
            ? await adapter.getPoolHistory(
                {
                  poolIds: pools.map((pool) => pool.id),
                  startTime: new Date(end - durationToMs[duration]).toISOString(),
                  endTime: new Date(end).toISOString(),
                  intervalMs,
                },
                options,
              )
            : { byPool: {}, unavailable: pools.map((pool) => pool.id) };
          return { pools, history };
        },
        onSuccess: ({ pools, history }) => {
          if (!signal.aborted) setSnapshot({ adapter, duration, pools, history, pending: false });
        },
        onError: () => {
          if (!signal.aborted)
            setSnapshot((previous) => ({
              ...previous,
              adapter,
              duration,
              pending: false,
              error: "Could not refresh pool statistics. Displayed values may be out of date.",
            }));
        },
      });
    },
  });
  const current = snapshot?.adapter === adapter ? snapshot : undefined;
  return {
    pools: current?.pools,
    history: current?.duration === duration ? current.history : undefined,
    pending: current?.duration === duration ? current.pending : true,
    error: current?.duration === duration ? current.error : undefined,
  };
}
