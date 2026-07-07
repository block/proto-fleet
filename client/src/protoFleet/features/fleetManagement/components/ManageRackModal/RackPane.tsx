import { useMemo } from "react";
import clsx from "clsx";

import type { AssignmentMode } from "./types";
import { computeSlotNumber, type NumberingOrigin } from "@/protoFleet/features/fleetManagement/utils/slotNumbering";
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
  onScanQr: () => void;
  onPopoverDismiss: () => void;
  onHoverMiner: (minerId: string | null) => void;
}

interface SlotInfo {
  row: number;
  col: number;
  slotNumber: number;
  key: string;
}

type PopoverAnchorX = "left" | "center" | "right";

function SlotPopover({
  anchorX,
  selectFromListDisabled,
  onSelectFromList,
  onSearchMiners,
  onScanQr,
  onDismiss,
}: {
  anchorX: PopoverAnchorX;
  selectFromListDisabled: boolean;
  onSelectFromList: () => void;
  onSearchMiners: () => void;
  onScanQr: () => void;
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
        className={clsx(
          "absolute top-full z-30 mt-1 w-44 rounded-xl border border-border-5 bg-surface-elevated-base py-1 shadow-300",
          // The menu is wider than a slot, so anchor it toward the grid interior:
          // edge-column slots would otherwise spill off the viewport when centered.
          anchorX === "left" && "left-0",
          anchorX === "right" && "right-0",
          anchorX === "center" && "left-1/2 -translate-x-1/2",
        )}
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
        <button
          type="button"
          role="menuitem"
          className="w-full px-4 py-2 text-left text-300 text-text-primary hover:bg-surface-5"
          onClick={(e) => {
            e.stopPropagation();
            onScanQr();
          }}
        >
          Scan barcode
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
  popoverAnchorX,
  hasMiners,
  slotSize,
  padWidth,
  onCellClick,
  onSelectFromList,
  onSearchMiners,
  onScanQr,
  onPopoverDismiss,
  onHoverMiner,
}: {
  slot: SlotInfo;
  assignedMinerId: string | undefined;
  isManualMode: boolean;
  isSelected: boolean;
  showPopover: boolean;
  popoverAnchorX: PopoverAnchorX;
  hasMiners: boolean;
  slotSize: number;
  padWidth: number;
  onCellClick: (row: number, col: number) => void;
  onSelectFromList: () => void;
  onSearchMiners: () => void;
  onScanQr: () => void;
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
      {isSelected && showPopover ? (
        <SlotPopover
          anchorX={popoverAnchorX}
          selectFromListDisabled={!hasMiners}
          onSelectFromList={onSelectFromList}
          onSearchMiners={onSearchMiners}
          onScanQr={onScanQr}
          onDismiss={onPopoverDismiss}
        />
      ) : null}
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
  onScanQr,
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
      {/* Negative margins escape outer p-4 + wrapper laptop:pl-6 → labels land 20px from pane edge. */}
      <div className="-mx-4 flex shrink-0 items-center justify-between pt-1 pr-5 pb-4 pl-5 laptop:-ml-10">
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
                showPopover={showPopover ? selectedSlotKey === slot.key : false}
                popoverAnchorX={slot.col === 0 ? "left" : slot.col === cols - 1 ? "right" : "center"}
                hasMiners={hasMiners}
                slotSize={slotSize}
                padWidth={padWidth}
                onCellClick={onCellClick}
                onSelectFromList={onSelectFromList}
                onSearchMiners={onSearchMiners}
                onScanQr={onScanQr}
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
