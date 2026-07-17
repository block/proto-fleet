import { describe, expect, it } from "vitest";

import { type RackPickerItem } from "../rackPickerItem";
import { computeRackSelectionDelta } from "./rackSelectionDelta";

const eligible = (id: string, label = `R-${id}`): RackPickerItem => ({
  id,
  label,
  buildingLabel: "—",
  statusLabel: "Unassigned",
  disabled: false,
  reassignment: false,
  minerCount: 0,
});

// A rack currently placed elsewhere — selectable only with the "Show assigned
// racks" toggle on, and reported in `reassigned` so the host can gate the
// reparent confirm.
const reassignmentItem = (id: string, minerCount: number, label = `R-${id}`): RackPickerItem => ({
  id,
  label,
  buildingLabel: "Other",
  statusLabel: "In another building",
  disabled: true,
  reassignment: true,
  minerCount,
});

describe("computeRackSelectionDelta", () => {
  it("returns empty delta when nothing changed", () => {
    const items = [eligible("1"), eligible("2")];
    const out = computeRackSelectionDelta(items, [1n, 2n], ["1", "2"]);
    expect(out.added).toEqual([]);
    expect(out.removed).toEqual([]);
    expect(out.reassigned).toEqual([]);
  });

  it("classifies newly-checked ids as added with labels", () => {
    const items = [eligible("1"), eligible("2", "Rack-2")];
    const out = computeRackSelectionDelta(items, [1n], ["1", "2"]);
    expect(out.added).toEqual([{ rackId: 2n, label: "Rack-2" }]);
    expect(out.removed).toEqual([]);
    expect(out.reassigned).toEqual([]);
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

  it("adds a selected reassignment row and reports it in reassigned with its miner count", () => {
    // With the toggle on the operator can select an already-placed rack. It is
    // a legitimate add AND surfaces in `reassigned` so the host prompts the
    // reparent confirm before committing.
    const items = [eligible("1"), reassignmentItem("2", 5)];
    const out = computeRackSelectionDelta(items, [], ["1", "2"]);
    expect(out.added).toEqual([
      { rackId: 1n, label: "R-1" },
      { rackId: 2n, label: "R-2" },
    ]);
    expect(out.reassigned).toEqual([{ rackId: 2n, label: "R-2", minerCount: 5 }]);
  });

  it("mixed delta: one add + one remove + one untouched-missing", () => {
    const items = [eligible("1"), eligible("3"), eligible("4")];
    const out = computeRackSelectionDelta(items, [1n, 3n, 99n], ["1", "4"]);
    expect(out.added).toEqual([{ rackId: 4n, label: "R-4" }]);
    expect(out.removed).toEqual([3n]); // 99n stays — missing from items
    expect(out.reassigned).toEqual([]);
  });
});
