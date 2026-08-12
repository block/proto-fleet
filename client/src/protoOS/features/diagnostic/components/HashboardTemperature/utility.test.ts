import { describe, expect, test } from "vitest";

import { groupAsicsByRow, sortAsics } from "./utility";
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

describe("groupAsicsByRow", () => {
  test("should group the asics into rows, in row order", () => {
    const asics: AsicData[] = [
      { id: "asic1", hashboardSerial: "hb1", row: 1, column: 1 },
      { id: "asic2", hashboardSerial: "hb1", row: 0, column: 1 },
      { id: "asic3", hashboardSerial: "hb1", row: 0, column: 0 },
    ];

    const rows = groupAsicsByRow(sortAsics(asics));

    expect(rows.map(({ row }) => row)).toEqual([0, 1]);
    expect(rows[0].asics.map(({ id }) => id)).toEqual(["asic3", "asic2"]);
    expect(rows[1].asics.map(({ id }) => id)).toEqual(["asic1"]);
  });

  test("should skip rows where no asic carries full position data", () => {
    const asics: AsicData[] = [
      { id: "asic1", hashboardSerial: "hb1", row: 0, column: 0 },
      { id: "asic2", hashboardSerial: "hb1", row: 1 },
    ];

    const rows = groupAsicsByRow(asics);

    expect(rows.map(({ row }) => row)).toEqual([0]);
  });

  test("should keep asics missing a column on a row that is otherwise positioned", () => {
    const asics: AsicData[] = [
      { id: "asic1", hashboardSerial: "hb1", row: 0, column: 0 },
      { id: "asic2", hashboardSerial: "hb1", row: 0 },
    ];

    const rows = groupAsicsByRow(asics);

    expect(rows[0].asics.map(({ id }) => id)).toEqual(["asic1", "asic2"]);
  });

  test("should return an empty array if the input asics array is empty", () => {
    const asics: AsicData[] = [];

    const rows = groupAsicsByRow(asics);

    expect(rows).toEqual([]);
  });
});
