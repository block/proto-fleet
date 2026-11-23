import { type ReactNode, useCallback, useState } from "react";

import PoolInfoPopover from "./PoolInfoPopover";
import PoolWidget from "./PoolWidget";
import type { PoolStatusEntry } from "./types";
import { useResponsivePopover } from "@/shared/components/Popover";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

interface PoolStatusProps {
  details?: ReactNode;
  loading?: boolean;
  onClickViewPools: () => void;
  onOpenPopover?: () => void;
  poolsInfo?: PoolStatusEntry[];
  showPoolStatusLink?: boolean;
  shouldShowPopover?: boolean;
}

const PoolStatus = ({
  details,
  loading = false,
  onClickViewPools,
  onOpenPopover,
  poolsInfo,
  showPoolStatusLink = false,
  shouldShowPopover = false,
}: PoolStatusProps) => {
  const { triggerRef } = useResponsivePopover();

  const [showPopover, setShowPopover] = useState(shouldShowPopover);

  const onClickOutside = useCallback(() => {
    setShowPopover(false);
  }, []);

  useClickOutside({
    ref: triggerRef,
    onClickOutside,
    ignoreSelectors: [".popover-content"],
  });

  const handleClickViewPools = useCallback(() => {
    setShowPopover(false);
    onClickViewPools();
  }, [onClickViewPools]);

  const isPopoverOpen = !loading && showPopover;

  return (
    <div className="relative" ref={triggerRef} data-testid="pool-status-widget">
      <PoolWidget
        loading={loading}
        label={showPoolStatusLink ? "Mining pools" : "Mining Pool"}
        onTogglePopover={() => {
          if (!showPopover) onOpenPopover?.();
          setShowPopover((prev) => !prev);
        }}
      />
      {isPopoverOpen ? (
        <PoolInfoPopover
          details={details}
          onClickViewPools={handleClickViewPools}
          poolsInfo={poolsInfo}
          showPoolStatusLink={showPoolStatusLink}
        />
      ) : null}
    </div>
  );
};

export default PoolStatus;
