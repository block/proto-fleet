import { describe, expect, it } from "vitest";

import {
  normalizeCidrLine,
  type NumericRangeBounds,
  type NumericRangeValue,
  validateCidrLine,
  validateNumericRange,
} from "./filterValidation";

const bounds: NumericRangeBounds = { min: 0, max: 100, unit: "TH/s" };

describe("validateNumericRange", () => {
  it("returns no errors for empty value", () => {
    const errors = validateNumericRange({}, bounds);
    expect(errors).toEqual({});
  });

  it("returns no errors for valid single bound", () => {
    expect(validateNumericRange({ min: 50 } satisfies NumericRangeValue, bounds)).toEqual({});
    expect(validateNumericRange({ max: 50 } satisfies NumericRangeValue, bounds)).toEqual({});
  });

  it("returns no errors for valid both bounds", () => {
    expect(validateNumericRange({ min: 10, max: 50 } satisfies NumericRangeValue, bounds)).toEqual({});
  });

  it("flags min below bounds.min", () => {
    expect(validateNumericRange({ min: -5 }, bounds)).toEqual({ min: expect.stringContaining("0") });
  });

  it("flags max above bounds.max", () => {
    expect(validateNumericRange({ max: 999 }, bounds)).toEqual({ max: expect.stringContaining("100") });
  });

  it("flags NaN min", () => {
    expect(validateNumericRange({ min: NaN }, bounds).min).toBeDefined();
  });

  it("flags non-finite max", () => {
    expect(validateNumericRange({ max: Number.POSITIVE_INFINITY }, bounds).max).toBeDefined();
    expect(validateNumericRange({ max: Number.NEGATIVE_INFINITY }, bounds).max).toBeDefined();
  });

  it("flags min > max with cross-field error", () => {
    const errors = validateNumericRange({ min: 60, max: 40 }, bounds);
    expect(errors.cross).toBeDefined();
    expect(errors.cross).toMatch(/Min/i);
  });

  it("does not flag min === max", () => {
    expect(validateNumericRange({ min: 50, max: 50 }, bounds)).toEqual({});
  });

  it("flags min == bounds.min as valid (inclusive boundary)", () => {
    expect(validateNumericRange({ min: 0 }, bounds)).toEqual({});
  });

  it("flags max == bounds.max as valid (inclusive boundary)", () => {
    expect(validateNumericRange({ max: 100 }, bounds)).toEqual({});
  });
});

describe("validateCidrLine", () => {
  it("accepts a canonical IPv4 CIDR", () => {
    expect(validateCidrLine("192.168.1.0/24")).toBeNull();
  });

  it("accepts a routable IPv6 CIDR", () => {
    expect(validateCidrLine("2001:db8::/64")).toBeNull();
  });

  it("accepts a non-canonical CIDR (host bits set) — server normalizes", () => {
    expect(validateCidrLine("192.168.1.5/24")).toBeNull();
  });

  it("accepts a bare IPv4 address (treated as /32)", () => {
    expect(validateCidrLine("10.0.0.5")).toBeNull();
  });

  it("accepts a bare routable IPv6 address (treated as /128)", () => {
    expect(validateCidrLine("2001:db8::1")).toBeNull();
  });

  it("rejects garbage", () => {
    expect(validateCidrLine("not a cidr")).toBeTypeOf("string");
    expect(validateCidrLine("")).toBeTypeOf("string");
    expect(validateCidrLine("999.999.999.999")).toBeTypeOf("string");
    expect(validateCidrLine("192.168.1.0/33")).toBeTypeOf("string");
    expect(validateCidrLine("192.168.1.0/-1")).toBeTypeOf("string");
  });

  it("rejects scoped and link-local IPv6", () => {
    expect(validateCidrLine("fe80::1")).toBeTypeOf("string");
    expect(validateCidrLine("fe80::/64")).toBeTypeOf("string");
    expect(validateCidrLine("fe80::1%en0")).toBeTypeOf("string");
  });

  it("trims surrounding whitespace before validating", () => {
    expect(validateCidrLine("  192.168.1.0/24  ")).toBeNull();
    expect(validateCidrLine("  2001:db8::1  ")).toBeNull();
  });
});

describe("normalizeCidrLine", () => {
  it("masks host bits to canonical network", () => {
    expect(normalizeCidrLine("192.168.1.5/24")).toBe("192.168.1.0/24");
    expect(normalizeCidrLine("10.1.2.3/8")).toBe("10.0.0.0/8");
  });

  it("appends /32 to a bare IPv4", () => {
    expect(normalizeCidrLine("10.0.0.5")).toBe("10.0.0.5/32");
  });

  it("appends /128 to a bare IPv6", () => {
    expect(normalizeCidrLine("2001:db8::1")).toBe("2001:db8::1/128");
  });

  it("leaves already-canonical CIDRs unchanged", () => {
    expect(normalizeCidrLine("192.168.1.0/24")).toBe("192.168.1.0/24");
    expect(normalizeCidrLine("2001:db8::/64")).toBe("2001:db8::/64");
  });

  it("trims surrounding whitespace", () => {
    expect(normalizeCidrLine("  192.168.1.0/24  ")).toBe("192.168.1.0/24");
    expect(normalizeCidrLine("  2001:db8::1  ")).toBe("2001:db8::1/128");
  });

  it("preserves host == network for /32", () => {
    expect(normalizeCidrLine("192.168.1.5/32")).toBe("192.168.1.5/32");
  });
});
