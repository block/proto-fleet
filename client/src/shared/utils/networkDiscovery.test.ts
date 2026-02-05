import { describe, expect, test } from "vitest";
import { parseManualTargets } from "@/shared/utils/networkDiscovery";

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
});
