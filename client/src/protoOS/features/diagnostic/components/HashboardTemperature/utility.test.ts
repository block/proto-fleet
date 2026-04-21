import { describe, expect, test } from "vitest";

import { getAsicsRows, sortAsics } from "./utility";
import { AsicData } from "@/protoOS/store";

describe("sortAsics", () => {
  test("should sort the asics array in ascending order based on row then column", () => {
    const asics: AsicData[] = [
      { id: "asic1", hashboardSerial: "hb1", row: 1, column: 1 },
      { id: "asic2", hashboardSerial: "hb1", row: 0, column: 1 },
      { id: "asic3", hashboardSerial: "hb1", row: 0, column: 0 },
    ];

    const sortedAsics = sortAsics(asics);

    expect(sortedAsics).toEqual([
      { id: "asic3", hashboardSerial: "hb1", row: 0, column: 0 },
      { id: "asic2", hashboardSerial: "hb1", row: 0, column: 1 },
      { id: "asic1", hashboardSerial: "hb1", row: 1, column: 1 },
    ]);
  });

  test("should return an empty array if the input asics array is empty", () => {
    const asics: AsicData[] = [];

    const sortedAsics = sortAsics(asics);

    expect(sortedAsics).toEqual([]);
  });
});

describe("getAsicsRows", () => {
  test("should return the unique rows from the asics", () => {
    const asics: AsicData[] = [
      { id: "asic1", hashboardSerial: "hb1", row: 1, column: 1 },
      { id: "asic2", hashboardSerial: "hb1", row: 0, column: 1 },
      { id: "asic3", hashboardSerial: "hb1", row: 0, column: 0 },
    ];

    const rows = getAsicsRows(sortAsics(asics));

    expect(rows).toEqual([0, 1]);
  });

  test("should return an empty array if the input asics array is empty", () => {
    const asics: AsicData[] = [];

    const rows = getAsicsRows(asics);

    expect(rows).toEqual([]);
  });
});
