import { useState } from "react";
import {
  isActivePool,
  type PoolConfigurationReadings,
  type PoolConfigurationStatisticsService,
  type PoolConfigurationSummary,
} from "@/protoOS/api/adapters/poolStatistics";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
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
  const [summary, setSummary] = useState<string | undefined>("Loading assignments…");
  const [readings, setReadings] = useState<PoolConfigurationReadings>();

  usePoll({
    params: service,
    fetchData: (signal) =>
      authRetry({
        signal,
        request: (options) => service.list(options),
        onSuccess: ({ enabled, configurations }) => {
          if (signal.aborted) return;
          setSummary(enabled ? describeAssignments(configurations) : undefined);
          if (!enabled || configurations.length !== 1) return;

          return authRetry({
            signal,
            request: (options) => service.read(configurations, options),
            onSuccess: ([reading]) => {
              if (!signal.aborted && reading && !reading.stale && !reading.error) setReadings(reading);
            },
          });
        },
        onError: () => {
          if (!signal.aborted) setSummary("Assignment summary unavailable.");
        },
      }),
  });

  const activePools = readings?.pools.filter((pool) => pool.url && isActivePool(pool)) ?? [];
  const coverage =
    readings?.observedBoards !== undefined && readings.observedBoards < readings.assignedCount
      ? `${readings.observedBoards} of ${readings.assignedCount} hashboards reporting`
      : undefined;

  return summary ? (
    <div className="space-y-3" role="status">
      <p className="text-300 wrap-anywhere text-text-primary-70">{summary}</p>
      {activePools.length > 0 ? (
        <div className="space-y-1">
          <p className="text-emphasis-200 text-text-primary">
            {activePools.length === 1 ? "Active pool" : "Active pools"}
          </p>
          <ul className="space-y-1">
            {activePools.map((pool) => (
              <li className="text-200 wrap-anywhere text-text-primary-70" key={pool.id}>
                {pool.url}
              </li>
            ))}
          </ul>
          {coverage ? <p className="text-200 text-text-primary-70">{coverage}</p> : null}
        </div>
      ) : null}
    </div>
  ) : null;
}
