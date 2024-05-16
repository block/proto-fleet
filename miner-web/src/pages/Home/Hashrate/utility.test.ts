import { describe, expect, test } from "vitest";

import { convertAggregateValues, convertHashrateValues } from "./utility";

// since epoch gets converted to local timestamp, check general format rather than exact time
const expectedTimestamp = new RegExp(/^[0-9]{2}:[0-9]{2}:[0-9]{2}$/);

describe("convertHashrateValues", () => {
  test("should convert MH/s to TH/s, shorten to two decimal places, remove trailing zeros, and convert epoch to timestamp format", () => {
    const data = [
      {
        datetime: 1715617336,
        value: 4069070.0,
      },
      {
        datetime: 1715617396,
        value: 2103689.0,
      },
    ];
    const result = convertHashrateValues(data) || [];
    expect(result[0].datetime).toMatch(expectedTimestamp);
    expect(result[1].datetime).toMatch(expectedTimestamp);
    expect(result[0].value).toEqual(4.07);
    expect(result[1].value).toEqual(2.1);
  });
});

describe("convertAggregateValues", () => {
  test("should convert MH/s to TH/s, shorten to two decimal places, and remove trailing zeros", () => {
    const data = {
      avg: 2335186.344262295,
      max: 4115689.0,
      min: 1601674.0,
    };
    const result = convertAggregateValues(data) || [];
    expect(result).toEqual({
      avg: 2.34,
      max: 4.12,
      min: 1.6,
    });
  });
});
