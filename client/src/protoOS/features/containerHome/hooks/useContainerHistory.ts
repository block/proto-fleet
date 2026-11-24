import { useCallback, useMemo, useState } from "react";
import type { ContainerHistoryResult, ContainerHistorySelection } from "@/protoOS/api/adapters/containerHistory";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import { type Duration, getDurationMs } from "@/shared/components/DurationSelector";
import { usePoll } from "@/shared/hooks/usePoll";

interface Snapshot {
  key: string;
  adapter: ReturnType<typeof useMinerHosting>["adapter"];
  result?: ContainerHistoryResult;
  window: [number, number];
  pending: boolean;
  error?: string;
}

/** Query-scoped history stays separate from live telemetry, so a duration change
 * cannot replace current readings or display the previous period's aggregates. */
export function useContainerHistory(duration: Duration, selections: ContainerHistorySelection[]) {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const queryKey = JSON.stringify({ duration, selections });
  const params = useMemo(() => ({ adapter, queryKey }), [adapter, queryKey]);
  const [snapshot, setSnapshot] = useState<Snapshot>();
  const fetchData = useCallback(
    async (signal: AbortSignal) => {
      if (!adapter?.getContainerHistory) return;
      const query = JSON.parse(queryKey) as { duration: Duration; selections: ContainerHistorySelection[] };
      const intervalMs = { "1h": 60_000, "12h": 300_000, "24h": 900_000, "48h": 900_000, "5d": 3_600_000 }[
        query.duration
      ];
      const end = Math.floor(Date.now() / intervalMs) * intervalMs;
      const window: [number, number] = [end - getDurationMs(query.duration), end];
      const accept = beginStoreWrite(`container-home:history:${queryKey}`);
      const currentSnapshot = (previous: Snapshot | undefined) =>
        previous?.key === queryKey && previous.adapter === adapter ? previous : undefined;
      // Keep the displayed period and stale warning until fresh data arrives.
      setSnapshot((previous) => ({
        ...currentSnapshot(previous),
        key: queryKey,
        adapter,
        window: currentSnapshot(previous)?.window ?? window,
        pending: true,
      }));
      const onRefreshFailure = () => {
        if (!signal.aborted && accept())
          setSnapshot((previous) => ({
            ...currentSnapshot(previous),
            key: queryKey,
            adapter,
            window: currentSnapshot(previous)?.window ?? window,
            pending: false,
            error: "Could not refresh history. Displayed history may be out of date.",
          }));
      };
      await authRetry({
        signal,
        request: (options) =>
          adapter.getContainerHistory!(
            {
              startTime: new Date(window[0]).toISOString(),
              endTime: new Date(window[1]).toISOString(),
              intervalMs,
              selections: query.selections,
            },
            options,
          ),
        onSuccess: (result) => {
          // The adapter resolves per-metric failures as unavailable entries.
          // An empty successful series is valid; no successful series is a failed refresh.
          if (result.unavailable.length > 0 && !Object.values(result.series).some(Boolean)) {
            onRefreshFailure();
            return;
          }
          if (!signal.aborted && accept()) setSnapshot({ key: queryKey, adapter, result, window, pending: false });
        },
        onError: onRefreshFailure,
      });
    },
    [adapter, authRetry, queryKey],
  );
  const enabled = Boolean(adapter?.getContainerHistory) && selections.length > 0;
  usePoll({ fetchData, params, enabled, poll: true, pollIntervalMs: 30_000 });
  const current = enabled && snapshot?.key === queryKey && snapshot.adapter === adapter ? snapshot : undefined;
  return {
    result: current?.result,
    window: current?.window,
    pending: enabled && (current?.pending ?? true),
    error: current?.error,
  };
}
