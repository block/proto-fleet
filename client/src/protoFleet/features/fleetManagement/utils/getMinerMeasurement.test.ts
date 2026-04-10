import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { getMinerMeasurement } from "./getMinerMeasurement";
import type { Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import { MeasurementSchema } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

const createMeasurement = (value: number, timestamp = new Date()): Measurement => {
  return create(MeasurementSchema, {
    value: value,
    timestamp: create(TimestampSchema, { seconds: BigInt(Math.floor(timestamp.getTime() / 1000)) }),
  });
};

const createMinerSnapshot = (overrides: Partial<MinerStateSnapshot> = {}): MinerStateSnapshot => {
  return {
    deviceIdentifier: "test-device-id",
    name: "Test Miner",
    macAddress: "00:00:00:00:00:00",
    ipAddress: "192.168.1.1",
    deviceStatus: DeviceStatus.ONLINE,
    pairingStatus: 1,
    hashrate: [],
    efficiency: [],
    powerUsage: [],
    temperature: [],
    errors: [],
    url: "",
    model: "",
    firmwareVersion: "",
    ...overrides,
  } as MinerStateSnapshot;
};

const hashrateGetter = (miner: MinerStateSnapshot) => miner.hashrate;

describe("getMinerMeasurement", () => {
  it("returns undefined when miner is undefined", () => {
    expect(getMinerMeasurement(undefined, hashrateGetter)).toBeUndefined();
  });

  it("returns undefined when miner is online but has no telemetry data", () => {
    const miner = createMinerSnapshot({
      deviceStatus: DeviceStatus.ONLINE,
      hashrate: [],
    });
    expect(getMinerMeasurement(miner, hashrateGetter)).toBeUndefined();
  });

  it("returns null when miner is offline", () => {
    const miner = createMinerSnapshot({
      deviceStatus: DeviceStatus.OFFLINE,
      hashrate: [createMeasurement(100)],
    });
    expect(getMinerMeasurement(miner, hashrateGetter)).toBeNull();
  });

  it("returns null when miner is offline and has no telemetry data", () => {
    const miner = createMinerSnapshot({
      deviceStatus: DeviceStatus.OFFLINE,
      hashrate: [],
    });
    expect(getMinerMeasurement(miner, hashrateGetter)).toBeNull();
  });

  it("returns null when miner is inactive and has no telemetry data", () => {
    const miner = createMinerSnapshot({
      deviceStatus: DeviceStatus.INACTIVE,
      hashrate: [],
    });
    expect(getMinerMeasurement(miner, hashrateGetter)).toBeNull();
  });

  it("returns measurement data when miner is online with valid data", () => {
    const hashrateData = [createMeasurement(100), createMeasurement(110)];
    const miner = createMinerSnapshot({
      deviceStatus: DeviceStatus.ONLINE,
      hashrate: hashrateData,
    });
    expect(getMinerMeasurement(miner, hashrateGetter)).toEqual(hashrateData);
  });

  it("returns measurement data when value is 0 (valid data)", () => {
    const hashrateData = [createMeasurement(0)];
    const miner = createMinerSnapshot({
      deviceStatus: DeviceStatus.ONLINE,
      hashrate: hashrateData,
    });
    expect(getMinerMeasurement(miner, hashrateGetter)).toEqual(hashrateData);
  });

  it("returns undefined when miner is online but measurements have no valid data", () => {
    const hashrateData = [create(MeasurementSchema, {}), create(MeasurementSchema, {})];
    const miner = createMinerSnapshot({
      deviceStatus: DeviceStatus.ONLINE,
      hashrate: hashrateData,
    });
    expect(getMinerMeasurement(miner, hashrateGetter)).toBeUndefined();
  });

  it("returns empty array when miner needs pool", () => {
    const miner = createMinerSnapshot({
      deviceStatus: DeviceStatus.NEEDS_MINING_POOL,
      hashrate: [createMeasurement(100)],
    });
    const result = getMinerMeasurement(miner, hashrateGetter);
    expect(result).toEqual([]);
    expect(result).toHaveLength(0);
  });

  it("returns empty array when miner needs authentication", () => {
    const miner = createMinerSnapshot({
      deviceStatus: DeviceStatus.ONLINE,
      pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
      hashrate: [createMeasurement(100)],
    });
    const result = getMinerMeasurement(miner, hashrateGetter);
    expect(result).toEqual([]);
    expect(result).toHaveLength(0);
  });

  it("returns stable empty array reference for needs-pool state", () => {
    const miner = createMinerSnapshot({
      deviceStatus: DeviceStatus.NEEDS_MINING_POOL,
    });
    const result1 = getMinerMeasurement(miner, hashrateGetter);
    const result2 = getMinerMeasurement(miner, hashrateGetter);
    expect(result1).toBe(result2); // Same reference
  });

  it("works with different measurement getters", () => {
    const efficiencyData = [createMeasurement(25.5)];
    const miner = createMinerSnapshot({
      deviceStatus: DeviceStatus.ONLINE,
      efficiency: efficiencyData,
    });
    expect(getMinerMeasurement(miner, (m) => m.efficiency)).toEqual(efficiencyData);
  });
});
