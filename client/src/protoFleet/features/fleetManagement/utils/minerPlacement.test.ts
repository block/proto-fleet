import { describe, expect, it } from "vitest";

import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  isMinerSnapshotIneligible,
  isPlacementIneligible,
  type MinerEligibility,
} from "@/protoFleet/features/fleetManagement/utils/minerPlacement";

// Target rack: id 1, in building 100 / site 10.
const TARGET: MinerEligibility = { rackId: 1n, buildingId: 100n, siteId: 10n };

describe("isPlacementIneligible", () => {
  it("keeps a miner already in the target rack", () => {
    expect(isPlacementIneligible({ rackId: 1n, buildingId: 100n, siteId: 10n }, TARGET)).toBe(false);
  });

  it("keeps a fully unplaced miner", () => {
    expect(isPlacementIneligible({}, TARGET)).toBe(false);
  });

  it("excludes a miner in a different rack", () => {
    expect(isPlacementIneligible({ rackId: 2n, buildingId: 100n, siteId: 10n }, TARGET)).toBe(true);
  });

  it("excludes a miner sitting directly in a different building (no rack)", () => {
    expect(isPlacementIneligible({ buildingId: 200n, siteId: 10n }, TARGET)).toBe(true);
  });

  it("excludes a miner in a different site", () => {
    expect(isPlacementIneligible({ siteId: 20n }, TARGET)).toBe(true);
  });

  it("does not confuse a same-id rack in the target with a different rack (id-based)", () => {
    // Label-based checks would collide here; id-based keeps them distinct.
    expect(isPlacementIneligible({ rackId: 1n }, TARGET)).toBe(false);
    expect(isPlacementIneligible({ rackId: 99n }, TARGET)).toBe(true);
  });

  describe("new rack (no target ids)", () => {
    const NEW_RACK: MinerEligibility = {};

    it("excludes every already-racked miner", () => {
      expect(isPlacementIneligible({ rackId: 5n }, NEW_RACK)).toBe(true);
    });

    it("flags an unracked-but-placed miner (an unplaced rack strips its building/site on assign)", () => {
      expect(isPlacementIneligible({ buildingId: 7n }, NEW_RACK)).toBe(true);
      expect(isPlacementIneligible({ siteId: 8n }, NEW_RACK)).toBe(true);
    });

    it("keeps a fully unplaced miner", () => {
      expect(isPlacementIneligible({}, NEW_RACK)).toBe(false);
    });
  });

  it("flags a miner whose site the target lacks (rack placed under a site with no building)", () => {
    // Target under site 10, no building. A miner directly in building 200 (same
    // site) has its building stripped on assign, so it's a reassignment.
    const SITE_ONLY: MinerEligibility = { rackId: 1n, siteId: 10n };
    expect(isPlacementIneligible({ buildingId: 200n, siteId: 10n }, SITE_ONLY)).toBe(true);
    expect(isPlacementIneligible({ siteId: 10n }, SITE_ONLY)).toBe(false);
  });
});

describe("isMinerSnapshotIneligible", () => {
  const snapshot = (placement: { rack?: bigint; building?: bigint; site?: bigint }): MinerStateSnapshot =>
    ({
      placement: {
        rack: placement.rack !== undefined ? { id: placement.rack, label: "" } : undefined,
        building: placement.building !== undefined ? { id: placement.building, label: "" } : undefined,
        site: placement.site !== undefined ? { id: placement.site, label: "" } : undefined,
        groups: [],
      },
    }) as unknown as MinerStateSnapshot;

  it("reads placement refs off the snapshot", () => {
    expect(isMinerSnapshotIneligible(snapshot({ rack: 1n, building: 100n, site: 10n }), TARGET)).toBe(false);
    expect(isMinerSnapshotIneligible(snapshot({ rack: 2n }), TARGET)).toBe(true);
    expect(isMinerSnapshotIneligible(snapshot({}), TARGET)).toBe(false);
  });

  it("treats a zero id (proto default) as unassigned", () => {
    expect(isMinerSnapshotIneligible(snapshot({ rack: 0n }), TARGET)).toBe(false);
  });
});
