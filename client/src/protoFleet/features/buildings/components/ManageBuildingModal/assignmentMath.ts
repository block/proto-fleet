// Pure helpers for ManageBuildingModal's grid placement. Extracted so
// the bounds-drop and alphabetical-fill logic can be unit-tested
// without standing up the full modal.

import { cellKey, type GridCellKey } from "./types";

export interface AssignmentEntry {
  rackId: bigint;
  label: string;
  aisleIndex?: number;
  positionInAisle?: number;
}

// Compute the auto (byName) assignment map. Sort assigned racks by label
// and fill grid cells row-major (aisle 0 first, then aisle 1, ...) up
// to capacity. Returns an empty map when either dimension is 0.
export const buildByNameAssignments = (
  entries: AssignmentEntry[],
  aisles: number,
  racksPerAisle: number,
): Record<GridCellKey, bigint> => {
  if (aisles <= 0 || racksPerAisle <= 0) return {};
  const sorted = [...entries].sort((a, b) => a.label.localeCompare(b.label));
  const out: Record<GridCellKey, bigint> = {};
  let idx = 0;
  outer: for (let aisle = 0; aisle < aisles; aisle++) {
    for (let position = 0; position < racksPerAisle; position++) {
      if (idx >= sorted.length) break outer;
      out[cellKey(aisle, position)] = sorted[idx].rackId;
      idx++;
    }
  }
  return out;
};

// Map manual entries → cellKey → rackId. Entries with no position are
// excluded so the grid renders them as floating (visible in the list,
// no cell highlighted). Out-of-bounds positions are dropped — a
// shrunken layout silently drops cells that no longer exist, matching
// the ManageRackModal pattern of "membership outlives placement".
// The BE-side guard against orphaning shrinks lives in UpdateBuilding;
// this function only normalizes display state.
export const buildManualAssignments = (
  entries: AssignmentEntry[],
  aisles: number,
  racksPerAisle: number,
): Record<GridCellKey, bigint> => {
  const out: Record<GridCellKey, bigint> = {};
  for (const e of entries) {
    if (e.aisleIndex === undefined || e.positionInAisle === undefined) continue;
    if (e.aisleIndex < 0 || e.aisleIndex >= aisles) continue;
    if (e.positionInAisle < 0 || e.positionInAisle >= racksPerAisle) continue;
    out[cellKey(e.aisleIndex, e.positionInAisle)] = e.rackId;
  }
  return out;
};
