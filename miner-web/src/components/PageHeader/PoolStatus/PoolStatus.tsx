import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { Pool } from "apiTypes";

import { useClickOutside } from "common/hooks/useClickOutside";

import PoolInfoPopover from "./PoolInfoPopover";
import PoolWidget from "./PoolWidget";

interface PoolStatusProps {
  loading?: boolean;
  onClickViewPools: () => void;
  poolsInfo?: Pool[];
  shouldShowPopover?: boolean;
}

const PoolStatus = ({
  loading = false,
  onClickViewPools,
  poolsInfo,
  shouldShowPopover = false,
}: PoolStatusProps) => {
  const WidgetRef = useRef<HTMLDivElement>(null);
  const [poolInfo, setPoolInfo] =
    useState<Pick<Pool, "priority" | "status" | "url">>();
  const [showPopover, setShowPopover] = useState(shouldShowPopover);

  const isAlive = useCallback((pool?: Pool) => pool?.status === "Alive", []);

  useEffect(() => {
    if (poolsInfo) {
      const activePool = poolsInfo.find(isAlive) || poolsInfo[0];

      setPoolInfo({
        priority: activePool?.priority,
        status: activePool?.status,
        url: activePool?.url,
      });
    }
  }, [isAlive, poolsInfo]);

  const isConnected = useMemo(() => isAlive(poolInfo), [isAlive, poolInfo]);

  const onClickOutside = useCallback(() => {
    setShowPopover(false);
  }, []);

  useClickOutside({ ref: WidgetRef, onClickOutside });

  const handleClickViewPools = useCallback(() => {
    setShowPopover(false);
    onClickViewPools();
  }, [onClickViewPools]);

  const isPopoverOpen = useMemo(
    () => !loading && showPopover,
    [loading, showPopover]
  );

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
