import { describe, expect, test } from "vitest";

import { getAsicsRows, sortAsics } from "./utility";
import { AsicStats } from "@/protoOS/api/types";

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
