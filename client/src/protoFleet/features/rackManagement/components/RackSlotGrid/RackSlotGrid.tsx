import { useMemo } from "react";
import RackSlot from "./RackSlot";
import type { RackSlotGridProps, SlotVisualState } from "./types";
import { computeSlotNumber } from "@/protoFleet/features/rackManagement/utils/slotNumbering";

export default function RackSlotGrid({
  rows,
  cols,
  slotStates = {},
  numberingOrigin = "bottom-left",
  slotsPerMiner = 1,
  slotSize: rawSlotSize = 48,
}: RackSlotGridProps) {
  const slotSize = Math.max(24, Math.min(64, rawSlotSize));
  const spm = slotsPerMiner || 1;

  const { displaySlots, gridCols } = useMemo(() => {
    const allSlots: { row: number; col: number; slotNumber: number }[] = [];
    for (let r = 0; r < rows; r++) {
      for (let c = 0; c < cols; c++) {
        allSlots.push({ row: r, col: c, slotNumber: computeSlotNumber(r, c, rows, cols, numberingOrigin) });
      }
    }

    if (spm > 1) {
      const filtered = allSlots.filter((_, i) => i % spm === 0);
      const totalSlots = Math.floor((cols * rows) / spm);
      const display = filtered.slice(0, totalSlots);
      const displayCols = Math.ceil(display.length / rows) || cols;
      return {
        displaySlots: display.map((s, idx) => ({
          slotNumber: idx + 1,
          state: slotStates[`${s.row}-${s.col}`] ?? ("empty" as SlotVisualState),
        })),
        gridCols: displayCols,
      };
    }

    return {
      displaySlots: allSlots.map((s) => ({
        slotNumber: s.slotNumber,
        state: slotStates[`${s.row}-${s.col}`] ?? ("empty" as SlotVisualState),
      })),
      gridCols: cols,
    };
  }, [rows, cols, slotStates, numberingOrigin, spm]);

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
