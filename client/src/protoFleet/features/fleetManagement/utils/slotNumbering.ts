export type NumberingOrigin = "bottom-left" | "top-left" | "bottom-right" | "top-right";

export function computeSlotNumber(
  row: number,
  col: number,
  rows: number,
  cols: number,
  origin: NumberingOrigin,
): number {
  switch (origin) {
    case "bottom-left":
      return (rows - 1 - row) * cols + col + 1;
    case "top-left":
      return row * cols + col + 1;
    case "bottom-right":
      return (rows - 1 - row) * cols + (cols - 1 - col) + 1;
    case "top-right":
      return row * cols + (cols - 1 - col) + 1;
  }
}

export function slotNumberToRowCol(
  slotNumber: number,
  rows: number,
  cols: number,
  origin: NumberingOrigin,
): { row: number; col: number } {
  const index = slotNumber - 1;
  const gridRow = Math.floor(index / cols);
  const gridCol = index % cols;

  switch (origin) {
    case "bottom-left":
      return { row: rows - 1 - gridRow, col: gridCol };
    case "top-left":
      return { row: gridRow, col: gridCol };
    case "bottom-right":
      return { row: rows - 1 - gridRow, col: cols - 1 - gridCol };
    case "top-right":
      return { row: gridRow, col: cols - 1 - gridCol };
  }
}
