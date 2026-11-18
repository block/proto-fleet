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
  const [revision, setRevision] = useState(0);
  const catalogueParams = useMemo(() => ({ key, revision }), [key, revision]);
  usePoll({
    params: catalogueParams,
    poll: true,
    pollIntervalMs: 30_000,
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
  let entries = configurations.slice(currentPage * 25, (currentPage + 1) * 25);
  if (selectedId) entries = selected ? [selected] : [];
  const entriesKey = JSON.stringify(entries.map(({ id, revision, assignedCount }) => [id, revision, assignedCount]));
  const selection = useMemo(() => ({ key, duration, entriesKey }), [key, duration, entriesKey]);
  const [readings, setReadings] = useState<{
    selection: typeof selection;
    configurations?: PoolConfigurationReadings[];
    history?: PoolHistoryResult;
    error?: string;
  }>();
  const readingParams = useMemo(() => ({ selection, revision }), [selection, revision]);
  usePoll({
    params: readingParams,
    enabled: current?.value?.enabled === true && entries.length > 0,
    poll: true,
    pollIntervalMs: 30_000,
    fetchData: (signal) =>
      authRetry({
        signal,
        request: async (options) => {
          const configurations = await service.read(entries, options);
          signal.throwIfAborted();
          const poolIds = configurations.flatMap((configuration) => configuration.pools.map((pool) => pool.id));
          const intervalMs = 3_600_000;
          const end = Math.floor(Date.now() / intervalMs) * intervalMs;
          let history: PoolHistoryResult = { byPool: {}, unavailable: poolIds };
          if (adapter?.getPoolHistory)
            try {
              history = await adapter.getPoolHistory(
                {
                  poolIds,
                  startTime: new Date(end - durationToMs[duration]).toISOString(),
                  endTime: new Date(end).toISOString(),
                  intervalMs,
                },
                options,
              );
            } catch (cause) {
              if (signal.aborted || isAuthenticationError(cause)) throw cause;
            }
          return { configurations, history };
        },
        onSuccess: (value) => {
          if (!signal.aborted) setReadings({ selection, ...value });
        },
        onError: () => {
          if (!signal.aborted)
            setReadings((previous) => ({
              selection,
              ...(previous?.selection === selection ? previous : {}),
              error: "Could not refresh configuration statistics. Displayed values may be out of date.",
            }));
        },
      }),
  });
  const currentReadings = readings?.selection === selection ? readings : undefined;
  return {
    enabled: current?.value?.enabled,
    configurations,
    selected,
    page: currentPage,
    readings: currentReadings?.configurations,
    history: currentReadings?.history,
    error: [current?.error, currentReadings?.error].filter(Boolean).join(" ") || undefined,
    refresh: () => setRevision((value) => value + 1),
  };
}
