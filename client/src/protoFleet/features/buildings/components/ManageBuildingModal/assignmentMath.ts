// Pure helpers for ManageBuildingModal's grid placement. Extracted so
// the bounds-drop and alphabetical-fill logic can be unit-tested
// without standing up the full modal.

import { cellKey, type GridCellKey, parseCellKey } from "./types";
import { type RackPlacementInput } from "@/protoFleet/api/buildings";

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

// The AssignRacksToBuilding batches a Save would dispatch, diffed from the
// load-time snapshot. Empty across all three buckets means the working set
// matches the server — the Save CTA is gated on that so a clean modal can't
// fire a no-op write (or toast a save that never happened).
export interface PlacementDelta {
  // Racks that left this building — dispatched with targetBuildingId
  // undefined, so they can't ride the in-building batch.
  unassign: RackPlacementInput[];
  // Racks staying in the building whose cell must be cleared. Includes both
  // explicit unplacements and a synthetic pre-place vacate for every mover,
  // so pass 1 frees every cell pass 2 will claim.
  inBuildingVacate: RackPlacementInput[];
  // Racks landing at a specific (aisle, position). Must run after every
  // vacate above, or a cross-chunk swap trips
  // uk_device_set_rack_building_position.
  inBuildingPlace: RackPlacementInput[];
}

export const isPlacementDeltaEmpty = (delta: PlacementDelta): boolean =>
  delta.unassign.length === 0 && delta.inBuildingVacate.length === 0 && delta.inBuildingPlace.length === 0;

// Diff the working set against the load-time snapshot (rackId →
// "aisle:position" | "unplaced") and bucket the result into the two-pass
// dispatch shape handleSave sends.
export const buildPlacementDelta = (
  entries: AssignmentEntry[],
  rackToCell: Map<string, GridCellKey>,
  initial: Map<string, string>,
): PlacementDelta => {
  const unassign: RackPlacementInput[] = [];
  const inBuildingVacate: RackPlacementInput[] = [];
  const inBuildingPlace: RackPlacementInput[] = [];
  const seenVacate = new Set<string>();

  for (const entry of entries) {
    const idStr = entry.rackId.toString();
    const placedKey = rackToCell.get(idStr);
    const next = placedKey
      ? (() => {
          const { aisle, position } = parseCellKey(placedKey);
          return `${aisle}:${position}`;
        })()
      : "unplaced";
    const prior = initial.get(idStr) ?? "missing";
    if (prior === next) continue;

    if (placedKey) {
      // Placement or move. A mover's old cell needs a pre-place vacate so
      // it's free before any placement chunk runs.
      const wasPlaced = prior !== "unplaced" && prior !== "missing";
      if (wasPlaced && !seenVacate.has(idStr)) {
        inBuildingVacate.push({ rackId: entry.rackId });
        seenVacate.add(idStr);
      }
      const { aisle, position } = parseCellKey(placedKey);
      inBuildingPlace.push({ rackId: entry.rackId, aisleIndex: aisle, positionInAisle: position });
    } else if (!seenVacate.has(idStr)) {
      // No cell chosen. Either a cell-clear (prior was placed) or a rack
      // newly added to the working set that was never dragged to a cell —
      // both ship as a member-only assign so the BE links/keeps the rack in
      // this building. Without the latter, racks added via Manage racks but
      // never placed would silently drop on save.
      inBuildingVacate.push({ rackId: entry.rackId });
      seenVacate.add(idStr);
    }
  }

  // Racks in the snapshot but no longer in the working set.
  const currentIds = new Set(entries.map((e) => e.rackId.toString()));
  for (const idStr of initial.keys()) {
    if (currentIds.has(idStr)) continue;
    unassign.push({ rackId: BigInt(idStr) });
  }

  return { unassign, inBuildingVacate, inBuildingPlace };
};
