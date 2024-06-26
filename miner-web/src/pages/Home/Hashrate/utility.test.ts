import { beforeAll, describe, expect, test } from "vitest";

import { TimeSeriesData } from "apiTypes";

import { getTimeFromEpoch } from "common/utils/stringUtils";

import {
  aggregateHashrateValues,
  convertAggregateValues,
  convertHashrateValues,
} from "./utility";

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
    expect(result[0].datetime).toEqual(data[0].datetime);
    expect(result[1].datetime).toEqual(data[1].datetime);
    expect(result[0].value).toEqual(4.06907);
    expect(result[1].value).toEqual(2.103689);
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

describe("aggregatePowerValues", () => {
  let hashrates: TimeSeriesData[] = [];
  let hashrates1: TimeSeriesData[] = [];
  let hashrates2: TimeSeriesData[] = [];
  let hashrates3: TimeSeriesData[] = [];

  beforeAll(() => {
    for (let i = 0; i < 360; i++) {
      // 11:37:20
      const datetime = 1718969840 + i * 60;
      const decimals = i * 0.01;
      hashrates.push({ datetime, value: 10 + decimals });
      hashrates1.push({
        datetime,
        value: 11 + decimals,
      });
      hashrates2.push({
        datetime,
        value: 12 + decimals,
      });
      hashrates3.push({
        datetime,
        value: 13 + decimals,
      });
    }
  });

  describe("with less than 360 data points, should return the same data", () => {
    const smallHashrates = hashrates.slice(-359);
    const smallHashrates1 = hashrates1.slice(-359);
    const smallHashrates2 = hashrates2.slice(-359);
    const smallHashrates3 = hashrates3.slice(-359);

    test("for 12 hours duration", () => {
      const result = aggregateHashrateValues(smallHashrates, "12h");
      const result1 = aggregateHashrateValues(smallHashrates1, "12h");
      const result2 = aggregateHashrateValues(smallHashrates2, "12h");
      const result3 = aggregateHashrateValues(smallHashrates3, "12h");
      expect(result).toEqual(smallHashrates);
      expect(result1).toEqual(smallHashrates1);
      expect(result2).toEqual(smallHashrates2);
      expect(result3).toEqual(smallHashrates3);
    });
    test("for 24 hours duration", () => {
      const result = aggregateHashrateValues(smallHashrates, "24h");
      const result1 = aggregateHashrateValues(smallHashrates1, "24h");
      const result2 = aggregateHashrateValues(smallHashrates2, "24h");
      const result3 = aggregateHashrateValues(smallHashrates3, "24h");
      expect(result).toEqual(smallHashrates);
      expect(result1).toEqual(smallHashrates1);
      expect(result2).toEqual(smallHashrates2);
      expect(result3).toEqual(smallHashrates3);
    });
    test("for 48 hours duration", () => {
      const result = aggregateHashrateValues(smallHashrates, "48h");
      const result1 = aggregateHashrateValues(smallHashrates1, "48h");
      const result2 = aggregateHashrateValues(smallHashrates2, "48h");
      const result3 = aggregateHashrateValues(smallHashrates3, "48h");
      expect(result).toEqual(smallHashrates);
      expect(result1).toEqual(smallHashrates1);
      expect(result2).toEqual(smallHashrates2);
      expect(result3).toEqual(smallHashrates3);
    });
    test("for 5 days duration", () => {
      const result = aggregateHashrateValues(smallHashrates, "5d");
      const result1 = aggregateHashrateValues(smallHashrates1, "5d");
      const result2 = aggregateHashrateValues(smallHashrates2, "5d");
      const result3 = aggregateHashrateValues(smallHashrates3, "5d");
      expect(result).toEqual(smallHashrates);
      expect(result1).toEqual(smallHashrates1);
      expect(result2).toEqual(smallHashrates2);
      expect(result3).toEqual(smallHashrates3);
    });
  });

  describe("with 360 data points and more", () => {
    describe("should return the same data", () => {
      test("for 12 hours duration", () => {
        const result = aggregateHashrateValues(hashrates, "12h");
        const result1 = aggregateHashrateValues(hashrates1, "12h");
        const result2 = aggregateHashrateValues(hashrates2, "12h");
        const result3 = aggregateHashrateValues(hashrates3, "12h");
        expect(result).toEqual(hashrates);
        expect(result1).toEqual(hashrates1);
        expect(result2).toEqual(hashrates2);
        expect(result3).toEqual(hashrates3);
      });

      test("for 24 hours duration", () => {
        const result = aggregateHashrateValues(hashrates, "24h");
        const result1 = aggregateHashrateValues(hashrates1, "24h");
        const result2 = aggregateHashrateValues(hashrates2, "24h");
        const result3 = aggregateHashrateValues(hashrates3, "24h");
        expect(result).toEqual(hashrates);
        expect(result1).toEqual(hashrates1);
        expect(result2).toEqual(hashrates2);
        expect(result3).toEqual(hashrates3);
      });
    });

    describe("should aggregate the data", () => {
      test("should return 10 minute aggregated values for 48 hours duration", () => {
        const result = aggregateHashrateValues(hashrates, "48h");
        const result1 = aggregateHashrateValues(hashrates1, "48h");
        const result2 = aggregateHashrateValues(hashrates2, "48h");
        const result3 = aggregateHashrateValues(hashrates3, "48h");
        expect(result).toHaveLength(36);
        const mismatchedTime = result?.find(
          (r) =>
            !result1?.find(
              (r1) =>
                getTimeFromEpoch(r1.datetime).slice(0, -3) ===
                getTimeFromEpoch(r.datetime).slice(0, -3)
            )
        );
        expect(mismatchedTime).toBeUndefined();
        const mismatchedTime1 = result1?.find(
          (r1) =>
            !result2?.find(
              (r2) =>
                getTimeFromEpoch(r2.datetime).slice(0, -3) ===
                getTimeFromEpoch(r1.datetime).slice(0, -3)
            )
        );
        expect(mismatchedTime1).toBeUndefined();
        const mismatchedTime2 = result2?.find(
          (r1) =>
            !result3?.find(
              (r2) =>
                getTimeFromEpoch(r2.datetime).slice(0, -3) ===
                getTimeFromEpoch(r1.datetime).slice(0, -3)
            )
        );
        expect(mismatchedTime2).toBeUndefined();
      });

      test("should return 60 minute aggregated values for 5 days duration", () => {
        const result = aggregateHashrateValues(hashrates, "5d");
        const result1 = aggregateHashrateValues(hashrates1, "5d");
        const result2 = aggregateHashrateValues(hashrates2, "5d");
        const result3 = aggregateHashrateValues(hashrates3, "5d");
        expect(result).toHaveLength(6);
        const mismatchedTime = result?.find(
          (r) =>
            !result1?.find(
              (r1) =>
                getTimeFromEpoch(r1.datetime).slice(0, -3) ===
                getTimeFromEpoch(r.datetime).slice(0, -3)
            )
        );
        expect(mismatchedTime).toBeUndefined();
        const mismatchedTime1 = result1?.find(
          (r1) =>
            !result2?.find(
              (r2) =>
                getTimeFromEpoch(r2.datetime).slice(0, -3) ===
                getTimeFromEpoch(r1.datetime).slice(0, -3)
            )
        );
        expect(mismatchedTime1).toBeUndefined();
        const mismatchedTime2 = result2?.find(
          (r1) =>
            !result3?.find(
              (r2) =>
                getTimeFromEpoch(r2.datetime).slice(0, -3) ===
                getTimeFromEpoch(r1.datetime).slice(0, -3)
            )
        );
        expect(mismatchedTime2).toBeUndefined();
      });
    });
  });
});
