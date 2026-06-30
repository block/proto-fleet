import { describe, expect, it } from "vitest";

import { cohortDeviceDisplayName, cohortDeviceSecondaryText, durationToExpiresAt } from "./utils";

describe("cohort duration helpers", () => {
  const baseDate = new Date("2026-06-25T12:00:00.000Z");

  it("converts presets to an expires_at date", () => {
    expect(durationToExpiresAt("24h", "", "hours", baseDate)?.toISOString()).toBe("2026-06-26T12:00:00.000Z");
    expect(durationToExpiresAt("3d", "", "hours", baseDate)?.toISOString()).toBe("2026-06-28T12:00:00.000Z");
  });

  it("supports no expiration and custom durations", () => {
    expect(durationToExpiresAt("none", "", "hours", baseDate)).toBeUndefined();
    expect(durationToExpiresAt("custom", "2", "days", baseDate)?.toISOString()).toBe("2026-06-27T12:00:00.000Z");
  });

  it("rejects invalid custom durations", () => {
    expect(() => durationToExpiresAt("custom", "0", "hours", baseDate)).toThrow(
      "Expiration duration must be greater than zero",
    );
  });
});

describe("cohort display helpers", () => {
  it("prefers resolved names and keeps device id as the last fallback", () => {
    expect(cohortDeviceDisplayName({ deviceIdentifier: "miner-1", display: { name: "Rig A" } })).toBe("Rig A");
    expect(cohortDeviceDisplayName({ deviceIdentifier: "miner-1", display: { workerName: "worker-a" } })).toBe(
      "worker-a",
    );
    expect(cohortDeviceDisplayName({ deviceIdentifier: "miner-1" })).toBe("miner-1");
  });

  it("uses serial before generic model labels", () => {
    expect(
      cohortDeviceDisplayName({
        deviceIdentifier: "miner-1",
        display: {
          name: "Proto Rig",
          manufacturer: "Proto",
          model: "Rig",
          workerName: "worker-a",
          serialNumber: "PROTO-SIM-001",
        },
      }),
    ).toBe("PROTO-SIM-001");
  });

  it("builds useful secondary metadata", () => {
    expect(
      cohortDeviceSecondaryText({
        workerName: "worker-a",
        manufacturer: "TestCorp",
        model: "TestMiner",
        ipAddress: "127.0.0.1",
        serialNumber: "SN-A",
      }),
    ).toBe("worker-a - TestCorp TestMiner - 127.0.0.1 - SN-A");
  });

  it("omits the primary label from secondary metadata", () => {
    expect(
      cohortDeviceSecondaryText(
        {
          workerName: "worker-a",
          manufacturer: "Proto",
          model: "Rig",
          ipAddress: "127.0.0.1",
          serialNumber: "PROTO-SIM-001",
        },
        "PROTO-SIM-001",
      ),
    ).toBe("worker-a - Proto Rig - 127.0.0.1");
  });
});
