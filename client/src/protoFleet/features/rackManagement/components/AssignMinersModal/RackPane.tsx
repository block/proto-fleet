import { useMemo } from "react";
import clsx from "clsx";

import type { AssignmentMode } from "./types";
import { computeSlotNumber, type NumberingOrigin } from "@/protoFleet/features/rackManagement/utils/slotNumbering";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";

interface RackPaneProps {
  rows: number;
  cols: number;
  numberingOrigin: NumberingOrigin;
  slotAssignments: Record<string, string>;
  assignmentMode: AssignmentMode;
  assignedCount: number;
  totalSlots: number;
  originLabel: string;
  selectedSlotKey: string | null;
  showPopover: boolean;
  hasMiners: boolean;
  onCellClick: (row: number, col: number) => void;
  onSelectFromList: () => void;
  onSearchMiners: () => void;
  onPopoverDismiss: () => void;
  onHoverMiner: (minerId: string | null) => void;
}

interface SlotInfo {
  row: number;
  col: number;
  slotNumber: number;
  key: string;
}

function SlotPopover({
  selectFromListDisabled,
  onSelectFromList,
  onSearchMiners,
  onDismiss,
}: {
  selectFromListDisabled: boolean;
  onSelectFromList: () => void;
  onSearchMiners: () => void;
  onDismiss: () => void;
}) {
  useEscapeDismiss(onDismiss);

  return (
    <>
      <div
        className="fixed inset-0 z-20"
        role="presentation"
        onClick={(e) => {
          e.stopPropagation();
          onDismiss();
        }}
      />
      <div
        className="absolute top-full left-1/2 z-30 mt-1 w-44 -translate-x-1/2 rounded-xl border border-border-5 bg-surface-elevated-base py-1 shadow-300"
        role="menu"
      >
        <button
          type="button"
          role="menuitem"
          className={clsx(
            "w-full px-4 py-2 text-left text-300",
            selectFromListDisabled ? "cursor-not-allowed text-text-primary-30" : "text-text-primary hover:bg-surface-5",
          )}
          disabled={selectFromListDisabled}
          onClick={(e) => {
            e.stopPropagation();
            onSelectFromList();
          }}
        >
          Select from list
        </button>
        <button
          type="button"
          role="menuitem"
          className="w-full px-4 py-2 text-left text-300 text-text-primary hover:bg-surface-5"
          onClick={(e) => {
            e.stopPropagation();
            onSearchMiners();
          }}
        >
          Search miners
        </button>
      </div>
    </>
  );
}

function RackSlotCell({
  slot,
  assignedMinerId,
  isManualMode,
  isSelected,
  showPopover,
  hasMiners,
  slotSize,
  padWidth,
  onCellClick,
  onSelectFromList,
  onSearchMiners,
  onPopoverDismiss,
  onHoverMiner,
}: {
  slot: SlotInfo;
  assignedMinerId: string | undefined;
  isManualMode: boolean;
  isSelected: boolean;
  showPopover: boolean;
  hasMiners: boolean;
  slotSize: number;
  padWidth: number;
  onCellClick: (row: number, col: number) => void;
  onSelectFromList: () => void;
  onSearchMiners: () => void;
  onPopoverDismiss: () => void;
  onHoverMiner: (minerId: string | null) => void;
}) {
  const isAssigned = !!assignedMinerId;
  const isClickable = isManualMode;
  const slotState = isSelected ? "selected" : isAssigned ? "assigned" : "empty";

  return (
    <div className="relative">
      <button
        type="button"
        data-testid={`rack-slot-${String(slot.slotNumber).padStart(padWidth, "0")}`}
        data-slot-state={slotState}
        className={clsx(
          "flex items-center justify-center rounded-lg tabular-nums transition-colors",
          isSelected
            ? "border-2 border-intent-warning-fill bg-surface-base"
            : isAssigned
              ? "border-2 border-core-primary-fill bg-surface-base"
              : "border border-border-10 bg-transparent",
          isClickable && !isSelected && "cursor-pointer hover:border-core-primary-fill hover:bg-core-primary-5",
          isClickable && isSelected && "cursor-pointer",
          !isClickable && "cursor-default",
        )}
        style={{ width: slotSize, height: slotSize, fontSize: Math.max(9, Math.min(12, slotSize * 0.3)) }}
        onClick={() => {
          if (!isManualMode) return;
          onCellClick(slot.row, slot.col);
        }}
        onMouseEnter={() => isAssigned && onHoverMiner(assignedMinerId)}
        onMouseLeave={() => isAssigned && onHoverMiner(null)}
        disabled={!isClickable}
      >
        <span className="font-medium text-text-primary-70">{String(slot.slotNumber).padStart(padWidth, "0")}</span>
      </button>
      {isSelected && showPopover && (
        <SlotPopover
          selectFromListDisabled={!hasMiners}
          onSelectFromList={onSelectFromList}
          onSearchMiners={onSearchMiners}
          onDismiss={onPopoverDismiss}
        />
      )}
    </div>
  );
}

export default function RackPane({
  rows,
  cols,
  numberingOrigin,
  slotAssignments,
  assignmentMode,
  assignedCount,
  totalSlots,
  originLabel,
  selectedSlotKey,
  showPopover,
  hasMiners,
  onCellClick,
  onSelectFromList,
  onSearchMiners,
  onPopoverDismiss,
  onHoverMiner,
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
    <div className="flex min-h-0 flex-1 flex-col p-4">
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
                isSelected={selectedSlotKey === slot.key}
                showPopover={showPopover && selectedSlotKey === slot.key}
                hasMiners={hasMiners}
                slotSize={slotSize}
                padWidth={padWidth}
                onCellClick={onCellClick}
                onSelectFromList={onSelectFromList}
                onSearchMiners={onSearchMiners}
                onPopoverDismiss={onPopoverDismiss}
                onHoverMiner={onHoverMiner}
              />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
