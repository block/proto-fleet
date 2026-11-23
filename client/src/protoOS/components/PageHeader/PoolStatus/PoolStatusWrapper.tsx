import { useEffect, useRef } from "react";
import { useMatch } from "react-router-dom";

import { navigationItems } from "../../NavigationMenu/constants";
import PoolAssignmentSummary from "./PoolAssignmentSummary";
import PoolStatus from "./PoolStatus";
import { isAuthenticationError } from "@/protoOS/api/adapters/errors";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { usePoolsInfo } from "@/protoOS/store";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import {
  beginPoolConfigurationModeRead,
  usePoolConfigurationMode,
} from "@/protoOS/store/hooks/usePoolConfigurationMode";
import { PopoverProvider } from "@/shared/components/Popover";
import { useNavigate } from "@/shared/hooks/useNavigate";
import { usePoll } from "@/shared/hooks/usePoll";

const PoolStatusWrapper = () => {
  const navigate = useNavigate();
  const poolsInfo = usePoolsInfo();
  const { adapter, isFleetHosted, minerRoot } = useMinerHosting();
  const authRetry = useAuthRetry();
  const statistics = isFleetHosted ? undefined : adapter?.poolStatistics;
  const mode = usePoolConfigurationMode(statistics);
  const poolsPage = useMatch(`${minerRoot}/${navigationItems.pools}`);
  const checkingMode = statistics !== undefined && mode === undefined;
  const recheck = useRef<AbortController | null>(null);
  useEffect(() => () => recheck.current?.abort(), [statistics]);

  const checkMode = async (signal: AbortSignal) => {
    if (!statistics) return;
    const recordMode = beginPoolConfigurationModeRead(statistics, signal);
    let failed = false;
    await authRetry({
      signal,
      request: (options) => statistics.getMiningGroupsEnabled(options),
      onSuccess: recordMode,
      onError: (error) => {
        recordMode("unavailable");
        failed = !isAuthenticationError(error);
      },
    });
    return failed;
  };

  // On the Pools page the assignment list supplies the initial mode.
  usePoll({
    params: statistics,
    enabled: statistics !== undefined && !poolsPage,
    fetchData: async (signal) => {
      if (!checkingMode) return;
      // Keep the fallback usable while retrying an initial failure once.
      if ((await checkMode(signal)) && !signal.aborted) await checkMode(signal);
    },
  });

  const showPoolStatusLink = mode === true || mode === "unavailable";
  const loading = checkingMode || (!showPoolStatusLink && poolsInfo === undefined);
  const viewPoolsPath = showPoolStatusLink ? navigationItems.pools : navigationItems.miningPools;
  const details = statistics && mode === true ? <PoolAssignmentSummary service={statistics} /> : null;

  return (
    <PopoverProvider>
      <PoolStatus
        details={details}
        poolsInfo={poolsInfo}
        loading={loading}
        showPoolStatusLink={showPoolStatusLink}
        onClickViewPools={() => navigate(`${minerRoot}/${viewPoolsPath}`)}
        onOpenPopover={() => {
          // When groups are enabled, the assignment list already checks the mode.
          if (!statistics || mode === true) return;
          recheck.current?.abort();
          const controller = new AbortController();
          recheck.current = controller;
          void checkMode(controller.signal);
        }}
      />
    </PopoverProvider>
  );
};

export default PoolStatusWrapper;
