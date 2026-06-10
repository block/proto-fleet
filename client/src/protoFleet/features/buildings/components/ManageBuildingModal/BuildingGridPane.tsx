import clsx from "clsx";

import { cellKey, type GridCellKey } from "./types";
import Button, { sizes, variants } from "@/shared/components/Button";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";

interface BuildingGridPaneProps {
  aisles: number;
  racksPerAisle: number;
  // Map of cellKey → rack label for cells that have a rack assigned. Empty
  // cells render as + placeholders.
  cellLabels: Record<GridCellKey, string>;
  // Map of cellKey → rackId so hover events can resolve back to a rack id
  // for the left-pane row highlight.
  cellRackIds: Record<GridCellKey, bigint>;
  // Cell click handler — only fires in manual mode. The host wires this so
  // byName mode ignores cell clicks (auto-fill owns the layout).
  onCellClick?: (aisle: number, position: number, key: GridCellKey) => void;
  selectedCellKey: GridCellKey | null;
  // Popover visibility for the selected cell. Mirrors RackPane.showPopover.
  showPopover: boolean;
  // Popover handlers — surfaced when a cell is selected and the popover is open.
  onSelectFromList: () => void;
  onSearchRacks: () => void;
  onPopoverDismiss: () => void;
  // Disables the "Select from list" popover option when there are no rows
  // available to drop into (matches the SlotPopover hasMiners gate).
  hasRacks: boolean;
  // Hover bridge to the left pane: cell-mouse-enter on assigned cells fires
  // onHoverRack(rackId); leave fires onHoverRack(null). The host pipes the
  // current hoveredRackId back via hoveredRackId so the cell can also
  // highlight when the corresponding row is hovered (symmetric).
  hoveredRackId: bigint | null;
  onHoverRack: (rackId: bigint | null) => void;
  // Empty-state escape hatch: when aisles or racks-per-aisle are unset, the
  // pane shows a "Building settings" link that opens the settings modal.
  onOpenSettings: () => void;
  // Compact summary line: "N of M cells filled" — surfaces in the right
  // pane's mini-header just like ManageRackModal's rack pane.
  assignedCount: number;
  totalCells: number;
}

// Mirrors RackPane's SlotPopover — same shape so the two surfaces feel
// identical. "Select from list" is disabled when the working set is empty
// (no rows to click).
function CellPopover({
  selectFromListDisabled,
  onSelectFromList,
  onSearchRacks,
  onDismiss,
}: {
  selectFromListDisabled: boolean;
  onSelectFromList: () => void;
  onSearchRacks: () => void;
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
            onSearchRacks();
          }}
        >
          Search racks
        </button>
      </div>
    </>
  );
}

// Renders the aisles × racks_per_aisle floor-plan grid. Cells are CSS-grid
// laid out so the visual maps 1:1 to the operator's mental model: each row
// is an aisle, each column is a slot within that aisle. The grid scales to
// the pane width so a 10-aisle × 20-rack building still reads at a glance.
const BuildingGridPane = ({
  aisles,
  racksPerAisle,
  cellLabels,
  cellRackIds,
  onCellClick,
  selectedCellKey,
  showPopover,
  onSelectFromList,
  onSearchRacks,
  onPopoverDismiss,
  hasRacks,
  hoveredRackId,
  onHoverRack,
  onOpenSettings,
  assignedCount,
  totalCells,
}: BuildingGridPaneProps) => {
  // Guard for un-initialized layouts. A building with aisles=0 or
  // racksPerAisle=0 has no grid to render — we show an empty-state with a
  // link to BuildingSettingsModal so the operator can fix the layout in one
  // click instead of hunting for the header button.
  if (aisles <= 0 || racksPerAisle <= 0) {
    return (
      <div className="flex h-full min-h-0 flex-col">
        <div className="flex shrink-0 items-start justify-between gap-4 px-5 pt-5">
          <span className="text-300 text-text-primary-50">Floor plan</span>
        </div>
        <div
          className="flex flex-1 flex-col items-center justify-center gap-3 p-5 text-center text-300 text-text-primary-50"
          data-testid="manage-building-grid-empty"
        >
          <span>Set aisles and racks per aisle to define the floor plan.</span>
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            onClick={onOpenSettings}
            testId="manage-building-grid-empty-open-settings"
          >
            Building settings
          </Button>
        </div>
      </div>
    );
  }

  const cells: { aisle: number; position: number; key: GridCellKey; label?: string; rackId?: bigint }[] = [];
  for (let aisle = 0; aisle < aisles; aisle++) {
    for (let position = 0; position < racksPerAisle; position++) {
      const key = cellKey(aisle, position);
      cells.push({ aisle, position, key, label: cellLabels[key], rackId: cellRackIds[key] });
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="manage-building-grid">
      <div className="flex shrink-0 items-start justify-between gap-4 px-5 pt-5">
        <span className="text-300 text-text-primary-50">Floor plan</span>
        <span className="shrink-0 text-300 text-text-primary-50">
          {assignedCount} of {totalCells} cells filled
        </span>
      </div>
      <div className="flex flex-1 items-center justify-center p-5">
        <div
          className="grid w-full max-w-[640px] gap-2"
          style={{ gridTemplateColumns: `repeat(${racksPerAisle}, minmax(0, 1fr))` }}
        >
          {cells.map((cell) => {
            const empty = !cell.label;
            const selectable = onCellClick !== undefined;
            const isSelected = selectedCellKey === cell.key;
            // Symmetric hover bridge: an assigned cell highlights when its
            // matching list row is hovered (host pipes hoveredRackId in),
            // and the list row highlights when this cell is hovered (we
            // emit onHoverRack on enter/leave).
            const isHovered = !!cell.rackId && hoveredRackId !== null && cell.rackId === hoveredRackId;
            return (
              <div key={cell.key} className="relative">
                <button
                  type="button"
                  onClick={selectable ? () => onCellClick(cell.aisle, cell.position, cell.key) : undefined}
                  onMouseEnter={() => cell.rackId !== undefined && onHoverRack(cell.rackId)}
                  onMouseLeave={() => cell.rackId !== undefined && onHoverRack(null)}
                  disabled={!selectable}
                  className={clsx(
                    "flex aspect-square w-full items-center justify-center rounded-lg text-300 transition-colors",
                    // Three visual states mirror RackPane exactly: selected
                    // (warning ring), assigned (primary border on surface),
                    // empty (border-10 outline on transparent). Hover lifts
                    // empty cells with a primary tint.
                    isSelected
                      ? "border-2 border-intent-warning-fill bg-surface-base text-emphasis-300 text-text-primary"
                      : empty
                        ? "border border-border-10 bg-transparent text-text-primary-30"
                        : "border-2 border-core-primary-fill bg-surface-base text-emphasis-300 text-text-primary",
                    // Symmetric-hover lift: matching list-row hover renders
                    // the cell as if mouse were over it.
                    isHovered && !isSelected && "bg-core-primary-5",
                    selectable &&
                      !isSelected &&
                      "cursor-pointer hover:border-core-primary-fill hover:bg-core-primary-5",
                    selectable && isSelected && "cursor-pointer",
                    !selectable && "cursor-default",
                  )}
                  data-testid={`manage-building-grid-cell-${cell.key}`}
                  data-cell-state={isSelected ? "selected" : empty ? "empty" : "assigned"}
                  aria-label={
                    empty
                      ? `Empty cell at aisle ${cell.aisle + 1}, position ${cell.position + 1}`
                      : `${cell.label} at aisle ${cell.aisle + 1}, position ${cell.position + 1}`
                  }
                >
                  {empty ? <span aria-hidden="true">+</span> : <span className="truncate px-1">{cell.label}</span>}
                </button>
                {isSelected && showPopover ? (
                  <CellPopover
                    selectFromListDisabled={!hasRacks}
                    onSelectFromList={onSelectFromList}
                    onSearchRacks={onSearchRacks}
                    onDismiss={onPopoverDismiss}
                  />
                ) : null}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
};

export default BuildingGridPane;
export type { BuildingGridPaneProps };
