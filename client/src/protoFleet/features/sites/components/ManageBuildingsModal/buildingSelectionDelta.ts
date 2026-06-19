// Pure delta computation for ManageBuildingsModal so the "seeded id missing
// from items is preserved" invariant can be unit-tested. Mirrors
// computeRackSelectionDelta: a seeded id absent from the listed items is
// left untouched (we can't tell deselection from a paging / race gap), so
// the caller never accidentally unassigns a building the response omitted.

import { type BuildingPickerItem } from "./buildingPickerItem";

export interface BuildingSelectionDelta {
  added: { buildingId: bigint; label: string }[];
  removed: bigint[];
}

// Compute the delta between the seeded selection (initial) and the
// operator's checked state (selectedItemIds) given the picker's current
// items list.
//
//   added: ids the operator just checked. Skipped when the item is
//   disabled or absent from items.
//
//   removed: seeded ids the operator unchecked. Skipped when the id is
//   absent from items — that means we don't actually know whether the
//   operator deselected it or whether listBuildings didn't return it. The
//   safe default is to leave it alone so the caller preserves membership.
export const computeBuildingSelectionDelta = (
  items: BuildingPickerItem[],
  initialSelectedBuildingIds: bigint[],
  selectedItemIds: string[],
): BuildingSelectionDelta => {
  const initialSet = new Set(initialSelectedBuildingIds.map((id) => id.toString()));
  const selectedSet = new Set(selectedItemIds);

  const added: { buildingId: bigint; label: string }[] = [];
  for (const id of selectedItemIds) {
    if (initialSet.has(id)) continue;
    const item = items.find((b) => b.id === id);
    if (!item || item.disabled) continue;
    added.push({ buildingId: BigInt(id), label: item.label });
  }

  const removed: bigint[] = [];
  for (const id of initialSelectedBuildingIds) {
    if (selectedSet.has(id.toString())) continue;
    const seedItem = items.find((b) => b.id === id.toString());
    if (!seedItem) continue;
    removed.push(id);
  }

  return { added, removed };
};
