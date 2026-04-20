import { useCallback, useMemo, useState } from "react";

import PoolInfoPopover from "./PoolInfoPopover";
import PoolWidget from "./PoolWidget";
import { PoolInfo } from "./types";
import { Pool } from "@/protoOS/api/generatedApi";
import { useResponsivePopover } from "@/shared/components/Popover";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

interface PoolStatusProps {
  loading?: boolean;
  onClickViewPools: () => void;
  poolsInfo?: Pool[];
  shouldShowPopover?: boolean;
}

const PoolStatus = ({ loading = false, onClickViewPools, poolsInfo, shouldShowPopover = false }: PoolStatusProps) => {
  const { triggerRef: WidgetRef } = useResponsivePopover();

  const [showPopover, setShowPopover] = useState(shouldShowPopover);

  const isAlive = useCallback(
    // TODO: remove alive when cgminer is removed
    (pool?: Pool) => /alive|active/i.test(pool?.status ?? ""),
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
        isConnected={isConnected}
        isOpen={isPopoverOpen}
        onTogglePopover={() => setShowPopover((prev) => !prev)}
      />
      {isPopoverOpen && (
        <PoolInfoPopover
          onClickViewPools={handleClickViewPools}
          poolInfo={poolInfo}
          poolsInfo={poolsInfo}
          isConnected={isConnected}
        />
      )}
    </div>
  );
};

export default PoolStatus;
