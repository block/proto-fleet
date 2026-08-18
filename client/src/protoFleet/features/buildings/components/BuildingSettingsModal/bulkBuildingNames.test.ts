import { describe, expect, it } from "vitest";

import { buildBulkBuildingNames, bulkBuildingCountMaximum, takenNameIndexes } from "./bulkBuildingNames";

// The zero-pad math itself is covered in utils/bulkNameSeries.test.ts; these
// exercise it through the building form's wrapper (its cap, its prefix rules).
describe("buildBulkBuildingNames", () => {
  it("counts up from the start value", () => {
    expect(buildBulkBuildingNames(3, { namePrefix: "Bldg-", counterStart: 5, counterScale: 2 })).toEqual([
      "Bldg-05",
      "Bldg-06",
      "Bldg-07",
    ]);
  });

  it("allows an empty prefix, leaving bare counters", () => {
    expect(buildBulkBuildingNames(2, { namePrefix: "", counterStart: 1, counterScale: 1 })).toEqual(["1", "2"]);
  });

  it("keeps the prefix exactly as typed, trailing space included", () => {
    // "Building - " is a legitimate prefix: the space is the separator, so
    // trimming it would silently produce "Building -1".
    expect(buildBulkBuildingNames(1, { namePrefix: "Building - ", counterStart: 1, counterScale: 1 })).toEqual([
      "Building - 1",
    ]);
  });

  it("returns nothing for a zero or negative count", () => {
    const options = { namePrefix: "B", counterStart: 1, counterScale: 1 };
    expect(buildBulkBuildingNames(0, options)).toEqual([]);
    expect(buildBulkBuildingNames(-5, options)).toEqual([]);
  });

  it("never repeats a name, even across the pad-width boundary", () => {
    // Why the form has no in-batch duplicate pre-check: a shared prefix plus a
    // strictly incrementing counter can't collide, so the server's
    // DUPLICATE_NAME_IN_BATCH reason is unreachable from this form.
    const names = buildBulkBuildingNames(120, { namePrefix: "B-", counterStart: 95, counterScale: 2 });
    expect(new Set(names).size).toBe(names.length);
  });

  it("caps at the server's batch limit", () => {
    const names = buildBulkBuildingNames(10_000, { namePrefix: "B", counterStart: 1, counterScale: 1 });
    expect(names).toHaveLength(bulkBuildingCountMaximum);
  });
});

describe("takenNameIndexes", () => {
  it("flags rows whose name already exists at the site", () => {
    expect(takenNameIndexes(["A-1", "A-2", "A-3"], ["A-2"])).toEqual([1]);
  });

  it("compares on the trimmed value, since that is what the server stores", () => {
    expect(takenNameIndexes(["A-1"], ["  A-1  "])).toEqual([0]);
  });
});
