/**
 * Container-module hashboard layout.
 *
 * A container hashboard renders a fixed grid of ASICs: 12 display rows and
 * `columns` columns. Rows are grouped into six "letter pairs" (F, E, D, C, B, A,
 * top to bottom); each pair has a top sub-row (`0`) and a bottom sub-row (`1`),
 * so the display-row order is F0, F1, E0, E1, D0, D1, C0, C1, B0, B1, A0, A1.
 *
 * ASICs are numbered serpentine (clockwise) within each letter pair, resetting
 * per letter:
 *   - top sub-row, left -> right:    F1, F2, ... F{N}
 *   - bottom sub-row, left -> right: F{2N}, F{2N-1}, ... F{N+1}
 * so for N = 28 columns the top sub-row is F1..F28 and the bottom sub-row is
 * F56 (col 0) .. F29 (col 27).
 *
 * These helpers are position-based (display row + column), which matches the
 * `row`/`column` fields already carried on `AsicData` and consumed by
 * `AsicTablePreview`. They intentionally do not assume a hardware ASIC-index
 * ordering, which is not yet confirmed.
 */

import { CONTAINER_HASHBOARD_COLUMNS } from "@/shared/utils/containerAsicLayout";

/** Letter for each row pair, top to bottom. */
export const CONTAINER_HASHBOARD_ROW_LETTERS = ["F", "E", "D", "C", "B", "A"] as const;

export { CONTAINER_HASHBOARD_COLUMNS };

/** Number of display rows on a container hashboard (6 letter pairs x 2). */
export const CONTAINER_HASHBOARD_ROWS = CONTAINER_HASHBOARD_ROW_LETTERS.length * 2;

/**
 * Serpentine ASIC label for a grid position, e.g. (row 0, col 0) -> "F1",
 * (row 1, col 0, 28 cols) -> "F56", (row 1, col 27, 28 cols) -> "F29".
 * Returns "" if the position is out of range.
 */
export const getContainerAsicLabel = (row: number, col: number, columns = CONTAINER_HASHBOARD_COLUMNS): string => {
  if (
    !Number.isInteger(row) ||
    !Number.isInteger(col) ||
    row < 0 ||
    row >= CONTAINER_HASHBOARD_ROWS ||
    col < 0 ||
    col >= columns
  ) {
    return "";
  }

  const letter = CONTAINER_HASHBOARD_ROW_LETTERS[Math.floor(row / 2)];
  const isBottomSubRow = row % 2 === 1;

  // Top sub-row counts up left-to-right (1..N); bottom sub-row counts down
  // left-to-right (2N..N+1) to complete the clockwise loop.
  const position = isBottomSubRow ? 2 * columns - col : col + 1;

  return `${letter}${position}`;
};
