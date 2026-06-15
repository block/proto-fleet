import clsx from "clsx";

import { type BuildingAssignmentMode, type GridCellKey, parseCellKey } from "./types";
import { ArrowRight, Checkmark, DismissTiny } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

interface AssignedRackRow {
  rackId: bigint;
  label: string;
  // Position string for the inline secondary line; undefined when not placed.
  positionLabel?: string;
}

interface BuildingRacksPaneProps {
  assignmentMode: BuildingAssignmentMode;
  onModeChange: (mode: BuildingAssignmentMode) => void;
  assignedRacks: AssignedRackRow[];
  selectedRackId: bigint | null;
  // Selected cell — drives the inline "drop into Aisle N, position M" hint
  // shown on the active rack row, mirroring MinerRow's slot-awaiting hint.
  selectedCellKey: GridCellKey | null;
  // Symmetric-hover bridge from the grid pane. When the operator hovers an
  // assigned cell, the matching row picks up a hover highlight (mirrors
  // MinersPane.isHovered).
  hoveredRackId: bigint | null;
  onHoverRack: (rackId: bigint | null) => void;
  onSelectRack: (rackId: bigint | null) => void;
  onRemoveRack: (rackId: bigint) => void;
  // Header-CTA-driven entry point. Pane shows a "Manage racks" link in the
  // empty-state copy so the operator can find their way to the bulk-add
  // surface even before any rack lands.
  onOpenManageRacks: () => void;
  saving?: boolean;
}

// Mode toggle uses the same Button primary/secondary swap MinersPane uses for
// its Assign manually / by name / by network segments — keeps the visual
// vocabulary consistent across rack-management surfaces.
const modeSegments: { key: BuildingAssignmentMode; title: string; testId: string }[] = [
  { key: "manual", title: "Assign manually", testId: "manage-building-mode-manual" },
  { key: "byName", title: "Assign by name", testId: "manage-building-mode-byname" },
];

interface RackRowProps {
  row: AssignedRackRow;
  isSelected: boolean;
  isHovered: boolean;
  isManual: boolean;
  saving: boolean;
  selectedCellHint: string | null;
  onHover: (rackId: bigint | null) => void;
  onSelect: (rackId: bigint | null) => void;
  onRemove: (rackId: bigint) => void;
}

// Row layout mirrors MinerRow from ManageRackModal: leading icon column
// (arrow when selected / checkmark when placed), title + subtitle stack,
// trailing hint or position label, and a trailing remove control. Manual
// mode is the only state where rows are interactive — byName mode
// presents the same rows read-only because positions are derived.
const RackRow = ({
  row,
  isSelected,
  isHovered,
  isManual,
  saving,
  selectedCellHint,
  onHover,
  onSelect,
  onRemove,
}: RackRowProps) => {
  const isAssigned = !!row.positionLabel;
  const isClickable = isManual && !saving;
  const hasIcon = isSelected || isAssigned;

  const handleClick = () => {
    if (!isClickable) return;
    onSelect(isSelected ? null : row.rackId);
  };

  return (
    <div
      className={clsx(
        "flex items-center px-3 py-3 transition-colors",
        isSelected && "bg-surface-5",
        !isSelected && isHovered && "bg-surface-5",
        isClickable && !isSelected && "cursor-pointer hover:bg-surface-5",
        !isClickable && "cursor-default",
      )}
      onClick={handleClick}
      onMouseEnter={() => isAssigned && onHover(row.rackId)}
      onMouseLeave={() => isAssigned && onHover(null)}
      data-testid={`manage-building-assigned-rack-${row.rackId.toString()}`}
    >
      <div
        className="shrink-0 overflow-hidden transition-all duration-300"
        style={{ width: hasIcon ? 32 : 0, marginRight: hasIcon ? 8 : 0 }}
      >
        <div
          className={clsx(
            "flex h-6 w-6 items-center justify-center rounded-full",
            isSelected && "bg-core-primary-fill",
            !isSelected && isAssigned && "bg-intent-success-fill",
          )}
        >
          {isSelected ? <ArrowRight width="w-3" className="text-white" /> : null}
          {!isSelected && isAssigned ? <Checkmark width="w-4" className="text-white" /> : null}
        </div>
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-300 text-text-primary">{row.label || "(unnamed rack)"}</div>
      </div>

      {isSelected ? (
        <span className="shrink-0 text-200 text-text-primary">{selectedCellHint ?? "select rack position"}</span>
      ) : null}

      {!isSelected && row.positionLabel ? (
        <span className="shrink-0 text-300 font-medium text-text-primary tabular-nums">{row.positionLabel}</span>
      ) : null}

      <div className="relative shrink-0">
        <button
          type="button"
          aria-label="Remove rack"
          className="flex h-8 w-8 items-center justify-center rounded-lg text-text-primary-70 hover:cursor-pointer"
          onClick={(e) => {
            e.stopPropagation();
            onRemove(row.rackId);
          }}
          disabled={saving}
          data-testid={`manage-building-remove-rack-${row.rackId.toString()}`}
        >
          <DismissTiny />
        </button>
      </div>
    </div>
  );
};

const BuildingRacksPane = ({
  assignmentMode,
  onModeChange,
  assignedRacks,
  selectedRackId,
  selectedCellKey,
  hoveredRackId,
  onHoverRack,
  onSelectRack,
  onRemoveRack,
  onOpenManageRacks,
  saving = false,
}: BuildingRacksPaneProps) => {
  const isManual = assignmentMode === "manual";
  const selectedCellHint = selectedCellKey
    ? (() => {
        const { aisle, position } = parseCellKey(selectedCellKey);
        return `assign to Aisle ${aisle + 1}, position ${position + 1}`;
      })()
    : null;

  return (
    <div className="flex flex-col gap-6 pr-6 pb-6 laptop:pr-10 laptop:pb-10">
      <section className="flex flex-col gap-3">
        <Header title="Racks" titleSize="text-heading-100" />
        <div className="flex items-center gap-2">
          {modeSegments.map((seg) => (
            <Button
              key={seg.key}
              variant={assignmentMode === seg.key ? variants.primary : variants.secondary}
              size={sizes.compact}
              onClick={() => onModeChange(seg.key)}
              disabled={saving}
              testId={seg.testId}
            >
              {seg.title}
            </Button>
          ))}
        </div>
        {assignedRacks.length === 0 ? (
          <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-50">
            <span>No racks added to this building yet.</span>
            <Button
              variant={variants.primary}
              size={sizes.compact}
              onClick={onOpenManageRacks}
              disabled={saving}
              testId="manage-building-empty-state-add"
            >
              Manage racks
            </Button>
          </div>
        ) : (
          <div className="flex flex-col divide-y divide-border-5" data-testid="manage-building-assigned-racks">
            {assignedRacks.map((row) => (
              <RackRow
                key={row.rackId.toString()}
                row={row}
                isSelected={selectedRackId === row.rackId}
                isHovered={hoveredRackId === row.rackId}
                isManual={isManual}
                saving={saving}
                selectedCellHint={selectedCellHint}
                onHover={onHoverRack}
                onSelect={onSelectRack}
                onRemove={onRemoveRack}
              />
            ))}
          </div>
        )}
      </section>
    </div>
  );
};

export default BuildingRacksPane;
export type { AssignedRackRow, BuildingRacksPaneProps };
