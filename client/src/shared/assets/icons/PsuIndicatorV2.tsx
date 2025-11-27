import clsx from "clsx";
import { IconProps } from "./types";

type PsuIndicatorProps = IconProps & {
  totalSlots?: number;
  position?: number;
};

const PsuIndicatorV2 = ({ className, totalSlots = 3, position = 1 }: PsuIndicatorProps) => {
  const activeSlot = position < 1 ? 1 : position > totalSlots ? totalSlots : position;

  return (
    <div className={clsx("flex h-6 items-start justify-center", className)} data-testid="psu-indicator">
      {Array.from({ length: totalSlots }).map((_, idx) => {
        const isActive = idx + 1 === activeSlot;
        return (
          <div
            key={`psu-slot-${idx}`}
            className={clsx("box-border flex flex-col items-center justify-end self-stretch px-0.5", {
              "rounded border-2 border-border-10 py-0.5": isActive,
              "py-1": !isActive,
            })}
          >
            <div className={clsx("h-1 w-2.5 rounded-xs", isActive ? "bg-core-primary-fill" : "bg-core-primary-10")} />
          </div>
        );
      })}
    </div>
  );
};

export default PsuIndicatorV2;
