import { describe, expect, it } from "vitest";

import { formatNumericRangeCondition, formatTextareaListCondition } from "./filterChipFormatting";

describe("formatNumericRangeCondition", () => {
  it("renders both bounds as a range", () => {
    expect(formatNumericRangeCondition({ min: 50, max: 200 }, "TH/s")).toBe("50 TH/s - 200 TH/s");
  });

  it("renders min-only with the inclusive operator", () => {
    expect(formatNumericRangeCondition({ min: 50 }, "TH/s")).toBe("≥ 50 TH/s");
  });

  it("renders max-only with the inclusive operator", () => {
    expect(formatNumericRangeCondition({ max: 200 }, "kW")).toBe("≤ 200 kW");
  });

  it("returns empty string for empty value", () => {
    expect(formatNumericRangeCondition({}, "TH/s")).toBe("");
  });

  it("preserves decimal precision passed by the user", () => {
    expect(formatNumericRangeCondition({ min: 1.5, max: 2.25 }, "kW")).toBe("1.5 kW - 2.25 kW");
  });
});

describe("formatTextareaListCondition", () => {
  it("returns the single value unchanged when only one entry", () => {
    expect(formatTextareaListCondition(["192.168.1.0/24"])).toBe("192.168.1.0/24");
  });

  it("collapses to '<n> entries' as soon as there are multiple entries (default noun)", () => {
    expect(formatTextareaListCondition(["192.168.1.0/24", "10.0.0.0/8"])).toBe("2 entries");
  });

  it("uses the provided plural noun when collapsing", () => {
    expect(formatTextareaListCondition(["192.168.1.0/24", "10.0.0.0/8"], { noun: "subnet" })).toBe("2 subnets");
    expect(formatTextareaListCondition(["a", "b", "c"], { noun: "subnet" })).toBe("3 subnets");
  });

  it("does not pluralize the singular case (still uses the raw value)", () => {
    expect(formatTextareaListCondition(["192.168.1.0/24"], { noun: "subnet" })).toBe("192.168.1.0/24");
  });

  it("returns empty string for empty array", () => {
    expect(formatTextareaListCondition([])).toBe("");
  });
});
