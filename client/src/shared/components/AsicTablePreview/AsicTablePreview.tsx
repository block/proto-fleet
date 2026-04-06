import { useMemo } from "react";
import type { AsicCellProps, AsicTablePreviewProps } from "./types";
import { map } from "@/shared/utils/math";

// Get color and opacity for a temperature value
const AsicCell = ({
  value,
  min,
  row,
  col,
  warningThreshold,
  criticalThreshold,
  dangerThreshold,
  colors,
}: AsicCellProps) => {
  // Calculate opacity based on temperature ranges
  let opacity: number;
  if (value === null || value === undefined) {
    opacity = 1.0;
  } else if (value >= criticalThreshold) {
    opacity = 1.0;
  } else if (value >= warningThreshold) {
    opacity = map(value, warningThreshold, criticalThreshold, 0.4, 1);
  } else {
    opacity = map(value, min, warningThreshold, 0.4, 0.05);
  }

  // Round opacity to nearest 0.05
  opacity = Math.round(opacity * 20) / 20;

  // Determine color based on thresholds
  let color: string;
  if (value === null || value === undefined) {
    color = colors.empty;
  } else if (value >= dangerThreshold) {
    color = colors.critical;
  } else if (value >= warningThreshold) {
    color = colors.warning;
  } else {
    color = colors.normal;
  }

  return (
    <div
      className="h-1.5 flex-1 rounded-sm"
      style={{
        backgroundColor: color,
        opacity,
      }}
      data-testid={`asic-${row}-${col}`}
      data-value={value}
    />
  );
};

const AsicTablePreview = ({
  asics,
  min = 0,
  warningThreshold = 65,
  dangerThreshold = 82,
  criticalThreshold = 90,
  colors = {
    normal: "var(--color-intent-info-fill)",
    warning: "var(--color-intent-warning-fill)",
    critical: "var(--color-intent-critical-fill)",
    empty: "var(--color-surface-5)",
  },
  className = "",
}: AsicTablePreviewProps) => {
  // Calculate grid dimensions and populate values
  const grid = useMemo(() => {
    if (!asics || asics.length === 0) {
      return [];
    }

    const maxRow = Math.max(...asics.map((a) => a.row), 0) + 1;
    const maxCol = Math.max(...asics.map((a) => a.col), 0) + 1;

    // Create a 2D array for the grid
    const gridArray: Array<Array<number | null>> = Array(maxRow)
      .fill(null)
      .map(() => Array(maxCol).fill(null));

    // Populate the grid with values
    asics.forEach((asic) => {
      if (asic.row >= 0 && asic.col >= 0) {
        gridArray[asic.row][asic.col] = asic.value;
      }
    });

    return gridArray;
  }, [asics]);

  return (
    <div className={`flex flex-col gap-1 ${className}`}>
      {grid.map((row, rowIndex) => (
        <div key={rowIndex} className="flex gap-1">
          {row.map((value, colIndex) => (
            <AsicCell
              key={`${rowIndex}-${colIndex}`}
              row={rowIndex}
              col={colIndex}
              value={value}
              min={min}
              warningThreshold={warningThreshold}
              criticalThreshold={criticalThreshold}
              dangerThreshold={dangerThreshold}
              colors={colors}
            />
          ))}
        </div>
      ))}
    </div>
  );
};

export default AsicTablePreview;
