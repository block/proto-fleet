import { describe, expect, test } from "vitest";
import { cidrToSubnetMask } from "@/shared/utils/network";

describe("cidrToSubnetMask", () => {
  test("should return the correct subnet mask for CIDR suffix of 24", () => {
    const cidr = "192.168.2.5/24";
    const result = cidrToSubnetMask(cidr);
    expect(result).toBe("255.255.255.0");
  });

  test("should return the correct subnet mask for CIDR suffix of 16", () => {
    const cidr = "192.168.2.5/16";
    const result = cidrToSubnetMask(cidr);
    expect(result).toBe("255.255.0.0");
  });

  test("should return the correct subnet mask for CIDR suffix of 8", () => {
    const cidr = "192.168.2.5/8";
    const result = cidrToSubnetMask(cidr);
    expect(result).toBe("255.0.0.0");
  });

  test("should return null for an invalid CIDR notation", () => {
    const mask = "255.255.255.0";
    const result = cidrToSubnetMask(mask);
    expect(result).toBe(null);
  });
});
