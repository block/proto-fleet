import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { miningInventoryRequest } from "./fleetInventory";
import { DeviceStatus, MinerListFilterSchema, NumericField } from "./generated/fleetmanagement/v1/fleetmanagement_pb";

describe("miningInventoryRequest", () => {
  it("keeps common search, site, and telemetry filters applicable to modules", () => {
    const request = miningInventoryRequest(
      create(MinerListFilterSchema, {
        siteIds: [4n],
        searchQuery: "rig",
        includeUnassigned: true,
        numericRanges: [{ field: NumericField.POWER_KW, min: 0, minInclusive: true }],
      }),
    );
    expect(request.minerFilter).toBeUndefined();
    expect(request.siteIds).toEqual([4n]);
    expect(request.searchQuery).toBe("rig");
    expect(request.telemetryRanges).toEqual([
      { field: 3, min: 0, max: undefined, minInclusive: true, maxInclusive: false },
    ]);
  });
  it("preserves the complete miner filter when a miner-only field is selected", () => {
    const request = miningInventoryRequest(
      create(MinerListFilterSchema, {
        models: ["S21"],
        siteIds: [4n],
        searchQuery: "192.0.2",
        ipCidrs: ["192.0.2.0/24"],
      }),
    );
    expect(request.minerFilter).toMatchObject({
      models: ["S21"],
      siteIds: [4n],
      searchQuery: "192.0.2",
      ipCidrs: ["192.0.2.0/24"],
    });
    expect(request.minerFilter?.pairingStatuses).not.toHaveLength(0);
  });
  it.each([DeviceStatus.ONLINE, DeviceStatus.OFFLINE, DeviceStatus.UPDATING])(
    "preserves native miner status semantics (%s)",
    (status) => {
      const request = miningInventoryRequest(create(MinerListFilterSchema, { deviceStatus: [status] }));
      expect(request.minerFilter?.deviceStatus).toEqual([status]);
      expect(request.states).toBeUndefined();
    },
  );
});
