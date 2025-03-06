import { beforeAll, describe, expect, test } from "vitest";

import { aggregatePowerValues } from "./utility";
import { TimeSeriesData } from "@/protoOS/api/types";

describe("aggregatePowerValues", () => {
  const data: TimeSeriesData[] = [];

  beforeAll(() => {
    for (let i = 0; i < 360; i++) {
      // 11:37:20
      data.push({ datetime: 1718969840 + i * 60, value: 10 + i * 0.01 });
    }
  });

  test("should return undefined if data is undefined", () => {
    const result = aggregatePowerValues(undefined, "12h");
    expect(result).toBeUndefined();
  });

  test("should return the same value if only one value", () => {
    const oneItemData = data.slice(-1);
    const result = aggregatePowerValues(oneItemData, "12h");
    expect(result).toHaveLength(1);
    expect(result).toEqual(oneItemData);
  });

  describe("with less than 100 data points, should return the same data", () => {
    let oneItemFromThresholdData: TimeSeriesData[] = [];

    beforeAll(() => {
      oneItemFromThresholdData = data.slice(-99);
    });

    test("for 12 hours duration", () => {
      const result = aggregatePowerValues(oneItemFromThresholdData, "12h");
      expect(result).toEqual(oneItemFromThresholdData);
    });
    test("for 24 hours duration", () => {
      const result = aggregatePowerValues(oneItemFromThresholdData, "24h");
      expect(result).toEqual(oneItemFromThresholdData);
    });
    test("for 48 hours duration", () => {
      const result = aggregatePowerValues(oneItemFromThresholdData, "48h");
      expect(result).toEqual(oneItemFromThresholdData);
    });
    test("for 5 days duration", () => {
      const result = aggregatePowerValues(oneItemFromThresholdData, "5d");
      expect(result).toEqual(oneItemFromThresholdData);
    });
  });

  describe("with less than 250 data points, should return 10 minute aggregates", () => {
    let oneItemFromThresholdData: TimeSeriesData[] = [];

    beforeAll(() => {
      oneItemFromThresholdData = data.slice(-249);
    });

    test("for 12 hours, 24 hours, 48 hours, 5 days duration", () => {
      const result12h = aggregatePowerValues(oneItemFromThresholdData, "12h");
      const result24h = aggregatePowerValues(oneItemFromThresholdData, "24h");
      const result48h = aggregatePowerValues(oneItemFromThresholdData, "48h");
      const result5d = aggregatePowerValues(oneItemFromThresholdData, "5d");
      expect(result12h).toHaveLength(25);
      expect(result12h).toEqual(
        expect.arrayContaining([
          // 1:28:20
          { datetime: 1718976500, value: 11.155 },
          // 1:38:20
          { datetime: 1718977100, value: 11.254999999999999 },
        ]),
      );
      expect(result24h).toEqual(result12h);
      expect(result48h).toEqual(result12h);
      expect(result5d).toEqual(result12h);
    });
  });

  describe("with less than 360 data points, should return 20 minute aggregates", () => {
    let oneItemFromThresholdData: TimeSeriesData[] = [];

    beforeAll(() => {
      oneItemFromThresholdData = data.slice(-359);
    });

    test("for 12 hours, 24 hours, 48 hours, 5 days duration", () => {
      const result12h = aggregatePowerValues(oneItemFromThresholdData, "12h");
      const result24h = aggregatePowerValues(oneItemFromThresholdData, "24h");
      const result48h = aggregatePowerValues(oneItemFromThresholdData, "48h");
      const result5d = aggregatePowerValues(oneItemFromThresholdData, "5d");
      expect(result12h).toHaveLength(18);
      expect(result12h).toEqual(
        expect.arrayContaining([
          // 11:38:20
          { datetime: 1718969900, value: 10.105 },
          // 1:38:20
          { datetime: 1718971100, value: 10.305 },
        ]),
      );
      expect(result24h).toEqual(result12h);
      expect(result48h).toEqual(result12h);
      expect(result5d).toEqual(result12h);
    });
  });

  describe("with 360 data points and more, should aggregate the data", () => {
    test("should return 30 minute aggregated values for 12 hours duration", () => {
      const result = aggregatePowerValues(data, "12h");
      expect(result).toHaveLength(12);
      expect(result).toEqual(
        expect.arrayContaining([
          // 11:37:20
          { datetime: 1718969840, value: 10.145 },
          // 12:07:20
          { datetime: 1718971640, value: 10.444999999999999 },
        ]),
      );
    });

    test("should return 60 minute aggregated values for 24 hours duration", () => {
      const result = aggregatePowerValues(data, "24h");
      expect(result).toHaveLength(6);
      expect(result).toEqual(
        expect.arrayContaining([
          // 11:37:20
          { datetime: 1718969840, value: 10.294999999999998 },
          // 12:37:20
          { datetime: 1718973440, value: 10.895000000000001 },
        ]),
      );
    });

    test("should return 120 minute aggregated values for 48 hours duration", () => {
      const result = aggregatePowerValues(data, "48h");
      expect(result).toHaveLength(3);
      expect(result).toEqual(
        expect.arrayContaining([
          // 11:37:20
          { datetime: 1718969840, value: 10.594999999999999 },
          // 13:37:20
          { datetime: 1718977040, value: 11.794999999999996 },
        ]),
      );
    });

    test("should return 300 minute aggregated values for 5 days duration", () => {
      const result = aggregatePowerValues(data, "5d");
      expect(result).toHaveLength(2);
      expect(result).toEqual([
        // 11:37:20
        { datetime: 1718969840, value: 11.494999999999996 },
        // 16:37:20
        { datetime: 1718987840, value: 13.294999999999998 },
      ]);
    });
  });
});
