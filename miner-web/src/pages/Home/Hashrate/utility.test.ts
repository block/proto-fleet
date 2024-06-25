import { describe, expect, test } from "vitest";

import {
  mockHashrateData1,
  mockHashrateData2,
  mockHashrateData3,
} from "./constants";
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
  describe("with less than 360 data points, should return the same data", () => {
    const hashrates1 = mockHashrateData1.data.slice(-359);
    const hashrates2 = mockHashrateData2.data.slice(-359);
    const hashrates3 = mockHashrateData3.data.slice(-359);

    test("for 12 hours duration", () => {
      const result1 = aggregateHashrateValues(hashrates1, "12h");
      const result2 = aggregateHashrateValues(hashrates2, "12h");
      const result3 = aggregateHashrateValues(hashrates3, "12h");
      expect(result1).toEqual(hashrates1);
      expect(result2).toEqual(hashrates2);
      expect(result3).toEqual(hashrates3);
    });
    test("for 24 hours duration", () => {
      const result1 = aggregateHashrateValues(hashrates1, "24h");
      const result2 = aggregateHashrateValues(hashrates2, "24h");
      const result3 = aggregateHashrateValues(hashrates3, "24h");
      expect(result1).toEqual(hashrates1);
      expect(result2).toEqual(hashrates2);
      expect(result3).toEqual(hashrates3);
    });
    test("for 48 hours duration", () => {
      const result1 = aggregateHashrateValues(hashrates1, "48h");
      const result2 = aggregateHashrateValues(hashrates2, "48h");
      const result3 = aggregateHashrateValues(hashrates3, "48h");
      expect(result1).toEqual(hashrates1);
      expect(result2).toEqual(hashrates2);
      expect(result3).toEqual(hashrates3);
    });
    test("for 5 days duration", () => {
      const result1 = aggregateHashrateValues(hashrates1, "5d");
      const result2 = aggregateHashrateValues(hashrates2, "5d");
      const result3 = aggregateHashrateValues(hashrates3, "5d");
      expect(result1).toEqual(hashrates1);
      expect(result2).toEqual(hashrates2);
      expect(result3).toEqual(hashrates3);
    });
  });

  describe("with 12 hours and 24 hours duration, should return the same data", () => {
    const hashrates1 = mockHashrateData1.data;
    const hashrates2 = mockHashrateData2.data;
    const hashrates3 = mockHashrateData3.data;

    test("for 12 hours duration", () => {
      const result1 = aggregateHashrateValues(hashrates1, "12h");
      const result2 = aggregateHashrateValues(hashrates2, "12h");
      const result3 = aggregateHashrateValues(hashrates3, "12h");
      expect(result1).toEqual(hashrates1);
      expect(result2).toEqual(hashrates2);
      expect(result3).toEqual(hashrates3);
    });

    test("for 24 hours duration", () => {
      const result1 = aggregateHashrateValues(hashrates1, "24h");
      const result2 = aggregateHashrateValues(hashrates2, "24h");
      const result3 = aggregateHashrateValues(hashrates3, "24h");
      expect(result1).toEqual(hashrates1);
      expect(result2).toEqual(hashrates2);
      expect(result3).toEqual(hashrates3);
    });
  });

  describe("with 360 data points and more, should aggregate the data for 48 hours and 5 days duration", () => {
    const hashrates1 = mockHashrateData1.data;
    const hashrates2 = mockHashrateData2.data;
    const hashrates3 = mockHashrateData3.data;

    test("should return 10 minute aggregated values for 48 hours duration", () => {
      const result1 = aggregateHashrateValues(hashrates1, "48h");
      const result2 = aggregateHashrateValues(hashrates2, "48h");
      const result3 = aggregateHashrateValues(hashrates3, "48h");
      expect(result1).toHaveLength(72);
      expect(result1).toEqual(
        expect.arrayContaining([
          // 4:13:02
          { datetime: 1719202382, value: 20751055.4 },
          // 4:23:06
          { datetime: 1719202986, value: 20781978.2 },
        ])
      );
      expect(result2).toEqual(
        expect.arrayContaining([
          // 4:13:02
          { datetime: 1719202382, value: 20697616.2 },
          // 4:23:06
          { datetime: 1719202986, value: 20697688.6 },
        ])
      );
      expect(result3).toEqual(
        expect.arrayContaining([
          // 4:13:02
          { datetime: 1719202382, value: 21237186.6 },
          // 4:23:06
          { datetime: 1719202986, value: 21270086.2 },
        ])
      );
    });

    test("should return 60 minute aggregated values for 5 days duration", () => {
      const result1 = aggregateHashrateValues(hashrates1, "5d");
      const result2 = aggregateHashrateValues(hashrates2, "5d");
      const result3 = aggregateHashrateValues(hashrates3, "5d");
      expect(result1).toHaveLength(12);
      expect(result1).toEqual(
        expect.arrayContaining([
          // 4:13:02
          { datetime: 1719202382, value: 20807676.6 },
          // 5:13:08
          { datetime: 1719205988, value: 19738770.7 },
        ])
      );
      expect(result2).toEqual(
        expect.arrayContaining([
          // 4:13:02
          { datetime: 1719202382, value: 20328653.7 },
          // 5:13:08
          { datetime: 1719205988, value: 20404523.1 },
        ])
      );
      expect(result3).toEqual(
        expect.arrayContaining([
          // 4:13:02
          { datetime: 1719202382, value: 21287905.666666668 },
          // 5:13:08
          { datetime: 1719205988, value: 21331857.5 },
        ])
      );
    });
  });
});
