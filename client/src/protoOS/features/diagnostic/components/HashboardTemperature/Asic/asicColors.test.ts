import { describe, expect, it } from "vitest";
import { getMetricColor, getMetricRange } from "./asicColors";

const palette = {
  cool: "rgb(0 100 200)",
  warning: "rgb(220 130 0)",
  danger: "rgb(220 0 0)",
  unknown: "rgb(100 100 100)",
  success: "rgb(0 130 60)",
};

const colors = (metric: "hashrate" | "frequency" | "voltage", values: number[]) => {
  const range = getMetricRange(metric, values);
  return values.map((value) => getMetricColor(palette, metric, value, range));
};

describe("ASIC metric colors", () => {
  it("keeps temperature thresholds independent of the board distribution", () => {
    expect(getMetricRange("temperature", [10, 20])).toEqual(getMetricRange("temperature", [80, 90]));
    expect(getMetricColor(palette, "temperature", 85, [0, 100])).toBe("rgb(220 0 0/ 0.9)");
    expect(getMetricColor(palette, "temperature", 85, [80, 90])).toBe("rgb(220 0 0/ 0.9)");
  });

  it.each(["hashrate", "frequency", "voltage"] as const)("uses a board-relative blue ramp for %s", (metric) => {
    const result = colors(metric, [500, 800, 1100]);
    expect(result).toEqual([
      `color-mix(in srgb, ${palette.cool} 8%, transparent)`,
      `color-mix(in srgb, ${palette.cool} 27%, transparent)`,
      `color-mix(in srgb, ${palette.cool} 45%, transparent)`,
    ]);
    expect(colors(metric, [250, 400, 550])).toEqual(result);
  });

  it.each(["hashrate", "frequency", "voltage"] as const)("does not amplify noise on uniform %s boards", (metric) => {
    for (const values of [
      [800, 800, 800],
      [0, 0, 0],
    ]) {
      const result = colors(metric, values);
      expect(new Set(result).size).toBe(1);
      expect(result[0]).toContain(palette.cool);
    }
    // Rounding may differ by one percentage point; noise must not span the ramp.
    for (const color of colors(metric, [799.99, 800, 800.01])) {
      expect(color).toMatch(/ 2[67]%,/);
    }
  });

  it.each(["hashrate", "frequency", "voltage"] as const)("preserves the %s spread with one dead ASIC", (metric) => {
    const values = Array.from({ length: 100 }, (_, index) => 950 + index);
    const result = colors(metric, [0, ...values]);
    expect(result[0]).toContain("8%");
    expect(result[1]).toContain("8%");
    expect(result[result.length - 1]).toContain("45%");
    expect(new Set(result.slice(1)).size).toBeGreaterThan(20);
    // A dead ASIC also remains distinct when all its neighbours are uniform.
    const uniform = colors(metric, [0, ...Array<number>(100).fill(1000)]);
    expect(uniform[0]).not.toBe(uniform[1]);
    expect(new Set(uniform.slice(1)).size).toBe(1);
  });

  it("ignores missing readings in the range and renders them as unknown", () => {
    for (const metric of ["hashrate", "frequency", "voltage"] as const) {
      expect(getMetricRange(metric, [undefined, NaN, Infinity, -Infinity])).toEqual([0, 0]);
      expect(getMetricRange(metric, [500, undefined, NaN, 1100, Infinity])).toEqual([500, 1100]);
      for (const value of [undefined, NaN, Infinity, -Infinity]) {
        expect(getMetricColor(palette, metric, value, [0, 300])).toBe(palette.unknown);
      }
      expect(getMetricColor(palette, metric, 0, [0, 0])).not.toMatch(/NaN|Infinity/);
    }
  });

  it("keeps a live ASIC distinct on an otherwise all-zero board", () => {
    const result = colors("hashrate", [1000, ...Array<number>(100).fill(0)]);
    expect(result[0]).toContain("45%");
    expect(result[0]).not.toBe(result[1]);
    expect(new Set(result.slice(1)).size).toBe(1);
  });
});
