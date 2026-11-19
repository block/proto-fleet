// @vitest-environment node
import { describe, expect, test } from "vitest";
import { formatFlowRate } from "./flowRate";

describe("formatFlowRate", () => {
  test("converts liters to US gallons and uses one decimal place", () => {
    expect(formatFlowRate(161.3, "GPM")).toBe("42.6 GPM");
    expect(formatFlowRate(161.3, "L/min")).toBe("161.3 L/min");
    expect(formatFlowRate(3.785411784, "GPM")).toBe("1.0 GPM");
  });

  test.each(["GPM", "L/min"] as const)("preserves zero and missing readings in %s", (unit) => {
    expect(formatFlowRate(0, unit)).toBe(`0.0 ${unit}`);
    for (const value of [undefined, null, NaN, Infinity, -Infinity]) {
      expect(formatFlowRate(value, unit)).toBe("—");
    }
  });
});
