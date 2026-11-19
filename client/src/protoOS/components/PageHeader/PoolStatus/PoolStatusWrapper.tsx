import { useState } from "react";

import { navigationItems } from "../../NavigationMenu/constants";
import PoolAssignmentSummary from "./PoolAssignmentSummary";
import PoolStatus from "./PoolStatus";
import type { PoolConfigurationStatisticsService } from "@/protoOS/api/adapters/poolStatistics";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { usePoolsInfo } from "@/protoOS/store";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { PopoverProvider } from "@/shared/components/Popover";
import { useNavigate } from "@/shared/hooks/useNavigate";
import { usePoll } from "@/shared/hooks/usePoll";

const PoolStatusWrapper = () => {
  const navigate = useNavigate();
  const poolsInfo = usePoolsInfo();
  const { adapter, isFleetHosted, minerRoot } = useMinerHosting();
  const authRetry = useAuthRetry();
  const statistics = isFleetHosted ? undefined : adapter?.poolStatistics;
  const [mode, setMode] = useState<{ service: PoolConfigurationStatisticsService; enabled: boolean }>();

  usePoll({
    params: statistics,
    enabled: statistics !== undefined,
    poll: true,
    pollIntervalMs: 15_000,
    fetchData: (signal) => {
      if (!statistics) return;
      return authRetry({
        signal,
        request: (options) => statistics.getMiningGroupsEnabled(options),
        onSuccess: (enabled) => {
          if (!signal.aborted) setMode({ service: statistics, enabled });
        },
        onError: () => {
          if (!signal.aborted) setMode(undefined);
        },
      });
    },
  });

  const miningGroupsEnabled = mode?.service === statistics ? mode?.enabled : undefined;
  const showPoolStatusLink = statistics !== undefined && miningGroupsEnabled !== false;
  const assignmentSummary = statistics && miningGroupsEnabled ? <PoolAssignmentSummary service={statistics} /> : null;
  const loading = !showPoolStatusLink && poolsInfo === undefined;
  const viewPoolsPath = showPoolStatusLink ? navigationItems.pools : navigationItems.miningPools;

  return (
    <PopoverProvider>
      <PoolStatus
        assignmentSummary={assignmentSummary}
        poolsInfo={poolsInfo}
        loading={loading}
        showPoolStatusLink={showPoolStatusLink}
        onClickViewPools={() => navigate(`${minerRoot}/${viewPoolsPath}`)}
      />
    </PopoverProvider>
  );
};

export default PoolStatusWrapper;
