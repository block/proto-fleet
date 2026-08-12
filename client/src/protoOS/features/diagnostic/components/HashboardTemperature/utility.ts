// import { AsicStats, HashboardInfo } from "@/protoOS/api/generatedApi";
import { AsicData } from "@/protoOS/store";

export const sortAsics = (asics: AsicData[]) => {
  return asics.sort((a, b) => {
    if (a.row === b.row) {
      return (a.column || 0) - (b.column || 0);
    }

    return (a.row || 0) - (b.row || 0);
  });
};

/**
 * Groups ASICs into display rows in one pass.
 *
 * A row appears only if at least one ASIC on it carries full position data, but
 * the row then holds every ASIC sharing that row index. Row order follows first
 * appearance in `asics`, so a sorted input yields sorted rows.
 */
export const groupAsicsByRow = (asics: AsicData[]): { row: number; asics: AsicData[] }[] => {
  const asicsByRow = new Map<number, AsicData[]>();

  for (const asic of asics) {
    if (asic.row === undefined) continue;

    const bucket = asicsByRow.get(asic.row);
    if (bucket) {
      bucket.push(asic);
    } else {
      asicsByRow.set(asic.row, [asic]);
    }
  }

  const rows: { row: number; asics: AsicData[] }[] = [];
  const seen = new Set<number>();

  for (const asic of asics) {
    if (asic.row === undefined || asic.column === undefined || seen.has(asic.row)) continue;

    seen.add(asic.row);
    rows.push({ row: asic.row, asics: asicsByRow.get(asic.row)! });
  }

  return rows;
};

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";

export const getRowLabel = (row: number) => {
  return alphabet.charAt(row);
};
