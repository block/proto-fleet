import { describe, expect, it } from "vitest";

import { type RackPickerItem } from "../rackPickerItem";
import { computeRackSelectionDelta } from "./rackSelectionDelta";

const eligible = (id: string, label = `R-${id}`): RackPickerItem => ({
  id,
  label,
  buildingLabel: "—",
  statusLabel: "Unassigned",
  disabled: false,
});

const disabledItem = (id: string, label = `R-${id}`): RackPickerItem => ({
  id,
  label,
  buildingLabel: "Other",
  statusLabel: "In another building",
  disabled: true,
});

describe("computeRackSelectionDelta", () => {
  it("returns empty delta when nothing changed", () => {
    const items = [eligible("1"), eligible("2")];
    const out = computeRackSelectionDelta(items, [1n, 2n], ["1", "2"]);
    expect(out.added).toEqual([]);
    expect(out.removed).toEqual([]);
  });

  it("classifies newly-checked ids as added with labels", () => {
    const items = [eligible("1"), eligible("2", "Rack-2")];
    const out = computeRackSelectionDelta(items, [1n], ["1", "2"]);
    expect(out.added).toEqual([{ rackId: 2n, label: "Rack-2" }]);
    expect(out.removed).toEqual([]);
  });

  it("classifies seeded-and-now-unchecked ids as removed", () => {
    const items = [eligible("1"), eligible("2"), eligible("3")];
    const out = computeRackSelectionDelta(items, [1n, 2n, 3n], ["1", "3"]);
    expect(out.added).toEqual([]);
    expect(out.removed).toEqual([2n]);
  });

  it("preserves seeded ids missing from items (race / paging gap)", () => {
    // Seeded 99n is no longer in the listRacks response. Without this
    // guard, the previous keep-set shape silently removed it.
    const items = [eligible("1")];
    const out = computeRackSelectionDelta(items, [1n, 99n], ["1"]);
    expect(out.removed).toEqual([]);
  });

  it("does not add disabled-row ids even if selectedItems lists them", () => {
    // Defensive: the row gate prevents toggling, but if the seeded set
    // included a disabled id and the operator never touched it, we
    // should not surface it as new.
    const items = [eligible("1"), disabledItem("2")];
    const out = computeRackSelectionDelta(items, [], ["1", "2"]);
    expect(out.added).toEqual([{ rackId: 1n, label: "R-1" }]);
  });

  it("mixed delta: one add + one remove + one untouched-missing", () => {
    const items = [eligible("1"), eligible("3"), eligible("4")];
    const out = computeRackSelectionDelta(items, [1n, 3n, 99n], ["1", "4"]);
    expect(out.added).toEqual([{ rackId: 4n, label: "R-4" }]);
    expect(out.removed).toEqual([3n]); // 99n stays — missing from items
  });
});
