import clsx from "clsx";
import type { RackSlotProps } from "./types";

const stateClasses: Record<string, string> = {
  empty: "border border-border-10 bg-transparent",
  occupied: "border-2 border-core-primary-fill bg-surface-base",
  selected: "border-2 border-core-accent-fill bg-surface-base",
  selectedOccupied: "border-2 border-core-accent-fill bg-surface-base",
  dragOver: "border-2 border-core-accent-fill bg-core-accent-10",
  peerHover: "border border-border-10 bg-core-primary-5",
};

export default function RackSlot({ slot, slotSize = 48 }: RackSlotProps) {
  return (
    <div
      className={clsx(
        "flex items-center justify-center rounded-lg text-[14px] font-medium text-text-primary-70 tabular-nums",
        stateClasses[slot.state],
      )}
      style={{ width: slotSize, height: slotSize }}
    >
      {String(slot.slotNumber).padStart(2, "0")}
    </div>
  );
}
