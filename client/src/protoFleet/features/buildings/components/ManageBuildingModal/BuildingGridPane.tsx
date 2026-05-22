import clsx from "clsx";

import { cellKey, type GridCellKey } from "./types";

interface BuildingGridPaneProps {
  aisles: number;
  racksPerAisle: number;
  // Map of cellKey → rack label for cells that have a rack assigned. Empty
  // cells render as + placeholders.
  cellLabels: Record<GridCellKey, string>;
  // Cell click handler — only fires in manual mode. The host wires this so
  // byName mode ignores cell clicks (auto-fill owns the layout).
  onCellClick?: (aisle: number, position: number, key: GridCellKey) => void;
  selectedCellKey: GridCellKey | null;
  // Compact summary line: "N of M cells filled" — surfaces in the right
  // pane's mini-header just like AssignMinersModal's rack pane.
  assignedCount: number;
  totalCells: number;
}

// Renders the aisles × racks_per_aisle floor-plan grid. Cells are CSS-grid
// laid out so the visual maps 1:1 to the operator's mental model: each row
// is an aisle, each column is a slot within that aisle. The grid scales to
// the pane width so a 10-aisle × 20-rack building still reads at a glance.
const BuildingGridPane = ({
  aisles,
  racksPerAisle,
  cellLabels,
  onCellClick,
  selectedCellKey,
  assignedCount,
  totalCells,
}: BuildingGridPaneProps) => {
  // Guard for un-initialized layouts. A building with aisles=0 or
  // racksPerAisle=0 has no grid to render — we show an empty-state instead
  // of an awkward 0×0 grid that would collapse to nothing.
  if (aisles <= 0 || racksPerAisle <= 0) {
    return (
      <div className="flex h-full min-h-0 flex-col">
        <div className="flex shrink-0 items-start justify-between gap-4 px-5 pt-5">
          <span className="text-300 text-text-primary-50">Floor plan</span>
        </div>
        <div
          className="flex flex-1 items-center justify-center p-5 text-300 text-text-primary-50"
          data-testid="manage-building-grid-empty"
        >
          Set aisles and racks per aisle to define the floor plan.
        </div>
      </div>
    );
  }

  const cells: { aisle: number; position: number; key: GridCellKey; label?: string }[] = [];
  for (let aisle = 0; aisle < aisles; aisle++) {
    for (let position = 0; position < racksPerAisle; position++) {
      const key = cellKey(aisle, position);
      cells.push({ aisle, position, key, label: cellLabels[key] });
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
            return (
              <button
                key={cell.key}
                type="button"
                onClick={selectable ? () => onCellClick(cell.aisle, cell.position, cell.key) : undefined}
                disabled={!selectable}
                className={clsx(
                  "flex aspect-square items-center justify-center rounded-lg border text-300",
                  empty
                    ? "border-dashed border-border-5 text-text-primary-30"
                    : "border-border-100 bg-surface-base text-emphasis-300 text-text-primary",
                  selectable && "hover:border-border-100 hover:bg-surface-base-hover cursor-pointer",
                  !selectable && "cursor-default",
                  isSelected && "ring-intent-focus-fill ring-2",
                )}
                data-testid={`manage-building-grid-cell-${cell.key}`}
                aria-label={
                  empty
                    ? `Empty cell at aisle ${cell.aisle + 1}, position ${cell.position + 1}`
                    : `${cell.label} at aisle ${cell.aisle + 1}, position ${cell.position + 1}`
                }
              >
                {empty ? <span aria-hidden="true">+</span> : <span className="truncate px-1">{cell.label}</span>}
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
};

export default BuildingGridPane;
export type { BuildingGridPaneProps };
