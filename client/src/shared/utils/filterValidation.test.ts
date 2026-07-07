import { describe, expect, it } from "vitest";

import {
  expandSubnetLineToCidrs,
  normalizeCidrLine,
  normalizeSubnetLine,
  type NumericRangeBounds,
  type NumericRangeValue,
  validateCidrLine,
  validateNumericRange,
  validateSubnetLine,
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

describe("validateSubnetLine", () => {
  it("accepts CIDRs and bare IPs like validateCidrLine", () => {
    expect(validateSubnetLine("192.168.1.0/24")).toBeNull();
    expect(validateSubnetLine("10.0.0.5")).toBeNull();
  });

  it("accepts short and full IPv4 ranges (discovery syntax)", () => {
    expect(validateSubnetLine("10.0.0.10-20")).toBeNull();
    expect(validateSubnetLine("10.0.0.10-10.0.0.20")).toBeNull();
    expect(validateSubnetLine("10.0.0.10 - 10.0.0.20")).toBeNull();
  });

  it("rejects an inverted or malformed range", () => {
    expect(validateSubnetLine("10.0.0.20-10")).not.toBeNull();
    expect(validateSubnetLine("10.0.0.10-999")).not.toBeNull();
  });

  it("rejects hostnames with a targeted message (filter matches by IP, not name)", () => {
    expect(validateSubnetLine("miner01")).toBe("Hostnames aren't supported here — use an IP, CIDR, or range");
    expect(validateSubnetLine("rack3-unit.local")).toBe("Hostnames aren't supported here — use an IP, CIDR, or range");
  });

  it("gives the generic CIDR error (not the hostname hint) for an all-numeric malformed IP", () => {
    const error = validateSubnetLine("999.1.1.1");
    expect(error).not.toBeNull();
    expect(error).not.toContain("Hostnames");
  });
});

describe("normalizeSubnetLine", () => {
  it("canonicalizes a short range to its full form so it dedups with the full form", () => {
    expect(normalizeSubnetLine("10.0.0.10-20")).toBe("10.0.0.10-10.0.0.20");
    expect(normalizeSubnetLine("10.0.0.10 - 10.0.0.20")).toBe("10.0.0.10-10.0.0.20");
  });

  it("defers to CIDR normalization for non-ranges", () => {
    expect(normalizeSubnetLine("10.0.0.5")).toBe("10.0.0.5/32");
  });
});

describe("expandSubnetLineToCidrs", () => {
  it("expands a range into its covering CIDRs", () => {
    expect(expandSubnetLineToCidrs("10.0.0.10-10.0.0.21")).toEqual([
      "10.0.0.10/31",
      "10.0.0.12/30",
      "10.0.0.16/30",
      "10.0.0.20/31",
    ]);
  });

  it("passes a CIDR/IP through as a single normalized entry", () => {
    expect(expandSubnetLineToCidrs("192.168.1.0/24")).toEqual(["192.168.1.0/24"]);
    expect(expandSubnetLineToCidrs("10.0.0.5")).toEqual(["10.0.0.5/32"]);
  });
});
