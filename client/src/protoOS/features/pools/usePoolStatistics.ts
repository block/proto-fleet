import { useMemo, useState } from "react";
import type { PoolHistoryResult, PoolRecordedShares } from "@/protoOS/api/adapters/poolHistory";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { usePoolsInfo, usePoolsInfoResource } from "@/protoOS/store/hooks/usePools";
import { type Duration, durations, durationToMs } from "@/shared/components/DurationSelector/constants";
import { usePoll } from "@/shared/hooks/usePoll";

export const poolDurations = durations;
export type PoolDuration = Duration;
// The retained share-history API accepts only aligned hour/day buckets.
const intervalMs = 3_600_000;

interface Selection {
  adapter: ReturnType<typeof useMinerHosting>["adapter"];
  duration: PoolDuration;
  poolIdsKey: string | undefined;
}

interface Snapshot {
  selection: Selection;
  history?: PoolHistoryResult;
  pending: boolean;
  error?: string;
}

interface RecordedShareSnapshot {
  selection: Pick<Selection, "adapter" | "poolIdsKey">;
  byPool?: PoolRecordedShares;
  error?: string;
}

export function usePoolStatistics(duration: PoolDuration) {
  const { adapter } = useMinerHosting();
  // AuthenticatedShell owns pool configuration/diagnostics polling. Read its
  // snapshot without mounting another fetching hook or starting another poll.
  const pools = usePoolsInfo();
  const poolsResource = usePoolsInfoResource();
  const authRetry = useAuthRetry();
  const poolIdsKey = pools === undefined ? undefined : JSON.stringify(pools.map(({ id }) => id).sort());
  const params = useMemo(() => ({ adapter, duration, poolIdsKey }), [adapter, duration, poolIdsKey]);
  const [snapshot, setSnapshot] = useState<Snapshot>();
  const shareParams = useMemo(() => ({ adapter, poolIdsKey }), [adapter, poolIdsKey]);
  const [recordedShares, setRecordedShares] = useState<RecordedShareSnapshot>();
  usePoll({
    params,
    enabled: Boolean(adapter) && pools !== undefined,
    poll: true,
    pollIntervalMs: 30_000,
    fetchData: async (signal) => {
      if (!adapter || !pools) return;
      setSnapshot((previous) => ({
        selection: params,
        pending: true,
        history: previous?.selection === params ? previous.history : undefined,
        error: previous?.selection === params ? previous.error : undefined,
      }));
      await authRetry({
        signal,
        request: async (options) => {
          signal.throwIfAborted();
          const end = Math.floor(Date.now() / intervalMs) * intervalMs;
          return adapter.getPoolHistory
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
        },
        onSuccess: (history) => {
          if (!signal.aborted) setSnapshot({ selection: params, history, pending: false });
        },
        onError: () => {
          if (!signal.aborted)
            setSnapshot((previous) => ({
              ...previous,
              selection: params,
              pending: false,
              error: "Could not refresh pool statistics. Displayed values may be out of date.",
            }));
        },
      });
    },
  });
  // Share results are queried only while this page is mounted. The global
  // 15-second pool poll keeps using the unmodified, low-cost diagnostics call.
  usePoll({
    params: shareParams,
    enabled: Boolean(adapter?.getPoolRecordedShares) && pools !== undefined,
    poll: true,
    pollIntervalMs: 30_000,
    fetchData: async (signal) => {
      if (!adapter?.getPoolRecordedShares || !pools) return;
      await authRetry({
        signal,
        request: (options) => adapter.getPoolRecordedShares!(options),
        onSuccess: (byPool) => {
          if (!signal.aborted) setRecordedShares({ selection: shareParams, byPool });
        },
        onError: () => {
          if (!signal.aborted)
            setRecordedShares((previous) => ({
              selection: shareParams,
              byPool: previous?.selection === shareParams ? previous.byPool : undefined,
              error: "Could not refresh last recorded share results. Displayed values may be out of date.",
            }));
        },
      });
    },
  });
  const current = snapshot?.selection === params ? snapshot : undefined;
  const currentShares = recordedShares?.selection === shareParams ? recordedShares : undefined;
  const poolsError =
    poolsResource?.status === "error"
      ? pools === undefined
        ? "Could not load pool information. Retrying automatically."
        : "Could not refresh pool information. Displayed pool details may be out of date."
      : undefined;
  return {
    pools:
      adapter && currentShares?.byPool
        ? pools?.map((pool) => {
            const share = currentShares.byPool?.[pool.id];
            return share
              ? {
                  ...pool,
                  lastShareAtMs: share.resultAtMs,
                  lastAssignedDifficulty: share.assignedDifficulty,
                  lastShareOutcome: share.outcome,
                }
              : pool;
          })
        : adapter
          ? pools
          : undefined,
    history: current?.history,
    pending: pools === undefined ? !poolsError : (current?.pending ?? true),
    error: [poolsError, current?.error, currentShares?.error].filter(Boolean).join(" ") || undefined,
  };
}
