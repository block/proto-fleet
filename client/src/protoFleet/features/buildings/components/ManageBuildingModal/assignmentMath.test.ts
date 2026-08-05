import { describe, expect, it } from "vitest";

import {
  type AssignmentEntry,
  buildByNameAssignments,
  buildManualAssignments,
  buildPlacementDelta,
  isPlacementDeltaEmpty,
} from "./assignmentMath";
import { cellKey, type GridCellKey } from "./types";

const entry = (id: bigint, label: string, aisle?: number, position?: number): AssignmentEntry => ({
  rackId: id,
  label,
  aisleIndex: aisle,
  positionInAisle: position,
});

describe("buildByNameAssignments", () => {
  it("returns empty when aisles or racksPerAisle is 0", () => {
    expect(buildByNameAssignments([entry(1n, "A")], 0, 5)).toEqual({});
    expect(buildByNameAssignments([entry(1n, "A")], 5, 0)).toEqual({});
  });

  it("fills cells row-major (aisle 0 first) in alphabetical order", () => {
    const result = buildByNameAssignments(
      [entry(3n, "Charlie"), entry(1n, "Alpha"), entry(2n, "Bravo")],
      // 2 aisles × 2 racks per aisle = 4 cells; we have 3 racks.
      2,
      2,
    );
    expect(result[cellKey(0, 0)]).toBe(1n); // Alpha
    expect(result[cellKey(0, 1)]).toBe(2n); // Bravo
    expect(result[cellKey(1, 0)]).toBe(3n); // Charlie
    expect(result[cellKey(1, 1)]).toBeUndefined();
    expect(Object.keys(result).length).toBe(3);
  });

  it("stops at capacity when more racks than cells", () => {
    const result = buildByNameAssignments(
      [entry(1n, "A"), entry(2n, "B"), entry(3n, "C")],
      // 1 × 2 = 2 cells.
      1,
      2,
    );
    expect(Object.keys(result).length).toBe(2);
    expect(result[cellKey(0, 0)]).toBe(1n);
    expect(result[cellKey(0, 1)]).toBe(2n);
  });

  it("ignores existing manual positions on entries (byName recomputes)", () => {
    const result = buildByNameAssignments([entry(1n, "Z", 4, 4), entry(2n, "A", 9, 9)], 2, 2);
    // Sort by label puts "A" first; alphabetical mapping wins, not the
    // pre-set positions.
    expect(result[cellKey(0, 0)]).toBe(2n);
    expect(result[cellKey(0, 1)]).toBe(1n);
  });
});

describe("buildManualAssignments", () => {
  it("includes only entries with both position fields set", () => {
    const result = buildManualAssignments(
      [
        entry(1n, "A", 0, 0),
        entry(2n, "B"), // no position
        entry(3n, "C", 1, 1),
      ],
      3,
      3,
    );
    expect(Object.keys(result).length).toBe(2);
    expect(result[cellKey(0, 0)]).toBe(1n);
    expect(result[cellKey(1, 1)]).toBe(3n);
  });

  it("drops out-of-bounds positions (shrunken layout)", () => {
    // Entry at (3, 0) is outside a 2×2 grid; silently excluded.
    const result = buildManualAssignments([entry(1n, "A", 0, 0), entry(2n, "B", 3, 0)], 2, 2);
    expect(Object.keys(result).length).toBe(1);
    expect(result[cellKey(0, 0)]).toBe(1n);
  });

  it("drops negative coordinates", () => {
    const result = buildManualAssignments([entry(1n, "A", -1, 0), entry(2n, "B", 0, -1)], 3, 3);
    expect(result).toEqual({});
  });

  it("returns empty when grid is 0×N or N×0", () => {
    expect(buildManualAssignments([entry(1n, "A", 0, 0)], 0, 3)).toEqual({});
    expect(buildManualAssignments([entry(1n, "A", 0, 0)], 3, 0)).toEqual({});
  });
});

describe("buildPlacementDelta", () => {
  const cells = (pairs: [bigint, number, number][]): Map<string, GridCellKey> =>
    new Map(pairs.map(([id, aisle, position]) => [id.toString(), cellKey(aisle, position)]));

  it("is empty when the working set matches the snapshot", () => {
    const delta = buildPlacementDelta(
      [entry(1n, "A", 0, 0), entry(2n, "B")],
      cells([[1n, 0, 0]]),
      new Map([
        ["1", "0:0"],
        ["2", "unplaced"],
      ]),
    );
    expect(delta).toEqual({ unassign: [], inBuildingVacate: [], inBuildingPlace: [] });
    expect(isPlacementDeltaEmpty(delta)).toBe(true);
  });

  it("sends a member-only assign for a rack added but never placed", () => {
    // Racks added via Manage racks and never dragged to a cell must still
    // link to the building, or they silently drop on save.
    const delta = buildPlacementDelta([entry(9n, "New")], cells([]), new Map());
    expect(delta.inBuildingVacate).toEqual([{ rackId: 9n }]);
    expect(delta.inBuildingPlace).toEqual([]);
    expect(isPlacementDeltaEmpty(delta)).toBe(false);
  });

  it("splits a mover into a pre-place vacate plus a place", () => {
    const delta = buildPlacementDelta([entry(1n, "A", 1, 2)], cells([[1n, 1, 2]]), new Map([["1", "0:0"]]));
    expect(delta.inBuildingVacate).toEqual([{ rackId: 1n }]);
    expect(delta.inBuildingPlace).toEqual([{ rackId: 1n, aisleIndex: 1, positionInAisle: 2 }]);
  });

  it("emits no pre-place vacate for a first-time placement", () => {
    const delta = buildPlacementDelta([entry(1n, "A", 0, 1)], cells([[1n, 0, 1]]), new Map([["1", "unplaced"]]));
    expect(delta.inBuildingVacate).toEqual([]);
    expect(delta.inBuildingPlace).toEqual([{ rackId: 1n, aisleIndex: 0, positionInAisle: 1 }]);
  });

  it("vacates in place when a placed rack loses its cell", () => {
    const delta = buildPlacementDelta([entry(1n, "A")], cells([]), new Map([["1", "0:0"]]));
    expect(delta.inBuildingVacate).toEqual([{ rackId: 1n }]);
    expect(delta.inBuildingPlace).toEqual([]);
  });

  it("unassigns racks dropped from the working set", () => {
    const delta = buildPlacementDelta(
      [entry(1n, "A", 0, 0)],
      cells([[1n, 0, 0]]),
      new Map([
        ["1", "0:0"],
        ["2", "0:1"],
      ]),
    );
    expect(delta.unassign).toEqual([{ rackId: 2n }]);
    expect(delta.inBuildingVacate).toEqual([]);
    expect(delta.inBuildingPlace).toEqual([]);
  });

  it("orders a swap so both old cells vacate before either place", () => {
    // A and B trade cells — the partial unique index would collide if a
    // place ran before the counterpart's old cell was cleared.
    const delta = buildPlacementDelta(
      [entry(1n, "A", 0, 1), entry(2n, "B", 0, 0)],
      cells([
        [1n, 0, 1],
        [2n, 0, 0],
      ]),
      new Map([
        ["1", "0:0"],
        ["2", "0:1"],
      ]),
    );
    expect(delta.inBuildingVacate).toEqual([{ rackId: 1n }, { rackId: 2n }]);
    expect(delta.inBuildingPlace).toEqual([
      { rackId: 1n, aisleIndex: 0, positionInAisle: 1 },
      { rackId: 2n, aisleIndex: 0, positionInAisle: 0 },
    ]);
  });
});
