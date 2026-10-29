/**
 * Maps the prototype's per-hashboard ASIC cells onto the shared
 * <AsicTablePreview> grid contract ({ row, col, value }).
 *
 * The prototype snapshot only carries a flat 0-based `index` per board, so we
 * lay the chips out into a roughly-wide grid. `value` is the chip temperature
 * (the heatmap axis AsicTablePreview colors on); an "off" chip becomes `null`
 * so the shared component renders it as an empty cell.
 */
import type { AsicCell } from "./types";
import type { AsicData } from "@/shared/components/AsicTablePreview";

/** Columns for a board of `count` chips — wide-ish so boards read as strips. */
export function gridColumns(count: number): number {
  if (count <= 0) return 1;
  return Math.max(1, Math.ceil(Math.sqrt(count * 4)));
}

export function toAsicData(asics: AsicCell[]): AsicData[] {
  const cols = gridColumns(asics.length);
  return asics.map((asic, i) => ({
    row: Math.floor(i / cols),
    col: i % cols,
    value: asic.health === "off" ? null : asic.tempC,
  }));
}
