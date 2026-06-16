import { describe, expect, it } from "vitest";
import { createDeviceSelector } from "./deviceSelector";
import { DeviceStatus, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

describe("createDeviceSelector", () => {
  describe("when selectionMode is 'all'", () => {
    it("returns DeviceSelector with allDevices filter (no criteria)", () => {
      const result = createDeviceSelector("all", ["device-1", "device-2"]);

      expect(result.selectionType.case).toBe("allDevices");
      if (result.selectionType.case === "allDevices") {
        expect(result.selectionType.value).toBeDefined();
        expect(result.selectionType.value.deviceStatus).toEqual([]);
        expect(result.selectionType.value.pairingStatus).toEqual([]);
      }
    });

    it("ignores deviceIdentifiers when mode is 'all'", () => {
      const result = createDeviceSelector("all", []);

      expect(result.selectionType.case).toBe("allDevices");
      if (result.selectionType.case === "allDevices") {
        expect(result.selectionType.value).toBeDefined();
      }
    });

    it("includes filter criteria for all-device selectors", () => {
      const result = createDeviceSelector("all", ["ignored-device"], {
        deviceStatuses: [DeviceStatus.NEEDS_MINING_POOL],
        pairingStatuses: [PairingStatus.DEFAULT_PASSWORD],
        models: ["Rig"],
        manufacturers: ["Proto"],
      });

      expect(result.selectionType.case).toBe("allDevices");
      if (result.selectionType.case === "allDevices") {
        expect(result.selectionType.value?.deviceStatus).toEqual([DeviceStatus.NEEDS_MINING_POOL]);
        expect(result.selectionType.value?.pairingStatus).toEqual([PairingStatus.DEFAULT_PASSWORD]);
        expect(result.selectionType.value?.models).toEqual(["Rig"]);
        expect(result.selectionType.value?.manufacturers).toEqual(["Proto"]);
      }
    });
  });

  describe("when selectionMode is 'subset'", () => {
    it("returns DeviceSelector with includeDevices containing device identifiers", () => {
      const deviceIdentifiers = ["device-1", "device-2", "device-3"];
      const result = createDeviceSelector("subset", deviceIdentifiers);

      expect(result.selectionType.case).toBe("includeDevices");
      if (result.selectionType.case === "includeDevices") {
        expect(result.selectionType.value?.deviceIdentifiers).toEqual(deviceIdentifiers);
      }
    });

    it("returns empty includeDevices when no devices provided", () => {
      const result = createDeviceSelector("subset", []);

      expect(result.selectionType.case).toBe("includeDevices");
      if (result.selectionType.case === "includeDevices") {
        expect(result.selectionType.value?.deviceIdentifiers).toEqual([]);
      }
    });
  });

  describe("when selectionMode is 'none'", () => {
    it("throws an error", () => {
      expect(() => createDeviceSelector("none", [])).toThrow("Cannot create DeviceSelector with no selection");
    });
  });
});
