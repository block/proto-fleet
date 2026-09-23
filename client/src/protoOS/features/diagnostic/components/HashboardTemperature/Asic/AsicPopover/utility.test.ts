import { describe, expect, test } from "vitest";

import {
  convertAndFormatTemperature,
  convertTelemetryHashrateToChartData,
  convertTelemetryTemperatureToChartData,
} from "./utility";

describe("convertAndFormatTemperature", () => {
  test("formats Celsius with space before degree sign", () => {
    expect(convertAndFormatTemperature(65.2, "C")).toBe("65.2 °C");
  });

  test("converts and formats Fahrenheit with space before degree sign", () => {
    expect(convertAndFormatTemperature(100, "F")).toBe("212.0 °F");
  });

  test("returns N/A for zero temperature", () => {
    expect(convertAndFormatTemperature(0, "C")).toBe("N/A");
  });

  test("returns N/A for null temperature", () => {
    expect(convertAndFormatTemperature(null, "C")).toBe("N/A");
  });

  test("returns N/A for undefined temperature", () => {
    expect(convertAndFormatTemperature(undefined, "C")).toBe("N/A");
  });

  test("hides unit when showUnits is false", () => {
    expect(convertAndFormatTemperature(65.2, "C", false)).toBe("65.2");
  });

  test("uses correct degree sign (U+00B0), not ordinal indicator", () => {
    const result = convertAndFormatTemperature(50, "C");
    expect(result).toContain("°"); // U+00B0
    expect(result).not.toContain("º"); // not U+00BA
  });
});

describe("convertTelemetryTemperatureToChartData", () => {
  const metric = {
    units: "°C" as const,
    points: [
      { timestampMs: 1_000, value: 60 },
      { timestampMs: 61_000, value: 61 },
    ],
  };

  test("uses the recorded timestamps in seconds", () => {
    expect(convertTelemetryTemperatureToChartData({ ...metric, bucketSizeMs: 60_000 })).toEqual([
      { datetime: 1, value: 60 },
      { datetime: 61, value: 61 },
    ]);
  });

  test("uses recorded timestamps when the bucket size is absent", () => {
    expect(convertTelemetryTemperatureToChartData(metric)).toEqual([
      { datetime: 1, value: 60 },
      { datetime: 61, value: 61 },
    ]);
  });
});

describe.each([
  { name: "temperature", convert: convertTelemetryTemperatureToChartData, value: 60, expectedValue: 60 },
  { name: "hashrate", convert: convertTelemetryHashrateToChartData, value: 2_000_000, expectedValue: 2 },
])("$name chart timestamps", ({ convert, value, expectedValue }) => {
  test("sorts sparse readings by their actual timestamps without changing the input", () => {
    const points = [
      { timestampMs: 181_000, value },
      { timestampMs: 1_000, value: 0 },
      { timestampMs: 61_000, value: null },
    ];
    const original = points.map((point) => ({ ...point }));

    expect(convert({ points, bucketSizeMs: 60_000 })).toEqual([
      { datetime: 1, value: 0 },
      { datetime: 61, value: null },
      { datetime: 181, value: expectedValue },
    ]);
    expect(points).toEqual(original);
  });

  test("handles empty history", () => {
    expect(convert({ points: [] })).toEqual([]);
  });
});
