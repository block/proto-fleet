/**
 * Column count for a container hashboard's ASIC grid.
 *
 * Confirmed 26 (jmarr, 2026-07-28). With 26 columns each letter pair holds 52
 * ASICs: top sub-row F1..F26 (left to right), bottom sub-row F52..F27 (left to
 * right).
 */
export const CONTAINER_HASHBOARD_COLUMNS = 26;

/**
 * Display rows beyond this are treated as invalid controller data. A container
 * hashboard has 12; the margin allows other layouts without letting one bad
 * coordinate allocate an enormous grid.
 */
export const CONTAINER_HASHBOARD_MAX_ROWS = 64;

/** A controller-reported ASIC position on its hashboard. */
export type AsicLayoutPosition = {
  stackId?: number;
  stackIndex?: number;
  x?: number;
  y?: number;
};

/**
 * Returns the display cell for a controller-reported ASIC position. Some
 * controllers report each stack as one long row; that row wraps at `columns`
 * for display only, leaving ASIC indices and stack identities unchanged.
 * A position that falls outside the grid has no cell.
 */
export const containerAsicPlacement = (
  entries: AsicLayoutPosition[],
  asicsPerStack: number | undefined,
  columns = CONTAINER_HASHBOARD_COLUMNS,
) => {
  // reduce, not a spread: a misreported layout may be far larger than a board.
  const widest = entries.reduce((widest, entry) => Math.max(widest, (entry.x ?? 0) + 1), 0);
  const rowsPerStack = Math.max(Math.ceil((asicsPerStack || columns) / columns), Math.ceil(widest / columns));
  const wrap =
    widest > columns ||
    (rowsPerStack > 1 &&
      entries.length > 0 &&
      entries.every(
        (entry) =>
          entry.stackId !== undefined &&
          entry.stackIndex !== undefined &&
          entry.x === entry.stackIndex &&
          entry.y === entry.stackId,
      ));
  return (position?: AsicLayoutPosition): { row?: number; column?: number } => {
    const row =
      wrap && position?.x !== undefined && position.y !== undefined
        ? position.y * rowsPerStack + Math.floor(position.x / columns)
        : position?.y;
    const column = wrap && position?.x !== undefined ? position.x % columns : position?.x;
    const inGrid = (value: number | undefined, size: number) =>
      value !== undefined && Number.isInteger(value) && value >= 0 && value < size;
    return inGrid(row, CONTAINER_HASHBOARD_MAX_ROWS) && inGrid(column, columns) ? { row, column } : {};
  };
};
