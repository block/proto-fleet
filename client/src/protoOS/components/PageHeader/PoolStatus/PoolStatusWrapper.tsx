import { useMemo, useRef, useState } from "react";

import { navigationItems } from "../../NavigationMenu/constants";
import PoolAssignmentSummary from "./PoolAssignmentSummary";
import PoolStatus from "./PoolStatus";
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
  const [modeRefresh, setModeRefresh] = useState(0);
  const modeKey = useMemo(() => ({ statistics, modeRefresh }), [statistics, modeRefresh]);
  const [mode, setMode] = useState<{ key: typeof modeKey; enabled: boolean }>();
  const modeRevision = useRef(0);

  usePoll({
    params: modeKey,
    enabled: statistics !== undefined,
    poll: true,
    pollIntervalMs: 15_000,
    fetchData: (signal) => {
      if (!statistics) return;
      const requestRevision = modeRevision.current;
      const isCurrent = () => !signal.aborted && requestRevision === modeRevision.current;
      return authRetry({
        signal,
        request: (options) => statistics.getMiningGroupsEnabled(options),
        onSuccess: (enabled) => {
          if (isCurrent()) setMode({ key: modeKey, enabled });
        },
        onError: () => {
          if (isCurrent()) setMode(undefined);
        },
      });
    },
  });

  const handleModeDisabled = () => {
    // Ignore any capability poll already in flight when the assignment list confirms disabled mode.
    modeRevision.current += 1;
    setMode({ key: modeKey, enabled: false });
  };

  const miningGroupsEnabled = mode?.key === modeKey ? mode.enabled : undefined;
  const checkingMode = mode?.enabled === false && mode.key.statistics === statistics && mode.key !== modeKey;
  const showPoolStatusLink = statistics !== undefined && miningGroupsEnabled !== false;
  const details = checkingMode ? (
    <p className="text-300 text-text-primary-70" role="status">
      Checking mining group status…
    </p>
  ) : statistics && miningGroupsEnabled ? (
    <PoolAssignmentSummary service={statistics} onModeDisabled={handleModeDisabled} />
  ) : null;
  const loading = !showPoolStatusLink && poolsInfo === undefined;
  const viewPoolsPath = showPoolStatusLink ? navigationItems.pools : navigationItems.miningPools;

  return (
    <PopoverProvider>
      <PoolStatus
        details={details}
        poolsInfo={poolsInfo}
        loading={loading}
        showPoolStatusLink={showPoolStatusLink}
        onClickViewPools={() => navigate(`${minerRoot}/${viewPoolsPath}`)}
        onOpenPopover={() => {
          // The assignment list already checks mode when opening the group popup.
          if (miningGroupsEnabled === false) setModeRefresh((refresh) => refresh + 1);
        }}
      />
    </PopoverProvider>
  );
};

export default PoolStatusWrapper;
