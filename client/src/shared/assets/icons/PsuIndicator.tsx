import clsx from "clsx";
import { IconProps } from "./types";

type PsuIndicatorProps = IconProps & {
  totalSlots?: number;
  slotPlacement?: number;
};

const PsuIndicator = ({ className, totalSlots = 3, slotPlacement = 1 }: PsuIndicatorProps) => {
  const activeSlot = slotPlacement < 1 ? 1 : slotPlacement > totalSlots ? totalSlots : slotPlacement;

  return (
    <div
      className={clsx("inline-flex h-4 items-center justify-center gap-[3px]", className)}
      data-testid="psu-indicator"
    >
      {Array.from({ length: totalSlots }).map((_, idx) => {
        const isActive = idx + 1 === activeSlot;
        return (
          <div
            key={`psu-slot-${idx}`}
            className="inline-flex w-[18px] flex-col items-center justify-center gap-0.5 self-stretch rounded p-[3px] outline-1 outline-offset-[-1px] outline-core-primary-10"
            data-testid={isActive ? "psu-slot-active" : "psu-slot"}
          >
            <div
              className={clsx("flex-1 self-stretch rounded-sm", isActive ? "bg-text-primary" : "bg-core-primary-10")}
            />
          </div>
        );
      })}
    </div>
  );
};

export default PsuIndicator;
