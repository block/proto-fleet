import { beforeAll, describe, expect, test } from "vitest";

import { aggregateEfficiencyValues } from "./utility";
import { TimeSeriesData } from "@/protoOS/api/types";

describe("aggregateEfficiencyValues", () => {
  const data: TimeSeriesData[] = [];

  beforeAll(() => {
    for (let i = 0; i < 360; i++) {
      // 11:37:20
      data.push({ datetime: 1718969840 + i * 60, value: 10 + i * 0.01 });
    }
  });

  test("should return undefined if data is undefined", () => {
    const result = aggregateEfficiencyValues(undefined, "12h");
    expect(result).toBeUndefined();
  });

  test("should return the same value if only one value", () => {
    const oneItemData = data.slice(-1);
    const result = aggregateEfficiencyValues(oneItemData, "12h");
    expect(result).toHaveLength(1);
    expect(result).toEqual(oneItemData);
  });

  describe("with less than 360 data points, should return the same data", () => {
    let oneItemFromThresholdData: TimeSeriesData[] = [];

    beforeAll(() => {
      oneItemFromThresholdData = data.slice(-359);
    });

    test("for 12 hours duration", () => {
      const result = aggregateEfficiencyValues(oneItemFromThresholdData, "12h");
      expect(result).toEqual(oneItemFromThresholdData);
    });
    test("for 24 hours duration", () => {
      const result = aggregateEfficiencyValues(oneItemFromThresholdData, "24h");
      expect(result).toEqual(oneItemFromThresholdData);
    });
    test("for 48 hours duration", () => {
      const result = aggregateEfficiencyValues(oneItemFromThresholdData, "48h");
      expect(result).toEqual(oneItemFromThresholdData);
    });
    test("for 5 days duration", () => {
      const result = aggregateEfficiencyValues(oneItemFromThresholdData, "5d");
      expect(result).toEqual(oneItemFromThresholdData);
    });
  });

  describe("with 360 data points and more, should aggregate the data", () => {
    test("should return 5 minute aggregated values for 12 hours duration", () => {
      const result = aggregateEfficiencyValues(data, "12h");
      expect(result).toHaveLength(72);
      expect(result).toEqual(
        expect.arrayContaining([
          // 11:37:20
          { datetime: 1718969840, value: 10.02 },
          // 11:42:20
          { datetime: 1718970140, value: 10.069999999999999 },
        ]),
      );
    });

    test("should return 10 minute aggregated values for 24 hours duration", () => {
      const result = aggregateEfficiencyValues(data, "24h");
      expect(result).toHaveLength(36);
      expect(result).toEqual(
        expect.arrayContaining([
          // 11:37:20
          { datetime: 1718969840, value: 10.045 },
          // 11:47:20
          { datetime: 1718970440, value: 10.145 },
        ]),
      );
    });

    test("should return 20 minute aggregated values for 48 hours duration", () => {
      const result = aggregateEfficiencyValues(data, "48h");
      expect(result).toHaveLength(18);
      expect(result).toEqual(
        expect.arrayContaining([
          // 11:37:20
          { datetime: 1718969840, value: 10.095 },
          // 11:57:20
          { datetime: 1718971040, value: 10.294999999999998 },
        ]),
      );
    });

    test("should return 180 minute aggregated values for 5 days duration", () => {
      const result = aggregateEfficiencyValues(data, "5d");
      expect(result).toHaveLength(2);
      expect(result).toEqual([
        // 11:37:20
        { datetime: 1718969840, value: 10.895 },
        // 14:37:20
        { datetime: 1718980640, value: 12.695000000000002 },
      ]);
    });
  });
});
