import { describe, expect, it } from "vitest";
import { formatAcceptanceRate, formatShareCount, getAcceptanceRate, normalizeShareCount } from "./poolStats";

describe("normalizeShareCount", () => {
  it("preserves finite positive values and normalizes invalid values", () => {
    expect(normalizeShareCount(12.5)).toBe(12.5);
    expect(normalizeShareCount(0)).toBe(0);
    expect(normalizeShareCount(-1)).toBe(0);
    expect(normalizeShareCount(Number.NaN)).toBe(0);
    expect(normalizeShareCount(Number.POSITIVE_INFINITY)).toBe(0);
  });
});

describe("getAcceptanceRate", () => {
  it("returns 0 when no shares have been submitted", () => {
    expect(getAcceptanceRate(0, 0, 0)).toBe(0);
  });

  it("returns 100 when every share is accepted", () => {
    expect(getAcceptanceRate(100, 0, 0)).toBe(100);
  });

  it("divides accepted by the total of all three counts", () => {
    // 71900 / (71900 + 305 + 17) = 99.55...%
    expect(getAcceptanceRate(71900, 305, 17)).toBeCloseTo(99.554, 2);
  });

  it("counts rejected and invalid against the rate", () => {
    expect(getAcceptanceRate(90, 5, 5)).toBe(90);
  });

  it("normalizes negative and non-finite telemetry", () => {
    expect(getAcceptanceRate(-1, 2, 2)).toBe(0);
    expect(getAcceptanceRate(10, -2, Number.NaN)).toBe(100);
    expect(getAcceptanceRate(Number.POSITIVE_INFINITY, 1, 0)).toBe(0);
  });
});

describe("formatAcceptanceRate", () => {
  it("drops trailing zeros on whole values", () => {
    expect(formatAcceptanceRate(100)).toBe("100%");
  });

  it("keeps up to two decimals", () => {
    expect(formatAcceptanceRate(97.312)).toBe("97.31%");
    expect(formatAcceptanceRate(99.5)).toBe("99.5%");
  });

  it("normalizes invalid and out-of-range values", () => {
    expect(formatAcceptanceRate(Number.NaN)).toBe("0%");
    expect(formatAcceptanceRate(Number.POSITIVE_INFINITY)).toBe("0%");
    expect(formatAcceptanceRate(-1)).toBe("0%");
    expect(formatAcceptanceRate(101)).toBe("100%");
  });
});

describe("formatShareCount", () => {
  it("leaves small counts untouched", () => {
    expect(formatShareCount(305)).toBe("305");
    expect(formatShareCount(17)).toBe("17");
    expect(formatShareCount(0)).toBe("0");
  });

  it("normalizes invalid and negative counts", () => {
    expect(formatShareCount(-1)).toBe("0");
    expect(formatShareCount(Number.NaN)).toBe("0");
    expect(formatShareCount(Number.POSITIVE_INFINITY)).toBe("0");
  });

  it("keeps one decimal for mantissas of ten or more", () => {
    expect(formatShareCount(71900)).toBe("71.9K");
    expect(formatShareCount(524300)).toBe("524.3K");
  });

  it("keeps two decimals for mantissas below ten", () => {
    expect(formatShareCount(1190)).toBe("1.19K");
  });

  it("drops trailing zeros", () => {
    expect(formatShareCount(134000)).toBe("134K");
  });

  it("scales into millions and billions", () => {
    expect(formatShareCount(165_600_000_000)).toBe("165.6B");
    expect(formatShareCount(2_500_000)).toBe("2.5M");
  });
});
