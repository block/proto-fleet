import { describe, expect, it } from "vitest";

import { formatListCountLabel } from "./listCountLabel";

describe("formatListCountLabel", () => {
  it("renders the plain count when no filters are active", () => {
    expect(formatListCountLabel(14, { singular: "miner", plural: "miners" })).toBe("14 miners");
  });

  it("singularizes the plain count", () => {
    expect(formatListCountLabel(1, { singular: "site", plural: "sites" })).toBe("1 site");
  });

  it("renders 'X of Y' when filters are active and counts differ", () => {
    expect(
      formatListCountLabel(5, { unfilteredTotal: 14, hasActiveFilters: true, singular: "miner", plural: "miners" }),
    ).toBe("5 of 14 miners");
  });

  it("singularizes the denominator in the 'X of Y' form", () => {
    expect(
      formatListCountLabel(0, { unfilteredTotal: 1, hasActiveFilters: true, singular: "rack", plural: "racks" }),
    ).toBe("0 of 1 rack");
  });

  it("falls back to the plain count when filters are active but counts match", () => {
    expect(
      formatListCountLabel(14, {
        unfilteredTotal: 14,
        hasActiveFilters: true,
        singular: "building",
        plural: "buildings",
      }),
    ).toBe("14 buildings");
  });

  it("falls back to the plain count when the unfiltered total is unknown", () => {
    expect(formatListCountLabel(5, { hasActiveFilters: true, singular: "rack", plural: "racks" })).toBe("5 racks");
  });
});
