import { useMemo } from "react";
import RackDetailSlot from "./RackDetailSlot";
import type { RackDetailGridProps, SlotHealthState } from "./types";
import { computeSlotNumber } from "@/protoFleet/features/rackManagement/utils/slotNumbering";
import useMeasure from "@/shared/hooks/useMeasure";

const GAP = 4;
const MIN_SLOT_SIZE = 24;

export default function RackDetailGrid({
  rows,
  cols,
  slotStates = {},
  numberingOrigin = "bottom-left",
  slotSize = 64,
  onEmptySlotClick,
}: RackDetailGridProps) {
  const [measureRef, { width: containerWidth }] = useMeasure<HTMLDivElement>();

  const { displaySlots, gridCols } = useMemo(() => {
    const allSlots: { row: number; col: number; slotNumber: number }[] = [];
    for (let r = 0; r < rows; r++) {
      for (let c = 0; c < cols; c++) {
        allSlots.push({ row: r, col: c, slotNumber: computeSlotNumber(r, c, rows, cols, numberingOrigin) });
      }
    }

    return {
      displaySlots: allSlots.map((s) => ({
        row: s.row,
        col: s.col,
        slotNumber: s.slotNumber,
        state: slotStates[`${s.row}-${s.col}`] ?? ("empty" as SlotHealthState),
      })),
      gridCols: cols,
    };
  }, [rows, cols, slotStates, numberingOrigin]);

  const computedSlotSize = useMemo(() => {
    if (!containerWidth) return 0;
    const maxFit = Math.floor((containerWidth - (gridCols - 1) * GAP) / gridCols);
    return Math.max(MIN_SLOT_SIZE, Math.min(slotSize, maxFit));
  }, [containerWidth, gridCols, slotSize]);

  return (
    <div ref={measureRef} className="flex w-full justify-center">
      {computedSlotSize > 0 && (
        <div
          data-testid="rack-detail-grid"
          className="grid"
          style={{
            gridTemplateColumns: `repeat(${gridCols}, ${computedSlotSize}px)`,
            gridAutoRows: `${computedSlotSize}px`,
            gap: GAP,
          }}
        >
          {displaySlots.map((slot, i) => (
            <RackDetailSlot key={i} slot={slot} slotSize={computedSlotSize} onEmptySlotClick={onEmptySlotClick} />
          ))}
        </div>
      )}
    </div>
  );
}
