import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { encodeFilterToURL, parseFilterFromURL, parseUrlToActiveFilters } from "./filterUrlParams";
import {
  MinerListFilterSchema,
  NumericField,
  NumericRangeFilterSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
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

      expect(params.getAll("status")).toEqual(["needs-attention"]);
    });

    it("should handle multiple different status values correctly", () => {
      const filter = create(MinerListFilterSchema, {
        deviceStatus: [DeviceStatus.ONLINE, DeviceStatus.ERROR, DeviceStatus.OFFLINE],
      });

      const params = encodeFilterToURL(filter);

      expect(params.getAll("status").sort()).toEqual(["hashing", "needs-attention", "offline"]);
    });
  });

  describe("parseUrlToActiveFilters", () => {
    it("should deduplicate status values from URL", () => {
      const params = new URLSearchParams("status=needs-attention&status=needs-attention");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.status?.length).toBe(1);
    });

    it("should deduplicate issue values from URL", () => {
      const params = new URLSearchParams("issues=control-board&issues=control-board&issues=fan&issues=fan");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.issues?.length).toBe(2);
      expect(activeFilters.dropdownFilters.issues).toContain("control-board");
      expect(activeFilters.dropdownFilters.issues).toContain("fan");
    });

    it("should deduplicate model values from URL", () => {
      const params = new URLSearchParams("model=Proto+Rig&model=Proto+Rig");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.model).toEqual(["Proto Rig"]);
    });

    it("should parse valid group IDs from URL", () => {
      const params = new URLSearchParams("group=1&group=2&group=3");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.group).toEqual(["1", "2", "3"]);
    });

    it("should deduplicate group values from URL", () => {
      const params = new URLSearchParams("group=1&group=1&group=2&group=2");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.group).toEqual(["1", "2"]);
    });

    it("should filter out empty group values from URL", () => {
      const params = new URLSearchParams("group=1&group=&group=2&group=");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.group).toEqual(["1", "2"]);
    });

    it("should filter out non-numeric group values from URL", () => {
      const params = new URLSearchParams("group=1&group=abc&group=2&group=xyz");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.group).toEqual(["1", "2"]);
    });

    it("should not set group filter when all values are invalid", () => {
      const params = new URLSearchParams("group=abc&group=&group=xyz");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.group).toBeUndefined();
    });

    it("accepts the legacy comma-joined URL format for backward compatibility", () => {
      const params = new URLSearchParams("group=1,2,3");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.group).toEqual(["1", "2", "3"]);
    });
  });

  describe("encodeFilterToURL - group IDs", () => {
    it("should encode group IDs to URL params", () => {
      const filter = create(MinerListFilterSchema, {
        groupIds: [1n, 2n, 3n],
      });

      const params = encodeFilterToURL(filter);

      expect(params.getAll("group")).toEqual(["1", "2", "3"]);
    });

    it("should not set group param when no group IDs", () => {
      const filter = create(MinerListFilterSchema, {});

      const params = encodeFilterToURL(filter);

      expect(params.has("group")).toBe(false);
    });
  });

  describe("parseFilterFromURL - group IDs", () => {
    it("should parse valid group IDs into BigInt values", () => {
      const params = new URLSearchParams("group=1&group=2&group=3");
      const filter = parseFilterFromURL(params);

      expect(filter?.groupIds).toEqual([1n, 2n, 3n]);
    });

    it("should skip empty group ID values", () => {
      const params = new URLSearchParams("group=1&group=&group=3");
      const filter = parseFilterFromURL(params);

      expect(filter?.groupIds).toEqual([1n, 3n]);
    });

    it("should skip non-numeric group ID values without throwing", () => {
      const params = new URLSearchParams("group=abc&group=1&group=xyz&group=2");
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

  describe("sleeping <-> {INACTIVE, MAINTENANCE}", () => {
    it("encodes INACTIVE and MAINTENANCE to a single sleeping status", () => {
      const filter = create(MinerListFilterSchema, {
        deviceStatus: [DeviceStatus.INACTIVE, DeviceStatus.MAINTENANCE],
      });

      const params = encodeFilterToURL(filter);

      expect(params.getAll("status")).toEqual(["sleeping"]);
    });

    it("encodes MAINTENANCE alone to sleeping", () => {
      const filter = create(MinerListFilterSchema, {
        deviceStatus: [DeviceStatus.MAINTENANCE],
      });

      const params = encodeFilterToURL(filter);

      expect(params.getAll("status")).toEqual(["sleeping"]);
    });

    it("parses sleeping URL state into both INACTIVE and MAINTENANCE", () => {
      const params = new URLSearchParams("status=sleeping");

      const filter = parseFilterFromURL(params);

      expect(filter?.deviceStatus).toEqual([DeviceStatus.INACTIVE, DeviceStatus.MAINTENANCE]);
    });
  });

  describe("parseUrlToActiveFilters - rack IDs", () => {
    it("should parse valid rack IDs from URL", () => {
      const params = new URLSearchParams("rack=10&rack=20&rack=30");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.rack).toEqual(["10", "20", "30"]);
    });

    it("should deduplicate rack values from URL", () => {
      const params = new URLSearchParams("rack=5&rack=5&rack=6&rack=6");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.rack).toEqual(["5", "6"]);
    });

    it("should filter out empty rack values from URL", () => {
      const params = new URLSearchParams("rack=1&rack=&rack=2&rack=");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.rack).toEqual(["1", "2"]);
    });

    it("should filter out non-numeric rack values from URL", () => {
      const params = new URLSearchParams("rack=1&rack=abc&rack=2&rack=xyz");
      const activeFilters = parseUrlToActiveFilters(params);

      expect(activeFilters.dropdownFilters.rack).toEqual(["1", "2"]);
    });

    it("should not set rack filter when all values are invalid", () => {
      const params = new URLSearchParams("rack=abc&rack=&rack=xyz");
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

      expect(params.getAll("rack")).toEqual(["10", "20", "30"]);
    });

    it("should not set rack param when no rack IDs", () => {
      const filter = create(MinerListFilterSchema, {});

      const params = encodeFilterToURL(filter);

      expect(params.has("rack")).toBe(false);
    });
  });

  describe("parseFilterFromURL - rack IDs", () => {
    it("should parse valid rack IDs into BigInt values", () => {
      const params = new URLSearchParams("rack=10&rack=20&rack=30");
      const filter = parseFilterFromURL(params);

      expect(filter?.rackIds).toEqual([10n, 20n, 30n]);
    });

    it("should skip empty rack ID values", () => {
      const params = new URLSearchParams("rack=1&rack=&rack=3");
      const filter = parseFilterFromURL(params);

      expect(filter?.rackIds).toEqual([1n, 3n]);
    });

    it("should skip non-numeric rack ID values without throwing", () => {
      const params = new URLSearchParams("rack=abc&rack=1&rack=xyz&rack=2");
      const filter = parseFilterFromURL(params);

      expect(filter?.rackIds).toEqual([1n, 2n]);
    });

    it("should handle rack param with only invalid values", () => {
      const params = new URLSearchParams("rack=abc");
      const filter = parseFilterFromURL(params);

      expect(filter?.rackIds).toEqual([]);
    });
  });

  describe("firmware versions", () => {
    it("encodes firmware versions as repeated URL params", () => {
      const filter = create(MinerListFilterSchema, {
        firmwareVersions: ["v3.5.2", "v3.5.1"],
      });

      const params = encodeFilterToURL(filter);

      // sorted on encode for stable output
      expect(params.getAll("firmware")).toEqual(["v3.5.1", "v3.5.2"]);
    });

    it("does not set firmware param when none selected", () => {
      const filter = create(MinerListFilterSchema, {});

      expect(encodeFilterToURL(filter).has("firmware")).toBe(false);
    });

    it("parses firmware versions into the filter", () => {
      const params = new URLSearchParams("firmware=v3.5.1&firmware=v3.5.2");

      expect(parseFilterFromURL(params)?.firmwareVersions).toEqual(["v3.5.1", "v3.5.2"]);
    });

    it("surfaces firmware in ActiveFilters", () => {
      const params = new URLSearchParams("firmware=v3.5.1&firmware=v3.5.2");

      expect(parseUrlToActiveFilters(params).dropdownFilters.firmware).toEqual(["v3.5.1", "v3.5.2"]);
    });
  });

  describe("zones", () => {
    it("encodes zones as repeated URL params", () => {
      const filter = create(MinerListFilterSchema, {
        zones: ["building-b", "Austin, Building 1"],
      });

      const params = encodeFilterToURL(filter);

      // sorted on encode for stable output
      expect(params.getAll("zone")).toEqual(["Austin, Building 1", "building-b"]);
    });

    it("round-trips a zone whose name contains a comma and spaces", () => {
      const filter = create(MinerListFilterSchema, {
        zones: ["Austin, Building 1"],
      });

      const params = encodeFilterToURL(filter);
      // Reconstruct via the URL string the browser actually emits/consumes,
      // so we verify the encoding survives serialization.
      const reparsed = parseFilterFromURL(new URLSearchParams(params.toString()));

      expect(reparsed?.zones).toEqual(["Austin, Building 1"]);
    });

    it("does not split a zone name on its embedded comma", () => {
      const params = new URLSearchParams();
      params.append("zone", "Austin, Building 1");

      expect(parseFilterFromURL(params)?.zones).toEqual(["Austin, Building 1"]);
    });

    it("surfaces zones in ActiveFilters", () => {
      const params = new URLSearchParams("zone=Austin%2C%20Building%201&zone=building-b");

      expect(parseUrlToActiveFilters(params).dropdownFilters.zone).toEqual(["Austin, Building 1", "building-b"]);
    });

    it("does not set zone param when none selected", () => {
      const filter = create(MinerListFilterSchema, {});

      expect(encodeFilterToURL(filter).has("zone")).toBe(false);
    });
  });

  describe("numeric range filters", () => {
    it("encodes both min and max for the matching telemetry field", () => {
      const filter = create(MinerListFilterSchema, {
        numericRanges: [
          create(NumericRangeFilterSchema, {
            field: NumericField.HASHRATE_THS,
            min: 90,
            max: 110,
            minInclusive: true,
            maxInclusive: true,
          }),
        ],
      });

      const params = encodeFilterToURL(filter);
      expect(params.get("hashrate_min")).toBe("90");
      expect(params.get("hashrate_max")).toBe("110");
    });

    it("encodes only min when max is unbounded", () => {
      const filter = create(MinerListFilterSchema, {
        numericRanges: [create(NumericRangeFilterSchema, { field: NumericField.POWER_KW, min: 2 })],
      });
      const params = encodeFilterToURL(filter);
      expect(params.get("power_min")).toBe("2");
      expect(params.has("power_max")).toBe(false);
    });

    it("round-trips a numeric filter through ActiveFilters", () => {
      const params = new URLSearchParams("hashrate_min=90&hashrate_max=110");
      const active = parseUrlToActiveFilters(params);
      expect(active.numericFilters.hashrate).toEqual({ min: 90, max: 110 });
    });

    it("drops malformed numeric values from the URL", () => {
      const params = new URLSearchParams("hashrate_min=abc&power_max=Infinity");
      const active = parseUrlToActiveFilters(params);
      expect(active.numericFilters.hashrate).toBeUndefined();
      expect(active.numericFilters.power).toBeUndefined();
    });
  });

  describe("subnet (textareaList) filter", () => {
    it("encodes ip_cidrs as repeated subnet entries, sorted", () => {
      const filter = create(MinerListFilterSchema, {
        ipCidrs: ["192.168.1.0/24", "10.0.0.0/8"],
      });
      expect(encodeFilterToURL(filter).getAll("subnet")).toEqual(["10.0.0.0/8", "192.168.1.0/24"]);
    });

    it("round-trips valid CIDRs into ActiveFilters textareaListFilters.subnet", () => {
      const params = new URLSearchParams("subnet=192.168.1.0%2F24&subnet=10.0.0.0%2F8");
      const active = parseUrlToActiveFilters(params);
      expect(active.textareaListFilters.subnet).toEqual(["192.168.1.0/24", "10.0.0.0/8"]);
    });

    it("normalizes a non-canonical CIDR from the URL", () => {
      const params = new URLSearchParams("subnet=192.168.1.5%2F24");
      const active = parseUrlToActiveFilters(params);
      expect(active.textareaListFilters.subnet).toEqual(["192.168.1.0/24"]);
    });

    it("accepts IPv6 CIDRs and bare IPv6 addresses from the URL", () => {
      const params = new URLSearchParams("subnet=2001%3Adb8%3A%3A%2F64&subnet=2001%3Adb8%3A%3A1");
      const active = parseUrlToActiveFilters(params);
      expect(active.textareaListFilters.subnet).toEqual(["2001:db8::/64", "2001:db8::1/128"]);
    });

    it("parses IPv6 CIDRs and bare IPv6 addresses into the protobuf filter", () => {
      const params = new URLSearchParams("subnet=2001%3Adb8%3A%3A%2F64&subnet=2001%3Adb8%3A%3A1");
      const filter = parseFilterFromURL(params);
      expect(filter?.ipCidrs).toEqual(["2001:db8::/64", "2001:db8::1/128"]);
    });

    it("silently drops invalid CIDRs from the URL", () => {
      const params = new URLSearchParams("subnet=garbage&subnet=10.0.0.0%2F8&subnet=fe80%3A%3A%2F64");
      const active = parseUrlToActiveFilters(params);
      expect(active.textareaListFilters.subnet).toEqual(["10.0.0.0/8"]);
    });
  });
});
