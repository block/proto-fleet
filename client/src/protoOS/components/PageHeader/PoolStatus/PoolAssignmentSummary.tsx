import { useState } from "react";
import type {
  PoolConfigurationStatisticsService,
  PoolConfigurationSummary,
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

  usePoll({
    params: service,
    fetchData: (signal) =>
      authRetry({
        signal,
        request: (options) => service.list(options),
        onSuccess: ({ enabled, configurations }) => {
          if (!signal.aborted) setSummary(enabled ? describeAssignments(configurations) : undefined);
        },
        onError: () => {
          if (!signal.aborted) setSummary("Assignment summary unavailable.");
        },
      }),
  });

  return summary ? (
    <p className="text-300 wrap-anywhere text-text-primary-70" role="status">
      {summary}
    </p>
  ) : null;
}
