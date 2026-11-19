import { useMemo, useState } from "react";
import type { PoolDuration } from "./usePoolStatistics";
import { isAuthenticationError } from "@/protoOS/api/adapters/errors";
import type { PoolHistoryResult, PoolShareStatistics } from "@/protoOS/api/adapters/poolHistory";
import type {
  PoolConfigurationReadings,
  PoolConfigurationStatisticsService,
} from "@/protoOS/api/adapters/poolStatistics";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import type { PoolInfo } from "@/protoOS/store/contract";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { durationToMs } from "@/shared/components/DurationSelector/constants";
import { usePoll } from "@/shared/hooks/usePoll";

const livePollIntervalMs = 30_000;
const historyPollIntervalMs = 5 * 60_000;
const historyBucketMs = 3_600_000;

export function configurationShares(pools: PoolInfo[], history?: PoolHistoryResult): PoolShareStatistics {
  const values = pools.map((pool) => history?.byPool[pool.id]);
  if (!values.length || values.some((value) => value?.accepted === undefined || value.rejected === undefined))
    return { partial: true };
  const accepted = values.reduce((sum, value) => sum + value!.accepted!, 0);
  const rejected = values.reduce((sum, value) => sum + value!.rejected!, 0);
  if (!Number.isSafeInteger(accepted + rejected)) return { partial: true };
  return {
    accepted,
    rejected,
    acceptanceRate: accepted + rejected ? accepted / (accepted + rejected) : undefined,
    partial: values.some((value) => value!.partial),
  };
}

export function usePoolConfigurationStatistics(
  service: PoolConfigurationStatisticsService,
  duration: PoolDuration,
  page: number,
  selectedId?: string,
) {
  const { adapter } = useMinerHosting();
  const generation = useMinerStore((state) => state.connectionGeneration);
  const authRetry = useAuthRetry();
  const key = useMemo(() => ({ service, generation }), [service, generation]);
  type Catalogue = Awaited<ReturnType<typeof service.list>>;
  const [catalogue, setCatalogue] = useState<{ key: typeof key; value?: Catalogue; error?: string }>();
  const [refreshCount, setRefreshCount] = useState(0);
  const catalogueParams = useMemo(() => ({ key, refreshCount }), [key, refreshCount]);
  usePoll({
    params: catalogueParams,
    poll: true,
    pollIntervalMs: livePollIntervalMs,
    fetchData: (signal) =>
      authRetry({
        signal,
        request: (options) => service.list(options),
        onSuccess: (value) => {
          if (!signal.aborted) setCatalogue({ key, value });
        },
        onError: () => {
          if (!signal.aborted)
            setCatalogue((previous) => ({
              key,
              value: previous?.key === key ? previous.value : undefined,
              error: "Could not refresh configurations. Displayed values may be out of date.",
            }));
        },
      }),
  });
  const current = catalogue?.key === key ? catalogue : undefined;
  const configurations = current?.value?.configurations ?? [];
  const currentPage = Math.min(page, Math.max(0, Math.ceil(configurations.length / 25) - 1));
  const selected = configurations.find((configuration) => configuration.id === selectedId);
  const entries = selectedId
    ? selected
      ? [selected]
      : []
    : configurations.slice(currentPage * 25, (currentPage + 1) * 25);
  const visiblePage = selectedId ? undefined : currentPage;
  const selection = useMemo(() => ({ key, visiblePage, selectedId }), [key, visiblePage, selectedId]);
  const entryVersions = entries.map(({ id, revision, assignedCount }) => [id, revision, assignedCount]);
  const readingsVersion = `${refreshCount}:${JSON.stringify(entryVersions)}`;
  const [readings, setReadings] = useState<{
    selection: typeof selection;
    configurations?: PoolConfigurationReadings[];
    error?: string;
  }>();
  const readingParams = useMemo(() => ({ selection, readingsVersion }), [selection, readingsVersion]);
  usePoll({
    params: readingParams,
    enabled: current?.value?.enabled === true && entries.length > 0,
    poll: true,
    pollIntervalMs: livePollIntervalMs,
    fetchData: (signal) =>
      authRetry({
        signal,
        request: (options) => service.read(entries, options),
        onSuccess: (configurations) => {
          if (!signal.aborted) setReadings({ selection, configurations });
        },
        onError: () => {
          if (!signal.aborted)
            setReadings((previous) => ({
              selection,
              configurations: previous?.selection === selection ? previous.configurations : undefined,
              error: "Could not refresh configuration statistics. Displayed values may be out of date.",
            }));
        },
      }),
  });
  const currentReadings = readings?.selection === selection ? readings : undefined;
  const readingsById = new Map(currentReadings?.configurations?.map((reading) => [reading.id, reading]));
  const visibleReadings = currentReadings?.configurations
    ? entries.map((entry): PoolConfigurationReadings => {
        const reading = readingsById.get(entry.id);
        if (reading?.revision !== entry.revision || reading.assignedCount !== entry.assignedCount)
          return { ...entry, pools: [], stale: true };
        return reading;
      })
    : undefined;
  const poolIds = visibleReadings?.flatMap((configuration) => configuration.pools.map((pool) => pool.id)) ?? [];
  const historyVersion = `${refreshCount}:${JSON.stringify([...poolIds].sort())}`;
  const historySelection = useMemo(() => ({ selection, adapter, duration }), [selection, adapter, duration]);
  const historyParams = useMemo(() => ({ historySelection, historyVersion }), [historySelection, historyVersion]);
  const [history, setHistory] = useState<{
    selection: typeof historySelection;
    version: string;
    value: PoolHistoryResult;
    error?: string;
  }>();
  // History uses completed hourly buckets and does not need the live poll interval.
  usePoll({
    params: historyParams,
    enabled: current?.value?.enabled === true && poolIds.length > 0,
    poll: true,
    pollIntervalMs: historyPollIntervalMs,
    fetchData: async (signal) => {
      const unavailable = { byPool: {}, unavailable: poolIds };
      await authRetry({
        signal,
        request: async (options) => {
          if (!adapter?.getPoolHistory) return unavailable;
          const end = Math.floor(Date.now() / historyBucketMs) * historyBucketMs;
          return adapter.getPoolHistory(
            {
              poolIds,
              startTime: new Date(end - durationToMs[duration]).toISOString(),
              endTime: new Date(end).toISOString(),
              intervalMs: historyBucketMs,
            },
            options,
          );
        },
        onSuccess: (value) => {
          if (!signal.aborted) setHistory({ selection: historySelection, version: historyVersion, value });
        },
        onError: (cause) => {
          if (!signal.aborted)
            setHistory({
              selection: historySelection,
              version: historyVersion,
              value: unavailable,
              error: isAuthenticationError(cause) ? "Could not refresh share history." : undefined,
            });
        },
      });
    },
  });
  const currentHistory = history?.selection === historySelection ? history : undefined;
  const visibleHistory = currentHistory && {
    byPool: Object.fromEntries(
      poolIds.flatMap((id) => {
        const value = currentHistory.value.byPool[id];
        return value ? [[id, value]] : [];
      }),
    ),
    unavailable: currentHistory.value.unavailable.filter((id) => poolIds.includes(id)),
  };
  return {
    enabled: current?.value?.enabled,
    configurations,
    selected,
    page: currentPage,
    readings: visibleReadings,
    history: visibleHistory,
    historyPending: poolIds.length > 0 && currentHistory?.version !== historyVersion,
    error: [current?.error, currentReadings?.error, currentHistory?.error].filter(Boolean).join(" ") || undefined,
    refresh: () => setRefreshCount((value) => value + 1),
  };
}
