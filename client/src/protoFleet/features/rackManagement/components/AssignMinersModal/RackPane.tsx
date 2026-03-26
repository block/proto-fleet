import { useMemo } from "react";
import clsx from "clsx";

import type { AssignmentMode } from "./types";
import { computeSlotNumber, type NumberingOrigin } from "@/protoFleet/features/rackManagement/utils/slotNumbering";

interface RackPaneProps {
  rows: number;
  cols: number;
  numberingOrigin: NumberingOrigin;
  slotAssignments: Record<string, string>;
  selectedMinerId: string | null;
  assignmentMode: AssignmentMode;
  assignedCount: number;
  totalSlots: number;
  originLabel: string;
  onSlotClick: (row: number, col: number) => void;
  onAssignedSlotClick: (deviceIdentifier: string) => void;
}

interface SlotInfo {
  row: number;
  col: number;
  slotNumber: number;
  key: string;
}

function RackSlotCell({
  slot,
  assignedMinerId,
  isManualMode,
  hasSelectedMiner,
  slotSize,
  padWidth,
  onSlotClick,
  onAssignedSlotClick,
}: {
  slot: SlotInfo;
  assignedMinerId: string | undefined;
  isManualMode: boolean;
  hasSelectedMiner: boolean;
  slotSize: number;
  padWidth: number;
  onSlotClick: (row: number, col: number) => void;
  onAssignedSlotClick: (deviceIdentifier: string) => void;
}) {
  const isAssigned = !!assignedMinerId;
  const isClickable = isManualMode && (isAssigned || hasSelectedMiner);

  return (
    <button
      type="button"
      className={clsx(
        "flex items-center justify-center rounded-lg tabular-nums transition-colors",
        isAssigned ? "border-2 border-core-primary-fill bg-surface-base" : "border border-border-10 bg-transparent",
        isClickable &&
          !isAssigned &&
          hasSelectedMiner &&
          "cursor-pointer hover:border-core-primary-fill hover:bg-core-primary-5",
        isClickable && isAssigned && "cursor-pointer",
        !isClickable && "cursor-default",
      )}
      style={{ width: slotSize, height: slotSize, fontSize: Math.max(9, Math.min(12, slotSize * 0.3)) }}
      onClick={() => {
        if (!isManualMode) return;
        if (isAssigned) {
          onAssignedSlotClick(assignedMinerId);
        } else {
          onSlotClick(slot.row, slot.col);
        }
      }}
      disabled={!isClickable}
    >
      <span className="font-medium text-text-primary-70">{String(slot.slotNumber).padStart(padWidth, "0")}</span>
    </button>
  );
}

export default function RackPane({
  rows,
  cols,
  numberingOrigin,
  slotAssignments,
  selectedMinerId,
  assignmentMode,
  assignedCount,
  totalSlots,
  originLabel,
  onSlotClick,
  onAssignedSlotClick,
}: RackPaneProps) {
  const slots = useMemo(() => {
    const result: SlotInfo[] = [];
    for (let r = 0; r < rows; r++) {
      for (let c = 0; c < cols; c++) {
        const key = `${r}-${c}`;
        result.push({
          row: r,
          col: c,
          slotNumber: computeSlotNumber(r, c, rows, cols, numberingOrigin),
          key,
        });
      }
    }
    return result;
  }, [rows, cols, numberingOrigin]);

  const padWidth = totalSlots >= 100 ? 3 : 2;

  // Compute slot size based on column count — allow shrinking to fit all columns
  const slotSize = Math.max(28, Math.min(72, Math.floor(480 / cols)));

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center justify-between pb-4">
        <span className="text-300 text-text-primary-50">
          {cols}x{rows}, {originLabel}
        </span>
        <span className="text-300 text-text-primary-50">
          {assignedCount}/{totalSlots} assigned
        </span>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="flex min-h-full w-full items-center overflow-x-auto">
          <div
            className="mx-auto my-auto w-fit"
            style={{
              display: "grid",
              gridTemplateColumns: `repeat(${cols}, ${slotSize}px)`,
              gap: slotSize <= 36 ? 4 : 8,
            }}
          >
            {slots.map((slot) => (
              <RackSlotCell
                key={slot.key}
                slot={slot}
                assignedMinerId={slotAssignments[slot.key]}
                isManualMode={assignmentMode === "manual"}
                hasSelectedMiner={!!selectedMinerId}
                slotSize={slotSize}
                padWidth={padWidth}
                onSlotClick={onSlotClick}
                onAssignedSlotClick={onAssignedSlotClick}
              />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
