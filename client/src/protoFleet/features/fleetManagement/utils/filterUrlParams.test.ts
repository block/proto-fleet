import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { encodeFilterToURL, parseUrlToActiveFilters } from "./filterUrlParams";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

describe("filterUrlParams", () => {
  describe("encodeFilterToURL", () => {
    it("should not create duplicate status values when encoding needs-attention filter", () => {
      const filter = create(MinerListFilterSchema, {
        deviceStatus: [DeviceStatus.ERROR, DeviceStatus.NEEDS_MINING_POOL],
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
  });
});
