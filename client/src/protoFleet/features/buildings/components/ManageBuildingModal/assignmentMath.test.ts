import { describe, expect, it } from "vitest";

import { type AssignmentEntry, buildByNameAssignments, buildManualAssignments } from "./assignmentMath";
import { cellKey } from "./types";

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
