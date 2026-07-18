import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { buildRackPickerItem, describeRackReassignment } from "./rackPickerItem";
import { DeviceSetSchema, RackInfoSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";

// currentSiteId 42n, currentBuildingId 7n throughout.
const CURRENT_SITE = 42n;
const CURRENT_BUILDING = 7n;

const rack = (opts: { id?: bigint; label?: string; buildingId?: bigint; siteId?: bigint; deviceCount?: number }) =>
  create(DeviceSetSchema, {
    id: opts.id ?? 1n,
    label: opts.label ?? "R1",
    deviceCount: opts.deviceCount ?? 0,
    typeDetails: {
      case: "rackInfo",
      value: create(RackInfoSchema, { rows: 1, columns: 1, buildingId: opts.buildingId ?? 0n, siteId: opts.siteId }),
    },
  });

const build = (r: ReturnType<typeof rack>) => buildRackPickerItem(r, CURRENT_SITE, CURRENT_BUILDING, {});

describe("buildRackPickerItem eligibility + labels", () => {
  it("labels a rack in this building as eligible", () => {
    const item = build(rack({ buildingId: CURRENT_BUILDING, siteId: CURRENT_SITE }))!;
    expect(item.statusLabel).toBe("In this building");
    expect(item.disabled).toBe(false);
    expect(item.reassignment).toBe(false);
    expect(item.crossSite).toBe(false);
  });

  it("labels an unassigned rack as eligible", () => {
    const item = build(rack({ buildingId: 0n }))!;
    expect(item.statusLabel).toBe("Unassigned");
    expect(item.disabled).toBe(false);
    expect(item.crossSite).toBe(false);
  });

  it("labels a same-site rack in another building as a building reparent", () => {
    const item = build(rack({ buildingId: 9n, siteId: CURRENT_SITE }))!;
    expect(item.statusLabel).toBe("In another building");
    expect(item.reassignment).toBe(true);
    expect(item.crossSite).toBe(false);
  });

  it("labels a cross-site rack as a site move even when it also has a buildingId", () => {
    // All-sites mode fetches cross-site racks; these usually also carry a
    // buildingId. The site move is the higher-stakes reparent, so it must win
    // the label over "In another building".
    const item = build(rack({ buildingId: 9n, siteId: 99n }))!;
    expect(item.statusLabel).toBe("In another site");
    expect(item.reassignment).toBe(true);
    expect(item.crossSite).toBe(true);
  });
});

describe("describeRackReassignment copy", () => {
  it("describes a cross-site rack as a site move", () => {
    const item = build(rack({ label: "Rack A", buildingId: 9n, siteId: 99n, deviceCount: 3 }))!;
    const copy = describeRackReassignment(item, "North");
    expect(copy).toContain("currently in another site");
    expect(copy).toContain("its 3 miners");
    expect(copy).toContain("out of that site");
  });

  it("describes a same-site rack as a building move", () => {
    const item = build(rack({ label: "Rack B", buildingId: 9n, siteId: CURRENT_SITE, deviceCount: 1 }))!;
    const copy = describeRackReassignment(item, "North");
    expect(copy).toContain("currently in another building");
    expect(copy).toContain("its 1 miner");
    expect(copy).toContain("out of that building");
  });
});
