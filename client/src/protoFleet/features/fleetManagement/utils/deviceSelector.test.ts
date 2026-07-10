import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { createDeviceSelector } from "./deviceSelector";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

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

    it("returns allMatchingFilter when a minerListFilter is provided", () => {
      const filter = create(MinerListFilterSchema, { rackIds: [7n], models: ["S19"] });
      const result = createDeviceSelector("all", [], undefined, filter);

      expect(result.selectionType.case).toBe("allMatchingFilter");
      if (result.selectionType.case === "allMatchingFilter") {
        expect(result.selectionType.value.rackIds).toEqual([7n]);
        expect(result.selectionType.value.models).toEqual(["S19"]);
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
