import { describe, expect, it } from "vitest";

import { type BuildingPickerItem } from "./buildingPickerItem";
import { computeBuildingSelectionDelta } from "./buildingSelectionDelta";

const eligible = (id: string, label = `B-${id}`): BuildingPickerItem => ({
  id,
  label,
  siteLabel: "—",
  statusLabel: "Unassigned",
  disabled: false,
});

const disabledItem = (id: string, label = `B-${id}`): BuildingPickerItem => ({
  id,
  label,
  siteLabel: "Other",
  statusLabel: "In another site",
  disabled: true,
});

describe("computeBuildingSelectionDelta", () => {
  it("returns empty delta when nothing changed", () => {
    const items = [eligible("1"), eligible("2")];
    const out = computeBuildingSelectionDelta(items, [1n, 2n], ["1", "2"]);
    expect(out.added).toEqual([]);
    expect(out.removed).toEqual([]);
  });

  it("classifies newly-checked ids as added with labels", () => {
    const items = [eligible("1"), eligible("2", "Building-2")];
    const out = computeBuildingSelectionDelta(items, [1n], ["1", "2"]);
    expect(out.added).toEqual([{ buildingId: 2n, label: "Building-2" }]);
    expect(out.removed).toEqual([]);
  });

  it("classifies seeded-and-now-unchecked ids as removed", () => {
    const items = [eligible("1"), eligible("2"), eligible("3")];
    const out = computeBuildingSelectionDelta(items, [1n, 2n, 3n], ["1", "3"]);
    expect(out.added).toEqual([]);
    expect(out.removed).toEqual([2n]);
  });

  it("preserves seeded ids missing from items (race / paging gap)", () => {
    const items = [eligible("1")];
    const out = computeBuildingSelectionDelta(items, [1n, 99n], ["1"]);
    expect(out.removed).toEqual([]);
  });

  it("does not add disabled-row ids even if selectedItems lists them", () => {
    const items = [eligible("1"), disabledItem("2")];
    const out = computeBuildingSelectionDelta(items, [], ["1", "2"]);
    expect(out.added).toEqual([{ buildingId: 1n, label: "B-1" }]);
  });

  it("mixed delta: one add + one remove + one untouched-missing", () => {
    const items = [eligible("1"), eligible("3"), eligible("4")];
    const out = computeBuildingSelectionDelta(items, [1n, 3n, 99n], ["1", "4"]);
    expect(out.added).toEqual([{ buildingId: 4n, label: "B-4" }]);
    expect(out.removed).toEqual([3n]); // 99n stays — missing from items
  });
});
