import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import type { MinerStateSnapshot } from "../slices/fleetSlice";
import { useFleetStore } from "../useFleetStore";
import {
  useMinerEfficiency,
  useMinerFirmwareVersion,
  useMinerHashrate,
  useMinerModel,
  useMinerPowerUsage,
  useMinerTemperature,
} from "./useFleet";
import { MeasurementSchema } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import type { Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import { DeviceStatus, MinerStateCountsSchema } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

describe("useFleet measurement selectors", () => {
  beforeEach(() => {
    // Reset store before each test - only reset data properties, keep methods
    useFleetStore.setState((state) => ({
      fleet: {
        ...state.fleet,
        miners: {},
        minerIds: [],
        totalMiners: 0,
        deviceStatusCounts: create(MinerStateCountsSchema, {}),
        isLoading: false,
        isStreaming: false,
      },
    }));
  });

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

  describe("useMinerHashrate", () => {
    it("returns undefined when miner is not in store", () => {
      const { result } = renderHook(() => useMinerHashrate("non-existent-id"));
      expect(result.current).toBeUndefined();
    });

    it("returns undefined when miner is online but has no telemetry data", () => {
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ONLINE,
        hashrate: [],
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerHashrate(miner.deviceIdentifier));
      expect(result.current).toBeUndefined();
    });

    it("returns null when miner is offline and has no telemetry data", () => {
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.OFFLINE,
        hashrate: [],
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerHashrate(miner.deviceIdentifier));
      expect(result.current).toBeNull();
    });

    it("returns null when miner is inactive and has no telemetry data", () => {
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.INACTIVE,
        hashrate: [],
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerHashrate(miner.deviceIdentifier));
      expect(result.current).toBeNull();
    });

    it("returns hashrate data when miner is online with valid data (value > 0)", () => {
      const hashrateData = [createMeasurement(100), createMeasurement(110), createMeasurement(105)];
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ONLINE,
        hashrate: hashrateData,
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerHashrate(miner.deviceIdentifier));
      expect(result.current).toEqual(hashrateData);
    });

    it("returns hashrate data when miner is online with valid data (value = 0)", () => {
      const hashrateData = [createMeasurement(0), createMeasurement(0)];
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ONLINE,
        hashrate: hashrateData,
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerHashrate(miner.deviceIdentifier));
      expect(result.current).toEqual(hashrateData);
    });

    it("returns undefined when miner is online but telemetry has no valid measurements", () => {
      // Array exists but measurements have no valid data
      const hashrateData = [create(MeasurementSchema, {}), create(MeasurementSchema, {})];
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ONLINE,
        hashrate: hashrateData,
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerHashrate(miner.deviceIdentifier));
      expect(result.current).toBeUndefined();
    });
  });

  describe("useMinerEfficiency", () => {
    it("returns undefined when miner is not in store", () => {
      const { result } = renderHook(() => useMinerEfficiency("non-existent-id"));
      expect(result.current).toBeUndefined();
    });

    it("returns undefined when miner is online but has no telemetry data", () => {
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ONLINE,
        hashrate: [],
        efficiency: [],
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerEfficiency(miner.deviceIdentifier));
      expect(result.current).toBeUndefined();
    });

    it("returns null when miner is offline", () => {
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.OFFLINE,
        hashrate: [],
        efficiency: [],
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerEfficiency(miner.deviceIdentifier));
      expect(result.current).toBeNull();
    });

    it("returns efficiency data when miner has valid hashrate data", () => {
      const hashrateData = [createMeasurement(100)];
      const efficiencyData = [createMeasurement(45.5)];
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ONLINE,
        hashrate: hashrateData,
        efficiency: efficiencyData,
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerEfficiency(miner.deviceIdentifier));
      expect(result.current).toEqual(efficiencyData);
    });

    it("returns undefined when hashrate is valid but efficiency has no valid measurements", () => {
      const hashrateData = [createMeasurement(100)];
      const efficiencyData = [create(MeasurementSchema, {})];
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ONLINE,
        hashrate: hashrateData,
        efficiency: efficiencyData,
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerEfficiency(miner.deviceIdentifier));
      expect(result.current).toBeUndefined();
    });
  });

  describe("useMinerPowerUsage", () => {
    it("returns undefined when miner is not in store", () => {
      const { result } = renderHook(() => useMinerPowerUsage("non-existent-id"));
      expect(result.current).toBeUndefined();
    });

    it("returns undefined when miner is online but has no telemetry data", () => {
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ONLINE,
        hashrate: [],
        powerUsage: [],
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerPowerUsage(miner.deviceIdentifier));
      expect(result.current).toBeUndefined();
    });

    it("returns null when miner is inactive", () => {
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.INACTIVE,
        hashrate: [],
        powerUsage: [],
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerPowerUsage(miner.deviceIdentifier));
      expect(result.current).toBeNull();
    });

    it("returns power usage data when miner has valid hashrate data", () => {
      const hashrateData = [createMeasurement(100)];
      const powerUsageData = [createMeasurement(3000)];
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ONLINE,
        hashrate: hashrateData,
        powerUsage: powerUsageData,
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerPowerUsage(miner.deviceIdentifier));
      expect(result.current).toEqual(powerUsageData);
    });

    it("returns undefined when hashrate is valid but powerUsage has no valid measurements", () => {
      const hashrateData = [createMeasurement(100)];
      const powerUsageData = [create(MeasurementSchema, {})];
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ONLINE,
        hashrate: hashrateData,
        powerUsage: powerUsageData,
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerPowerUsage(miner.deviceIdentifier));
      expect(result.current).toBeUndefined();
    });
  });

  describe("useMinerTemperature", () => {
    it("returns undefined when miner is not in store", () => {
      const { result } = renderHook(() => useMinerTemperature("non-existent-id"));
      expect(result.current).toBeUndefined();
    });

    it("returns undefined when miner is online but has no telemetry data", () => {
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ONLINE,
        hashrate: [],
        temperature: [],
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerTemperature(miner.deviceIdentifier));
      expect(result.current).toBeUndefined();
    });

    it("returns null when miner is offline", () => {
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.OFFLINE,
        hashrate: [],
        temperature: [],
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerTemperature(miner.deviceIdentifier));
      expect(result.current).toBeNull();
    });

    it("returns temperature data when miner has valid hashrate data", () => {
      const hashrateData = [createMeasurement(100)];
      const temperatureData = [createMeasurement(72.5)];
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ONLINE,
        hashrate: hashrateData,
        temperature: temperatureData,
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerTemperature(miner.deviceIdentifier));
      expect(result.current).toEqual(temperatureData);
    });

    it("returns undefined when hashrate is valid but temperature has no valid measurements", () => {
      const hashrateData = [createMeasurement(100)];
      const temperatureData = [create(MeasurementSchema, {})];
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ONLINE,
        hashrate: hashrateData,
        temperature: temperatureData,
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerTemperature(miner.deviceIdentifier));
      expect(result.current).toBeUndefined();
    });
  });

  describe("edge cases", () => {
    it("handles miners with ERROR status correctly", () => {
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.ERROR,
        hashrate: [],
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      // ERROR status is not OFFLINE or INACTIVE, so should show skeleton
      const { result } = renderHook(() => useMinerHashrate(miner.deviceIdentifier));
      expect(result.current).toBeUndefined();
    });

    it("handles miners with MAINTENANCE status correctly", () => {
      const miner = createMinerSnapshot({
        deviceStatus: DeviceStatus.MAINTENANCE,
        hashrate: [],
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      // MAINTENANCE status is not OFFLINE or INACTIVE, so should show skeleton
      const { result } = renderHook(() => useMinerHashrate(miner.deviceIdentifier));
      expect(result.current).toBeUndefined();
    });
  });

  describe("useMinerModel", () => {
    it("returns the correct model when miner exists", () => {
      const miner = createMinerSnapshot({
        model: "Proto Rig",
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerModel(miner.deviceIdentifier));
      expect(result.current).toBe("Proto Rig");
    });

    it("returns undefined when miner doesn't exist", () => {
      const { result } = renderHook(() => useMinerModel("non-existent-id"));
      expect(result.current).toBeUndefined();
    });

    it("handles empty string model", () => {
      const miner = createMinerSnapshot({
        model: "",
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerModel(miner.deviceIdentifier));
      expect(result.current).toBe("");
    });

    it("returns correct model for Bitmain miners", () => {
      const miner = createMinerSnapshot({
        model: "Antminer S19",
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerModel(miner.deviceIdentifier));
      expect(result.current).toBe("Antminer S19");
    });
  });

  describe("useMinerFirmwareVersion", () => {
    it("returns the correct firmware version when miner exists", () => {
      const miner = createMinerSnapshot({
        firmwareVersion: "1.2.3",
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerFirmwareVersion(miner.deviceIdentifier));
      expect(result.current).toBe("1.2.3");
    });

    it("returns undefined when miner doesn't exist", () => {
      const { result } = renderHook(() => useMinerFirmwareVersion("non-existent-id"));
      expect(result.current).toBeUndefined();
    });

    it("handles empty string firmware version", () => {
      const miner = createMinerSnapshot({
        firmwareVersion: "",
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerFirmwareVersion(miner.deviceIdentifier));
      expect(result.current).toBe("");
    });

    it("handles date-based version format", () => {
      const miner = createMinerSnapshot({
        firmwareVersion: "2024.01.15",
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerFirmwareVersion(miner.deviceIdentifier));
      expect(result.current).toBe("2024.01.15");
    });

    it("handles semantic version with pre-release tag", () => {
      const miner = createMinerSnapshot({
        firmwareVersion: "v1.0.0-beta",
      });

      useFleetStore.setState({
        fleet: {
          ...useFleetStore.getState().fleet,
          miners: { [miner.deviceIdentifier]: miner },
        },
      });

      const { result } = renderHook(() => useMinerFirmwareVersion(miner.deviceIdentifier));
      expect(result.current).toBe("v1.0.0-beta");
    });
  });
});
