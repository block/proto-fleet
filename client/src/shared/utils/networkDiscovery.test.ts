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

  test("accepts hostnames with hyphens, underscores, and trailing dots", () => {
    const input = "miner-01, miner_02, miner.local.";
    const { targets, invalidEntries } = parseManualTargets(input);

    expect(targets.ipAddresses).toEqual(["miner-01", "miner_02", "miner.local."]);
    expect(invalidEntries).toEqual([]);
  });
});
