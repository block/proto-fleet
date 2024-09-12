import { describe, expect, test } from "vitest";

import { AsicStats } from "apiTypes";

import { getAsicsRows, getRowLabel, sortAsics } from "./utility";

describe("sortAsics", () => {
  test("should sort the asics array in ascending order based on row then column", () => {
    const asics: AsicStats[] = [
      { row: 1, column: 1, temp_c: 51.1 },
      { row: 0, column: 1, temp_c: 50.1 },
      { row: 0, column: 0, temp_c: 50.0 },
    ];

    const sortedAsics = sortAsics(asics);

    expect(sortedAsics).toEqual([
      { row: 0, column: 0, temp_c: 50.0 },
      { row: 0, column: 1, temp_c: 50.1 },
      { row: 1, column: 1, temp_c: 51.1 },
    ]);
  });

  test("should return an empty array if the input asics array is empty", () => {
    const asics: AsicStats[] = [];

    const sortedAsics = sortAsics(asics);

    expect(sortedAsics).toEqual([]);
  });
});

describe("getAsicsRows", () => {
  test("should return the unique rows from the asics", () => {
    const asics: AsicStats[] = [
      { row: 1, column: 1, temp_c: 51.1 },
      { row: 0, column: 1, temp_c: 50.1 },
      { row: 0, column: 0, temp_c: 50.0 },
    ];

    const rows = getAsicsRows(sortAsics(asics));

    expect(rows).toEqual([0, 1]);
  });

  test("should return an empty array if the input asics array is empty", () => {
    const asics: AsicStats[] = [];

    const rows = getAsicsRows(asics);

    expect(rows).toEqual([]);
  });
});

describe("getRowLabel", () => {
  test("should return the alphabet character for the given row number", () => {
    expect(getRowLabel(0)).toBe("A");
    expect(getRowLabel(1)).toBe("B");
    expect(getRowLabel(2)).toBe("C");
    expect(getRowLabel(3)).toBe("D");
    expect(getRowLabel(4)).toBe("E");
    expect(getRowLabel(5)).toBe("F");
    expect(getRowLabel(6)).toBe("G");
    expect(getRowLabel(7)).toBe("H");
    expect(getRowLabel(8)).toBe("I");
    expect(getRowLabel(9)).toBe("J");
  });
});
