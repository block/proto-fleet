import { useState } from "react";
import ConnectedPoolsCard from "./ConnectedPoolsCard";
import {
  isActivePool,
  type PoolConfigurationReadings,
  type PoolConfigurationStatisticsService,
  type PoolConfigurationSummary,
} from "@/protoOS/api/adapters/poolStatistics";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { beginPoolConfigurationModeRead } from "@/protoOS/store/hooks/usePoolConfigurationMode";
import { usePoll } from "@/shared/hooks/usePoll";

function describeAssignments(configurations: PoolConfigurationSummary[]) {
  if (configurations.length === 0) return "No hashboards assigned.";

  const count = configurations.reduce((total, configuration) => total + configuration.assignedCount, 0);
  const hashboards = `${count} ${count === 1 ? "hashboard" : "hashboards"}`;

  return configurations.length === 1
    ? `${configurations[0].name} · ${hashboards}`
    : `${hashboards} across ${configurations.length} pool configurations`;
}

export default function PoolAssignmentSummary({ service }: { service: PoolConfigurationStatisticsService }) {
  const authRetry = useAuthRetry();
  const [summary, setSummary] = useState("Loading assignments…");
  const [readings, setReadings] = useState<PoolConfigurationReadings>();

  usePoll({
    params: service,
    fetchData: (signal) => {
      const recordMode = beginPoolConfigurationModeRead(service, signal);
      return authRetry({
        signal,
        request: (options) => service.list(options),
        onSuccess: ({ enabled, configurations }) => {
          if (signal.aborted) return;
          recordMode(enabled);
          if (!enabled) {
            setSummary("Assignment summary unavailable.");
            return;
          }

          setSummary(describeAssignments(configurations));
          if (configurations.length !== 1) return;

          return authRetry({
            signal,
            request: (options) => service.read(configurations, { ...options, includeLastRecordedShares: false }),
            onSuccess: ([reading]) => {
              if (signal.aborted || !reading) return;
              if (reading.stale) {
                setSummary("Configuration changed");
              } else if (!reading.error) {
                setReadings(reading);
              }
            },
          });
        },
        onError: () => {
          if (!signal.aborted) setSummary("Assignment summary unavailable.");
        },
      });
    },
  });

  const activePools =
    readings?.pools.flatMap((pool, index) => (pool.url && isActivePool(pool) ? [{ ...pool, index }] : [])) ?? [];
  const coverage =
    readings?.observedBoards !== undefined && readings.observedBoards < readings.assignedCount
      ? `This controller observes ${readings.observedBoards} of ${readings.assignedCount} assigned hashboards.`
      : undefined;

  return (
    <div className="space-y-3" role="status">
      <p className="text-300 wrap-anywhere text-text-primary-70">{summary}</p>
      {activePools.length > 0 ? <ConnectedPoolsCard pools={activePools} /> : null}
      {coverage ? <p className="text-200 text-text-primary-70">{coverage}</p> : null}
    </div>
  );
}
