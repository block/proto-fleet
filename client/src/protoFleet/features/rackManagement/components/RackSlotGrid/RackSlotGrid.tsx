import { useMemo } from "react";
import RackSlot from "./RackSlot";
import type { RackSlotGridProps, SlotVisualState } from "./types";
import { computeSlotNumber } from "@/protoFleet/features/rackManagement/utils/slotNumbering";

export default function RackSlotGrid({
  rows,
  cols,
  slotStates = {},
  numberingOrigin = "bottom-left",
  slotSize: rawSlotSize = 48,
}: RackSlotGridProps) {
  const slotSize = Math.max(24, Math.min(64, rawSlotSize));

  const { displaySlots, gridCols } = useMemo(() => {
    const allSlots: { row: number; col: number; slotNumber: number }[] = [];
    for (let r = 0; r < rows; r++) {
      for (let c = 0; c < cols; c++) {
        allSlots.push({ row: r, col: c, slotNumber: computeSlotNumber(r, c, rows, cols, numberingOrigin) });
      }
    }

    return {
      displaySlots: allSlots.map((s) => ({
        slotNumber: s.slotNumber,
        state: slotStates[`${s.row}-${s.col}`] ?? ("empty" as SlotVisualState),
      })),
      gridCols: cols,
    };
  }, [rows, cols, slotStates, numberingOrigin]);

  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: `repeat(${gridCols}, ${slotSize}px)`,
        gap: 8,
      }}
    >
      {displaySlots.map((slot, i) => (
        <RackSlot key={i} slot={slot} slotSize={slotSize} />
      ))}
    </div>
  );
}
