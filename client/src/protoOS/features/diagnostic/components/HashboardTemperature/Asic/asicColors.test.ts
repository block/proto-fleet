import { describe, expect, it } from "vitest";
import { getMetricColor, getMetricRange } from "./asicColors";

const palette = {
  cool: "rgb(0 100 200)",
  warning: "rgb(220 130 0)",
  danger: "rgb(220 0 0)",
  unknown: "rgb(100 100 100)",
  success: "rgb(0 130 60)",
};

describe("ASIC metric colors", () => {
  it("keeps temperature thresholds while making high hashrate favorable", () => {
    expect(getMetricColor(palette, "temperature", 85, [0, 100])).toBe("rgb(220 0 0/ 0.9)");
    expect(getMetricColor(palette, "hashrate", 1100, [0, 1100])).toContain(palette.success);
    expect(getMetricColor(palette, "hashrate", 1100, [0, 1100])).not.toBe(
      getMetricColor(palette, "hashrate", 0, [0, 300]),
    );
  });

  it("uses neutral colors for voltage and frequency", () => {
    for (const metric of ["voltage", "frequency"] as const) {
      expect(getMetricColor(palette, metric, 800, [500, 800])).toContain(palette.cool);
      expect(getMetricColor(palette, metric, 500, [500, 800])).not.toBe(
        getMetricColor(palette, metric, 800, [500, 800]),
      );
    }
  });

  it("handles unknown, equal, empty and all-zero readings without a favorable fallback", () => {
    for (const value of [undefined, NaN, Infinity])
      expect(getMetricColor(palette, "hashrate", value, [0, 300])).toBe(palette.unknown);
    expect(getMetricRange("hashrate", [0, 0, undefined])).toEqual([0, 0]);
    expect(getMetricRange("voltage", [800, 800, NaN])).toEqual([800, 800]);
    expect(getMetricRange("frequency", [])).toEqual([0, 0]);
    expect(getMetricColor(palette, "hashrate", 0, [0, 0])).toBe(getMetricColor(palette, "hashrate", 0, [0, 300]));
    expect(getMetricColor(palette, "voltage", 800, [800, 800])).not.toMatch(/NaN|Infinity/);
  });
});

describe("Shared FPO hashrate thresholds", () => {
  it.each([
    [0, "danger"],
    [999.9, "danger"],
    [1000, "warning"],
    [1074.9, "warning"],
    [1075, "success"],
    [2000, "success"],
  ] as const)("colors %s GH/s with the %s band regardless of the board range", (value, band) => {
    const color = getMetricColor(palette, "hashrate", value, [0, 1100]);
    expect(color).toContain(palette[band]);
    expect(getMetricColor(palette, "hashrate", value, [0, 100_000])).toBe(color);
  });
  it.each([undefined, NaN, Infinity])("keeps missing/non-finite readings unknown: %s", (value) => {
    expect(getMetricColor(palette, "hashrate", value, [0, 1100])).toBe(palette.unknown);
  });
});
