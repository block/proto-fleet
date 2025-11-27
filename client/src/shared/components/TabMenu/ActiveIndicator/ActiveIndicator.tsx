import { memo } from "react";
import clsx from "clsx";

type ActiveIndicatorProps = {
  activeIndex?: number;
  activeIndicatorTransX?: string;
  activeIndicatorTransY?: string;
  shouldAnimate: boolean;
};

const ActiveIndicator = memo(
  ({ activeIndex, activeIndicatorTransX, activeIndicatorTransY, shouldAnimate }: ActiveIndicatorProps) => {
    return (
      <div
        data-testid="active-indicator"
        className={clsx(
          "absolute -left-4 h-full w-[calc(25%+(-3*theme(spacing.10))/4+theme(spacing.8))] rounded-2xl bg-surface-base shadow-100",
          "phone:top-2 phone:left-2 phone:h-[calc(50%-theme(spacing.3))] phone:w-[calc(50%-theme(spacing.3))]",
          shouldAnimate && "transition-transform duration-500 ease-in-out",
        )}
        style={{
          opacity: activeIndex !== undefined ? 1 : 0,
          transform: `translate3d(${activeIndicatorTransX || "0"}, ${activeIndicatorTransY || "0"}, 0)`,
        }}
      />
    );
  },
);

ActiveIndicator.displayName = "ActiveIndicator";

export default ActiveIndicator;
