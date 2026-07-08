import { describe, expect, test } from "vitest";
import {
  intToIpv4,
  ipv4RangeToCidrs,
  isValidCidr,
  isValidIpv6,
  looksLikeIpRange,
  parseManualTargets,
} from "@/shared/utils/networkDiscovery";

describe("parseManualTargets", () => {
  test("parses IPs, hostnames, CIDR subnets, and ranges", () => {
    const input = "192.168.1.10, miner01\n192.168.1.0/24\n192.168.1.1-10\n10.0.0.5-10.0.0.10";
    const { targets, invalidEntries } = parseManualTargets(input);

    expect(targets.ipAddresses).toEqual(["192.168.1.10", "miner01"]);
    expect(targets.subnets).toEqual(["192.168.1.0/24"]);
    expect(targets.ipRanges).toEqual([
      { startIp: "192.168.1.1", endIp: "192.168.1.10" },
      { startIp: "10.0.0.5", endIp: "10.0.0.10" },
    ]);
    expect(invalidEntries).toEqual([]);
  });

  test("flags invalid entries and does not coerce them", () => {
    const input = "999.1.1.1, 10.0.0.1-0, 192.168.1.1-192.168.1.0";
    const { targets, invalidEntries } = parseManualTargets(input);

    expect(targets.ipAddresses).toEqual([]);
    expect(targets.subnets).toEqual([]);
    expect(targets.ipRanges).toEqual([]);
    expect(invalidEntries).toEqual(["999.1.1.1", "10.0.0.1-0", "192.168.1.1-192.168.1.0"]);
  });

  test("categorizes invalid entries by type", () => {
    const input = "999.1.1.1, 10.0.0.1-0, 192.168.1.0/33, -invalid";
    const { invalidEntries, categorizedInvalidEntries } = parseManualTargets(input);

    expect(invalidEntries).toHaveLength(4);
    expect(categorizedInvalidEntries.ipAddresses).toEqual(["999.1.1.1", "-invalid"]);
    expect(categorizedInvalidEntries.ipRanges).toEqual(["10.0.0.1-0"]);
    expect(categorizedInvalidEntries.subnets).toEqual(["192.168.1.0/33"]);
  });

  test("accepts hostnames with hyphens, underscores, and trailing dots", () => {
    const input = "miner-01, miner_02, miner.local.";
    const { targets, invalidEntries } = parseManualTargets(input);

    expect(targets.ipAddresses).toEqual(["miner-01", "miner_02", "miner.local."]);
    expect(invalidEntries).toEqual([]);
  });

  test("parses IP ranges with spaces around dash", () => {
    const input = "10.32.1.100 - 10.32.1.150, 192.168.1.1 - 50";
    const { targets, invalidEntries } = parseManualTargets(input);

    expect(targets.ipRanges).toEqual([
      { startIp: "10.32.1.100", endIp: "10.32.1.150" },
      { startIp: "192.168.1.1", endIp: "192.168.1.50" },
    ]);
    expect(invalidEntries).toEqual([]);
  });

  test("rejects IP-like entries with invalid octets and trailing dot", () => {
    const input = "999.999.999.999., 256.1.1.1.";
    const { targets, invalidEntries, categorizedInvalidEntries } = parseManualTargets(input);

    expect(targets.ipAddresses).toEqual([]);
    expect(invalidEntries).toEqual(["999.999.999.999.", "256.1.1.1."]);
    expect(categorizedInvalidEntries.ipAddresses).toEqual(["999.999.999.999.", "256.1.1.1."]);
  });

  test("normalizes valid IP addresses with trailing dot", () => {
    const input = "192.168.1.1., 10.0.0.1.";
    const { targets, invalidEntries } = parseManualTargets(input);

    expect(targets.ipAddresses).toEqual(["192.168.1.1", "10.0.0.1"]);
    expect(invalidEntries).toEqual([]);
  });

  test("parses IPv6 addresses alongside IPv4", () => {
    const input = "fd00::1, 2001:db8::1\n192.168.1.10";
    const { targets, invalidEntries } = parseManualTargets(input);

    expect(targets.ipAddresses).toEqual(["fd00::1", "2001:db8::1", "192.168.1.10"]);
    expect(invalidEntries).toEqual([]);
  });

  test("flags invalid IPv6 addresses", () => {
    const input = "fd00::xyz";
    const { invalidEntries, categorizedInvalidEntries } = parseManualTargets(input);

    expect(invalidEntries).toHaveLength(1);
    expect(categorizedInvalidEntries.ipAddresses).toEqual(["fd00::xyz"]);
  });

  test("flags IPv6 CIDRs as invalid", () => {
    const input = "fd00::/64, fd00::/120";
    const { invalidEntries } = parseManualTargets(input);

    expect(invalidEntries).toEqual(["fd00::/64", "fd00::/120"]);
  });

  test("mixed IPv4 and IPv6 input is categorized correctly", () => {
    const input = "192.168.1.0/24, ::1, 10.0.0.1";
    const { targets, invalidEntries } = parseManualTargets(input);

    expect(targets.ipAddresses).toEqual(["::1", "10.0.0.1"]);
    expect(targets.subnets).toEqual(["192.168.1.0/24"]);
    expect(invalidEntries).toEqual([]);
  });
});

describe("isValidIpv6", () => {
  test("accepts valid IPv6 addresses", () => {
    expect(isValidIpv6("::1")).toBe(true);
    expect(isValidIpv6("fd00::1")).toBe(true);
    expect(isValidIpv6("2001:db8::1")).toBe(true);
    expect(isValidIpv6("2001:0db8:85a3:0000:0000:8a2e:0370:7334")).toBe(true);
  });

  test("rejects invalid and unusable addresses", () => {
    expect(isValidIpv6("192.168.1.1")).toBe(false);
    expect(isValidIpv6("not-an-ip")).toBe(false);
    expect(isValidIpv6("")).toBe(false);
    expect(isValidIpv6("fd00::xyz")).toBe(false);
    expect(isValidIpv6("fe80::1")).toBe(false); // link-local requires scope
    expect(isValidIpv6("febf::1234")).toBe(false); // full fe80::/10 range
    expect(isValidIpv6("fe80::1%eth0")).toBe(false); // scoped address
  });
});

describe("looksLikeIpRange", () => {
  test("matches short and full range syntax (with optional spaces)", () => {
    expect(looksLikeIpRange("10.0.0.10-20")).toBe(true);
    expect(looksLikeIpRange("10.0.0.10-10.0.0.20")).toBe(true);
    expect(looksLikeIpRange("10.0.0.10 - 10.0.0.20")).toBe(true);
  });

  test("does not match CIDRs or bare IPs", () => {
    expect(looksLikeIpRange("10.0.0.0/24")).toBe(false);
    expect(looksLikeIpRange("10.0.0.10")).toBe(false);
  });
});

describe("ipv4RangeToCidrs", () => {
  test("aligned power-of-two range collapses to a single CIDR", () => {
    expect(ipv4RangeToCidrs("192.168.1.0", "192.168.1.255")).toEqual(["192.168.1.0/24"]);
    expect(ipv4RangeToCidrs("10.0.0.8", "10.0.0.15")).toEqual(["10.0.0.8/29"]);
  });

  test("arbitrary range decomposes into the minimal covering set", () => {
    // The 8–12 miners case: 10.0.0.10 through 10.0.0.21 inclusive.
    expect(ipv4RangeToCidrs("10.0.0.10", "10.0.0.21")).toEqual([
      "10.0.0.10/31",
      "10.0.0.12/30",
      "10.0.0.16/30",
      "10.0.0.20/31",
    ]);
  });

  test("single address yields a /32", () => {
    expect(ipv4RangeToCidrs("10.0.0.5", "10.0.0.5")).toEqual(["10.0.0.5/32"]);
  });

  test("covers the full IPv4 space without overflowing", () => {
    expect(ipv4RangeToCidrs("0.0.0.0", "255.255.255.255")).toEqual(["0.0.0.0/0"]);
  });

  test("inverted range yields nothing", () => {
    expect(ipv4RangeToCidrs("10.0.0.20", "10.0.0.10")).toEqual([]);
  });

  test("every address in the range is covered by exactly one emitted CIDR", () => {
    const cidrs = ipv4RangeToCidrs("10.0.0.10", "10.0.0.21");
    const contains = (cidr: string, ip: number) => {
      const [net, bits] = cidr.split("/");
      const netInt = net.split(".").reduce((a, p) => ((a << 8) + Number(p)) >>> 0, 0);
      const mask = Number(bits) === 0 ? 0 : (0xffffffff << (32 - Number(bits))) >>> 0;
      return (ip & mask) >>> 0 === netInt;
    };
    for (let octet = 10; octet <= 21; octet++) {
      const ip = (10 << 24) + octet;
      expect(cidrs.filter((c) => contains(c, ip >>> 0)).length).toBe(1);
    }
  });
});

describe("intToIpv4", () => {
  test("round-trips with the documented octet packing", () => {
    expect(intToIpv4(0)).toBe("0.0.0.0");
    expect(intToIpv4(0xffffffff)).toBe("255.255.255.255");
    expect(intToIpv4((192 << 24) | (168 << 16) | (1 << 8) | 42)).toBe("192.168.1.42");
  });
});

describe("isValidCidr", () => {
  test("accepts valid IPv4 CIDRs", () => {
    expect(isValidCidr("192.168.1.0/24")).toBe(true);
    expect(isValidCidr("10.0.0.0/8")).toBe(true);
  });

  test("rejects IPv6 CIDRs and invalid CIDRs", () => {
    expect(isValidCidr("192.168.1.0/33")).toBe(false);
    expect(isValidCidr("fd00::/64")).toBe(false);
    expect(isValidCidr("fd00::/120")).toBe(false);
    expect(isValidCidr("::1/128")).toBe(false);
    expect(isValidCidr("not-a-cidr/24")).toBe(false);
  });
});
