import clsx from "clsx";
import type { SlotStatus } from "./types";

interface MiniRackGridProps {
  cols: number;
  rows: number;
  slots: SlotStatus[];
}

const slotColorMap: Record<SlotStatus, string> = {
  empty: "bg-transparent",
  healthy: "bg-core-primary-fill/10",
  needsAttention: "bg-intent-critical-fill/20",
  offline: "bg-core-accent-fill/20",
  sleeping: "bg-core-primary-20/30",
};

function getGridScale(cols: number) {
  if (cols <= 6) return { slotSize: 16, gap: 4, dotSize: 8 };
  if (cols <= 9) return { slotSize: 12, gap: 3, dotSize: 6 };
  return { slotSize: 10, gap: 2, dotSize: 5 };
}

const MiniRackGrid = ({ cols, rows, slots }: MiniRackGridProps) => {
  const { slotSize, gap, dotSize } = getGridScale(cols);
  const totalSlots = cols * rows;

  return (
    <div className="flex justify-center">
      <div
        data-testid="rack-card-grid"
        className="grid"
        style={{
          gridTemplateColumns: `repeat(${cols}, ${slotSize}px)`,
          gap: `${gap}px`,
        }}
      >
        {Array.from({ length: totalSlots }, (_, i) => {
          const status = slots[i] ?? "empty";
          const hasDot = status === "needsAttention" || status === "offline";

          return (
            <div
              key={i}
              data-testid="rack-card-slot"
              className={clsx("relative rounded-[3px]", slotColorMap[status], {
                "border border-core-primary-10": status === "empty",
              })}
              style={{ width: slotSize, height: slotSize }}
            >
              {hasDot && (
                <span
                  className={clsx("absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 rounded-full", {
                    "bg-intent-critical-fill": status === "needsAttention",
                    "bg-core-accent-fill": status === "offline",
                  })}
                  style={{ width: dotSize, height: dotSize }}
                />
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default MiniRackGrid;
