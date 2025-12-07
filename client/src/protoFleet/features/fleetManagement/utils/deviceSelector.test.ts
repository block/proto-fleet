import { describe, expect, it } from "vitest";
import { createDeviceSelector } from "./deviceSelector";

describe("createDeviceSelector", () => {
  describe("when selectionMode is 'all'", () => {
    it("returns DeviceSelector with allDevices set to true", () => {
      const result = createDeviceSelector("all", ["device-1", "device-2"]);

      expect(result.selectionType.case).toBe("allDevices");
      expect(result.selectionType.value).toBe(true);
    });

    it("ignores deviceIdentifiers when mode is 'all'", () => {
      const result = createDeviceSelector("all", []);

      expect(result.selectionType.case).toBe("allDevices");
      expect(result.selectionType.value).toBe(true);
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
