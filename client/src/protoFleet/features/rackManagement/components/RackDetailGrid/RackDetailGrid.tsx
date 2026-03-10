import { useMemo } from "react";
import RackDetailSlot from "./RackDetailSlot";
import type { NumberingOrigin, RackDetailGridProps, SlotHealthState } from "./types";

function computeSlotNumber(row: number, col: number, rows: number, cols: number, origin: NumberingOrigin): number {
  switch (origin) {
    case "bottom-left":
      return (rows - 1 - row) * cols + col + 1;
    case "top-left":
      return row * cols + col + 1;
    case "bottom-right":
      return (rows - 1 - row) * cols + (cols - 1 - col) + 1;
    case "top-right":
      return row * cols + (cols - 1 - col) + 1;
  }
}

export default function RackDetailGrid({
  rows,
  cols,
  slotStates = {},
  numberingOrigin = "bottom-left",
  slotsPerMiner = 1,
  slotSize = 64,
}: RackDetailGridProps) {
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
          state: slotStates[`${s.row}-${s.col}`] ?? ("empty" as SlotHealthState),
        })),
        gridCols: displayCols,
      };
    }

    return {
      displaySlots: allSlots.map((s) => ({
        slotNumber: s.slotNumber,
        state: slotStates[`${s.row}-${s.col}`] ?? ("empty" as SlotHealthState),
      })),
      gridCols: cols,
    };
  }, [rows, cols, slotStates, numberingOrigin, spm]);

  return (
    <div
      className="grid"
      style={{
        gridTemplateColumns: `repeat(${gridCols}, minmax(0, ${slotSize}px))`,
        gap: 4,
      }}
    >
      {displaySlots.map((slot, i) => (
        <RackDetailSlot key={i} slot={slot} slotSize={slotSize} />
      ))}
    </div>
  );
}
