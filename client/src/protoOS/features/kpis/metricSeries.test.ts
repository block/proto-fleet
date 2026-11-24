import { describe, expect, test } from "vitest";

import { getHighestAveragePerformer, getHighestMaximumPerformer, getLowestAveragePerformer } from "./metricSeries";
import type { KpiChartSeries } from "@/protoOS/store";

const tanks: KpiChartSeries[] = Array.from({ length: 6 }, (_, index) => ({
  key: `tank:${index + 1}`,
  label: String(index + 1),
  name: `Tank ${index + 1}`,
  colorIndex: index + 1,
  averageValue: index === 2 ? 782.4 : 800 + index,
  maximumValue: index === 4 ? 90 : 70 + index,
}));

describe("metric series performers", () => {
  test("identifies the lowest-performing tank by average", () => {
    expect(getLowestAveragePerformer(tanks)).toBe("Tank 3");
  });

  test("supports metrics where a higher average is worse", () => {
    expect(getHighestAveragePerformer(tanks)).toBe("Tank 6");
  });

  test("identifies the hottest tank by maximum", () => {
    expect(getHighestMaximumPerformer(tanks)).toBe("Tank 5");
  });
});
