import { describe, expect, test } from "vitest";

import { deriveStatusSegments, formatRackCardStats, mapSlotStatuses } from "./rackCardMapper";

import {
  type DeviceSetStats,
  type RackSlotStatus,
  SlotDeviceStatus,
} from "@/protoFleet/api/generated/device_set/v1/device_set_pb";

// Helper to create a partial DeviceSetStats with sensible defaults
function makeStats(overrides: Partial<DeviceSetStats> = {}): DeviceSetStats {
  return {
    deviceSetId: 1n,
    deviceCount: 10,
    reportingCount: 0,
    totalHashrateThs: 0,
    avgEfficiencyJth: 0,
    totalPowerKw: 0,
    minTemperatureC: 0,
    maxTemperatureC: 0,
    hashingCount: 0,
    brokenCount: 0,
    offlineCount: 0,
    sleepingCount: 0,
    hashrateReportingCount: 0,
    efficiencyReportingCount: 0,
    powerReportingCount: 0,
    temperatureReportingCount: 0,
    controlBoardIssueCount: 0,
    fanIssueCount: 0,
    hashBoardIssueCount: 0,
    psuIssueCount: 0,
    slotStatuses: [],
    ...overrides,
  } as DeviceSetStats;
}

function makeSlot(row: number, column: number, status: SlotDeviceStatus): RackSlotStatus {
  return { row, column, status } as RackSlotStatus;
}

describe("deriveStatusSegments", () => {
  test("returns healthy when no issues, offline, or sleeping", () => {
    expect(deriveStatusSegments(makeStats())).toEqual([{ color: "bg-intent-success-fill", text: "Healthy" }]);
  });

  test("returns issues segment only", () => {
    expect(deriveStatusSegments(makeStats({ fanIssueCount: 3 }))).toEqual([
      { color: "bg-intent-critical-fill", text: "3 issues" },
    ]);
  });

  test("returns offline segment only", () => {
    expect(deriveStatusSegments(makeStats({ offlineCount: 2 }))).toEqual([
      { color: "bg-intent-warning-fill", text: "2 offline" },
    ]);
  });

  test("returns sleeping segment only", () => {
    expect(deriveStatusSegments(makeStats({ sleepingCount: 1 }))).toEqual([
      { color: "bg-core-primary-20", text: "1 sleeping" },
    ]);
  });

  test("returns all segments in order: issues, offline, sleeping", () => {
    expect(deriveStatusSegments(makeStats({ hashBoardIssueCount: 2, offlineCount: 3, sleepingCount: 1 }))).toEqual([
      { color: "bg-intent-critical-fill", text: "2 issues" },
      { color: "bg-intent-warning-fill", text: "3 offline" },
      { color: "bg-core-primary-20", text: "1 sleeping" },
    ]);
  });

  test("sums all issue component counts into one segment", () => {
    expect(
      deriveStatusSegments(
        makeStats({ controlBoardIssueCount: 1, fanIssueCount: 2, hashBoardIssueCount: 3, psuIssueCount: 4 }),
      ),
    ).toEqual([{ color: "bg-intent-critical-fill", text: "10 issues" }]);
  });
});

describe("mapSlotStatuses", () => {
  test("returns all empty for no slot statuses", () => {
    const result = mapSlotStatuses([], 2, 3);
    expect(result).toEqual(["empty", "empty", "empty", "empty", "empty", "empty"]);
  });

  test("places slots by row-major position", () => {
    const slots = [
      makeSlot(0, 0, SlotDeviceStatus.HEALTHY),
      makeSlot(0, 1, SlotDeviceStatus.OFFLINE),
      makeSlot(1, 0, SlotDeviceStatus.NEEDS_ATTENTION),
      makeSlot(1, 1, SlotDeviceStatus.SLEEPING),
    ];
    const result = mapSlotStatuses(slots, 2, 2);
    expect(result).toEqual(["healthy", "offline", "needsAttention", "sleeping"]);
  });

  test("handles sparse slot data (not all positions filled)", () => {
    const slots = [makeSlot(0, 2, SlotDeviceStatus.HEALTHY), makeSlot(1, 0, SlotDeviceStatus.OFFLINE)];
    const result = mapSlotStatuses(slots, 2, 3);
    expect(result).toEqual(["empty", "empty", "healthy", "offline", "empty", "empty"]);
  });

  test("ignores out-of-bounds slots", () => {
    const slots = [makeSlot(5, 5, SlotDeviceStatus.HEALTHY)];
    const result = mapSlotStatuses(slots, 2, 2);
    expect(result).toEqual(["empty", "empty", "empty", "empty"]);
  });

  test("maps all SlotDeviceStatus values correctly", () => {
    const slots = [
      makeSlot(0, 0, SlotDeviceStatus.UNSPECIFIED),
      makeSlot(0, 1, SlotDeviceStatus.EMPTY),
      makeSlot(0, 2, SlotDeviceStatus.HEALTHY),
      makeSlot(0, 3, SlotDeviceStatus.NEEDS_ATTENTION),
      makeSlot(0, 4, SlotDeviceStatus.OFFLINE),
      makeSlot(0, 5, SlotDeviceStatus.SLEEPING),
    ];
    const result = mapSlotStatuses(slots, 1, 6);
    expect(result).toEqual(["empty", "empty", "healthy", "needsAttention", "offline", "sleeping"]);
  });
});

describe("formatRackCardStats", () => {
  test("returns undefined for all metrics when no devices are reporting", () => {
    const result = formatRackCardStats(makeStats(), "C");
    expect(result).toEqual({
      hashrate: undefined,
      efficiency: undefined,
      power: undefined,
      temperature: undefined,
    });
  });

  test("formats hashrate when reporting", () => {
    const result = formatRackCardStats(makeStats({ hashrateReportingCount: 1, totalHashrateThs: 123.456 }), "C");
    expect(result.hashrate).toBe("123.5 TH/s");
  });

  test("formats efficiency when reporting", () => {
    const result = formatRackCardStats(makeStats({ efficiencyReportingCount: 1, avgEfficiencyJth: 22.5 }), "C");
    expect(result.efficiency).toBe("22.5 J/TH");
  });

  test("formats power when reporting", () => {
    const result = formatRackCardStats(makeStats({ powerReportingCount: 1, totalPowerKw: 5.1 }), "C");
    expect(result.power).toBe("5.1 kW");
  });

  test("formats temperature range in Celsius when reporting", () => {
    const result = formatRackCardStats(
      makeStats({ temperatureReportingCount: 1, minTemperatureC: 45, maxTemperatureC: 78 }),
      "C",
    );
    expect(result.temperature).toBe("45.0 °C – 78.0 °C");
  });

  test("formats temperature range in Fahrenheit when reporting", () => {
    const result = formatRackCardStats(
      makeStats({ temperatureReportingCount: 1, minTemperatureC: 45, maxTemperatureC: 78 }),
      "F",
    );
    expect(result.temperature).toBe("113.0 °F – 172.4 °F");
  });
});
