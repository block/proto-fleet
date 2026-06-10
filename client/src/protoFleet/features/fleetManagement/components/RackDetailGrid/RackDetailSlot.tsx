import clsx from "clsx";
import type { RackDetailSlotProps } from "./types";

const stateClasses: Record<string, string> = {
  healthy: "border border-core-primary-fill/18 bg-transparent",
  needsAttention: "border border-core-primary-fill/10 bg-intent-critical-fill/14",
  offline: "border border-core-primary-fill/10 bg-core-accent-fill/14",
  sleeping: "border border-core-primary-fill/8 bg-transparent",
  empty: "bg-transparent",
};

const dotColors: Record<string, string | null> = {
  healthy: null,
  needsAttention: "#ef4444",
  offline: "#f97316",
  sleeping: "#d4d4d8",
  empty: null,
};

export default function RackDetailSlot({ slot, slotSize = 64, onEmptySlotClick }: RackDetailSlotProps) {
  const { row, col } = slot;
  const { state, slotNumber } = slot;
  const num = String(slotNumber).padStart(2, "0");
  const dotColor = dotColors[state];
  const slotTestId = `rack-detail-slot-${num}`;

  if (state === "empty") {
    return (
      <div
        data-testid={slotTestId}
        data-slot-state="empty"
        className="flex items-center justify-center rounded-lg"
        style={{ width: slotSize, height: slotSize }}
      >
        <button
          data-testid="rack-detail-slot-empty-action"
          type="button"
          aria-label={`Assign miner to slot ${num}`}
          onClick={() => onEmptySlotClick?.(row, col)}
          className="flex cursor-pointer items-center justify-center rounded-full bg-core-primary-fill/6 text-core-primary-fill/25 transition-colors hover:bg-core-primary-fill/10 hover:text-core-primary-fill/50"
          style={{
            width: slotSize * 0.7,
            height: slotSize * 0.7,
            maxWidth: 36,
            maxHeight: 36,
            fontSize: `clamp(12px, 3cqi, 16px)`,
          }}
        >
          +
        </button>
      </div>
    );
  }

  const compact = slotSize < 44;
  const iconOnly = slotSize < 40;

  return (
    <div
      data-testid={slotTestId}
      data-slot-state={state}
      className={clsx(
        "flex items-center justify-center rounded-lg font-medium tabular-nums",
        compact ? "flex-col" : "flex-row",
        stateClasses[state],
      )}
      style={{ width: slotSize, height: slotSize, gap: iconOnly ? 0 : 4 }}
    >
      {dotColor ? (
        <span
          className="inline-block shrink-0 rounded-full"
          style={{
            width: `clamp(6px, 1.8cqi, 9px)`,
            height: `clamp(6px, 1.8cqi, 9px)`,
            background: dotColor,
          }}
        />
      ) : null}
      {!iconOnly ? (
        <span
          data-testid="rack-detail-slot-number"
          className={clsx("leading-none", state === "sleeping" ? "text-text-primary-30" : "text-text-primary-70")}
          style={{ fontSize: `clamp(10px, 2.5cqi, 14px)` }}
        >
          {num}
        </span>
      ) : null}
    </div>
  );
}
