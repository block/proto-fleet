import { type ReactNode, useCallback, useMemo, useState } from "react";

import PoolInfoPopover from "./PoolInfoPopover";
import PoolWidget from "./PoolWidget";
import { PoolInfo } from "./types";
import type { PoolInfo as NormalizedPoolInfo } from "@/protoOS/store/contract";
import { useResponsivePopover } from "@/shared/components/Popover";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

type PoolStatusEntry = Pick<NormalizedPoolInfo, "status" | "url"> & Partial<Pick<NormalizedPoolInfo, "priority">>;

interface PoolStatusProps {
  assignmentSummary?: ReactNode;
  loading?: boolean;
  onClickViewPools: () => void;
  poolsInfo?: PoolStatusEntry[];
  showPoolStatusLink?: boolean;
  shouldShowPopover?: boolean;
}

const PoolStatus = ({
  assignmentSummary,
  loading = false,
  onClickViewPools,
  poolsInfo,
  showPoolStatusLink = false,
  shouldShowPopover = false,
}: PoolStatusProps) => {
  const { triggerRef: WidgetRef } = useResponsivePopover();

  const [showPopover, setShowPopover] = useState(shouldShowPopover);

  const isAlive = useCallback(
    // TODO: remove alive when cgminer is removed
    (pool?: Pick<NormalizedPoolInfo, "status">) => /alive|active/i.test(pool?.status ?? ""),
    [],
  );

  // Derive poolInfo directly from poolsInfo
  const poolInfo = useMemo<PoolInfo | undefined>(() => {
    if (!poolsInfo) return undefined;

    const activePool = poolsInfo.find(isAlive) || poolsInfo[0];

    return {
      index: poolsInfo.indexOf(activePool),
      status: activePool?.status,
      url: activePool?.url,
    };
  }, [poolsInfo, isAlive]);

  const isConnected = useMemo(() => isAlive(poolInfo), [isAlive, poolInfo]);

  const onClickOutside = useCallback(() => {
    setShowPopover(false);
  }, []);

  useClickOutside({
    ref: WidgetRef,
    onClickOutside,
    ignoreSelectors: [".popover-content"],
  });

  const handleClickViewPools = useCallback(() => {
    setShowPopover(false);
    onClickViewPools();
  }, [onClickViewPools]);

  const isPopoverOpen = useMemo(() => !loading && showPopover, [loading, showPopover]);

  return (
    <div className="relative" ref={WidgetRef} data-testid="pool-status-widget">
      <PoolWidget
        loading={loading}
        label={showPoolStatusLink ? "Mining pools" : "Mining Pool"}
        isOpen={isPopoverOpen}
        onTogglePopover={() => setShowPopover((prev) => !prev)}
      />
      {isPopoverOpen ? (
        <PoolInfoPopover
          assignmentSummary={assignmentSummary}
          onClickViewPools={handleClickViewPools}
          poolInfo={poolInfo}
          poolsInfo={poolsInfo}
          isConnected={isConnected}
          showPoolStatusLink={showPoolStatusLink}
        />
      ) : null}
    </div>
  );
};

export default PoolStatus;
