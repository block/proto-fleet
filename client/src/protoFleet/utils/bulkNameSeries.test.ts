import { describe, expect, it } from "vitest";

import { buildBulkNameSeries, formatBulkCounter, overlongNameIndexes } from "./bulkNameSeries";

describe("formatBulkCounter", () => {
  it("treats scale as a zero-pad width, matching bulk rename", () => {
    expect(formatBulkCounter(1, 1)).toBe("1");
    expect(formatBulkCounter(1, 3)).toBe("001");
    expect(formatBulkCounter(42, 4)).toBe("0042");
  });

  it("leaves a number wider than the scale intact rather than truncating it", () => {
    expect(formatBulkCounter(1000, 2)).toBe("1000");
  });

  it("clamps an out-of-range scale into the supported 1-6 window", () => {
    expect(formatBulkCounter(7, 0)).toBe("7");
    expect(formatBulkCounter(7, 99)).toBe("000007");
  });
});

describe("overlongNameIndexes", () => {
  it("measures the finished name, counter included", () => {
    // The prefix fits on its own; the zero-padding is what puts it over.
    const names = buildBulkNameSeries(2, { namePrefix: "x".repeat(97), counterStart: 1, counterScale: 6 }, 500);
    expect(names.map((n) => n.length)).toEqual([103, 103]);
    expect(overlongNameIndexes(names, 100)).toEqual([0, 1]);
  });

  it("flags only the rows that cross the cap, so a widening counter shows up mid-run", () => {
    // 9 → 10 widens the counter past the padding, so the run straddles the cap.
    const names = buildBulkNameSeries(3, { namePrefix: "y".repeat(9), counterStart: 9, counterScale: 1 }, 500);
    expect(names).toEqual(["yyyyyyyyy9", "yyyyyyyyy10", "yyyyyyyyy11"]);
    expect(overlongNameIndexes(names, 10)).toEqual([1, 2]);
  });

  it("returns nothing when every name fits", () => {
    expect(overlongNameIndexes(["A-1", "A-2"], 100)).toEqual([]);
  });
});
