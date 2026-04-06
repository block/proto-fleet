import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { encodeFilterToURL, parseFilterFromURL, parseUrlToActiveFilters } from "./filterUrlParams";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

describe("filterUrlParams", () => {
  describe("encodeFilterToURL", () => {
    it("should not create duplicate status values when encoding needs-attention filter", () => {
      const filter = create(MinerListFilterSchema, {
        deviceStatus: [
          DeviceStatus.ERROR,
          DeviceStatus.NEEDS_MINING_POOL,
          DeviceStatus.UPDATING,
          DeviceStatus.REBOOT_REQUIRED,
        ],
      });

      const params = encodeFilterToURL(filter);
      const statusParam = params.get("status");

      expect(statusParam).toBe("needs-attention");
      expect(statusParam?.split(",").length).toBe(1);
    });

    it("should handle multiple different status values correctly", () => {
      const filter = create(MinerListFilterSchema, {
        deviceStatus: [DeviceStatus.ONLINE, DeviceStatus.ERROR, DeviceStatus.OFFLINE],
      });

      const params = encodeFilterToURL(filter);
      const statusParam = params.get("status");

      const statusValues = statusParam?.split(",").sort();
      expect(statusValues).toEqual(["hashing", "needs-attention", "offline"]);
    });
  });

  describe("parseUrlToActiveFilters", () => {
    it("should deduplicate status values from URL", () => {
      const params = new URLSearchParams("status=needs-attention,needs-attention");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.status?.length).toBe(1);
    });

    it("should deduplicate issue values from URL", () => {
      const params = new URLSearchParams("issues=control-board,control-board,fan,fan");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.issues?.length).toBe(2);
      expect(activeFilters.dropdownFilters.issues).toContain("control-board");
      expect(activeFilters.dropdownFilters.issues).toContain("fan");
    });

    it("should deduplicate model values from URL", () => {
      const params = new URLSearchParams("model=Proto Rig,Proto Rig");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.model?.length).toBe(1);
    });

    it("should parse valid group IDs from URL", () => {
      const params = new URLSearchParams("group=1,2,3");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.group).toEqual(["1", "2", "3"]);
    });

    it("should deduplicate group values from URL", () => {
      const params = new URLSearchParams("group=1,1,2,2");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.group).toEqual(["1", "2"]);
    });

    it("should filter out empty group values from URL", () => {
      const params = new URLSearchParams("group=1,,2,");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.group).toEqual(["1", "2"]);
    });

    it("should filter out non-numeric group values from URL", () => {
      const params = new URLSearchParams("group=1,abc,2,xyz");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.group).toEqual(["1", "2"]);
    });

    it("should not set group filter when all values are invalid", () => {
      const params = new URLSearchParams("group=abc,,xyz");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.group).toBeUndefined();
    });
  });

  describe("encodeFilterToURL - group IDs", () => {
    it("should encode group IDs to URL params", () => {
      const filter = create(MinerListFilterSchema, {
        groupIds: [1n, 2n, 3n],
      });

      const params = encodeFilterToURL(filter);

      expect(params.get("group")).toBe("1,2,3");
    });

    it("should not set group param when no group IDs", () => {
      const filter = create(MinerListFilterSchema, {});

      const params = encodeFilterToURL(filter);

      expect(params.has("group")).toBe(false);
    });
  });

  describe("parseFilterFromURL - group IDs", () => {
    it("should parse valid group IDs into BigInt values", () => {
      const params = new URLSearchParams("group=1,2,3");
      const filter = parseFilterFromURL(params);

      expect(filter?.groupIds).toEqual([1n, 2n, 3n]);
    });

    it("should skip empty group ID values", () => {
      const params = new URLSearchParams("group=1,,3");
      const filter = parseFilterFromURL(params);

      expect(filter?.groupIds).toEqual([1n, 3n]);
    });

    it("should skip non-numeric group ID values without throwing", () => {
      const params = new URLSearchParams("group=abc,1,xyz,2");
      const filter = parseFilterFromURL(params);

      expect(filter?.groupIds).toEqual([1n, 2n]);
    });

    it("should handle group param with only invalid values", () => {
      const params = new URLSearchParams("group=abc");
      const filter = parseFilterFromURL(params);

      expect(filter?.groupIds).toEqual([]);
    });

    it("should return undefined when no filter params present", () => {
      const params = new URLSearchParams();
      const filter = parseFilterFromURL(params);

      expect(filter).toBeUndefined();
    });
  });

  describe("parseFilterFromURL - needs attention", () => {
    it("should expand needs-attention URL state to all attention statuses", () => {
      const params = new URLSearchParams("status=needs-attention");
      const filter = parseFilterFromURL(params);

      expect(filter?.deviceStatus).toEqual([
        DeviceStatus.ERROR,
        DeviceStatus.NEEDS_MINING_POOL,
        DeviceStatus.UPDATING,
        DeviceStatus.REBOOT_REQUIRED,
      ]);
    });
  });

  describe("parseUrlToActiveFilters - rack IDs", () => {
    it("should parse valid rack IDs from URL", () => {
      const params = new URLSearchParams("rack=10,20,30");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.rack).toEqual(["10", "20", "30"]);
    });

    it("should deduplicate rack values from URL", () => {
      const params = new URLSearchParams("rack=5,5,6,6");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.rack).toEqual(["5", "6"]);
    });

    it("should filter out empty rack values from URL", () => {
      const params = new URLSearchParams("rack=1,,2,");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.rack).toEqual(["1", "2"]);
    });

    it("should filter out non-numeric rack values from URL", () => {
      const params = new URLSearchParams("rack=1,abc,2,xyz");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.rack).toEqual(["1", "2"]);
    });

    it("should not set rack filter when all values are invalid", () => {
      const params = new URLSearchParams("rack=abc,,xyz");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.rack).toBeUndefined();
    });
  });

  describe("encodeFilterToURL - rack IDs", () => {
    it("should encode rack IDs to URL params", () => {
      const filter = create(MinerListFilterSchema, {
        rackIds: [10n, 20n, 30n],
      });

      const params = encodeFilterToURL(filter);

      expect(params.get("rack")).toBe("10,20,30");
    });

    it("should not set rack param when no rack IDs", () => {
      const filter = create(MinerListFilterSchema, {});

      const params = encodeFilterToURL(filter);

      expect(params.has("rack")).toBe(false);
    });
  });

  describe("parseFilterFromURL - rack IDs", () => {
    it("should parse valid rack IDs into BigInt values", () => {
      const params = new URLSearchParams("rack=10,20,30");
      const filter = parseFilterFromURL(params);

      expect(filter?.rackIds).toEqual([10n, 20n, 30n]);
    });

    it("should skip empty rack ID values", () => {
      const params = new URLSearchParams("rack=1,,3");
      const filter = parseFilterFromURL(params);

      expect(filter?.rackIds).toEqual([1n, 3n]);
    });

    it("should skip non-numeric rack ID values without throwing", () => {
      const params = new URLSearchParams("rack=abc,1,xyz,2");
      const filter = parseFilterFromURL(params);

      expect(filter?.rackIds).toEqual([1n, 2n]);
    });

    it("should handle rack param with only invalid values", () => {
      const params = new URLSearchParams("rack=abc");
      const filter = parseFilterFromURL(params);

      expect(filter?.rackIds).toEqual([]);
    });
  });
});
