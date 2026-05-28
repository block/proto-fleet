// Pure delta computation extracted from ManageRacksModal so the
// "seeded id missing from items is preserved" invariant can be
// unit-tested. This is the bug-fix anchor for the previous keep-set
// shape that silently dropped seeded racks the listRacks response
// happened to omit (race / paging / soft-delete window).

import { type RackPickerItem } from "../rackPickerItem";

export interface RackSelectionDelta {
  added: { rackId: bigint; label: string }[];
  removed: bigint[];
}

// Compute the delta between the seeded selection (initial) and the
// operator's checked state (selectedItemIds) given the picker's
// current items list.
//
//   added: ids the operator just checked. Skipped when the item is
//   disabled (operator shouldn't have been able to toggle one, but
//   defensive) or absent from items.
//
//   removed: seeded ids the operator unchecked. Skipped when the id
//   is absent from items — that means we don't actually know whether
//   the operator deselected it or whether listRacks didn't return it.
//   The safe default is to leave it alone so the caller preserves
//   membership.
export const computeRackSelectionDelta = (
  items: RackPickerItem[],
  initialSelectedRackIds: bigint[],
  selectedItemIds: string[],
): RackSelectionDelta => {
  const initialSet = new Set(initialSelectedRackIds.map((id) => id.toString()));
  const selectedSet = new Set(selectedItemIds);

  const added: { rackId: bigint; label: string }[] = [];
  for (const id of selectedItemIds) {
    if (initialSet.has(id)) continue;
    const item = items.find((r) => r.id === id);
    if (!item || item.disabled) continue;
    added.push({ rackId: BigInt(id), label: item.label });
  }

  const removed: bigint[] = [];
  for (const id of initialSelectedRackIds) {
    if (selectedSet.has(id.toString())) continue;
    const seedItem = items.find((r) => r.id === id.toString());
    if (!seedItem) continue;
    removed.push(id);
  }

  return { added, removed };
};
