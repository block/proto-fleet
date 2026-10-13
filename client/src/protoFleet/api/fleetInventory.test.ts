import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { miningInventoryRequest } from "./fleetInventory";
import { EntityKind, EntityRefSchema } from "./generated/fleetinventory/v1/fleetinventory_pb";
import { DeviceStatus, MinerListFilterSchema, NumericField } from "./generated/fleetmanagement/v1/fleetmanagement_pb";

describe("miningInventoryRequest", () => {
  it("uses typed facility and group scopes without making placement miner-only", () => {
    const filter = create(MinerListFilterSchema, {
      buildingIds: [7n],
      rackIds: [7n],
      includeNoBuilding: true,
      includeNoRack: true,
    });
    const request = miningInventoryRequest(filter, undefined, [
      create(EntityRefSchema, { kind: EntityKind.CONTAINER, id: 7n }),
    ]);
    expect(request.minerFilter).toBeUndefined();
    expect(request.ancestors).toMatchObject([
      { kind: EntityKind.BUILDING, id: 7n },
      { kind: EntityKind.RACK, id: 7n },
      { kind: EntityKind.CONTAINER, id: 7n },
    ]);
    expect(request.includeNoFacility).toBe(true);
    expect(request.includeNoGroup).toBe(true);
    expect(filter.buildingIds).toEqual([7n]);
    expect(filter.includeNoBuilding).toBe(true);
  });
  it("keeps miner-only filters independent of combined placement scopes", () => {
    const request = miningInventoryRequest(
      create(MinerListFilterSchema, { models: ["S21"], buildingIds: [7n], rackIds: [8n], includeNoRack: true }),
      undefined,
      [create(EntityRefSchema, { kind: EntityKind.TANK, id: 8n })],
    );
    expect(request.minerFilter).toMatchObject({ models: ["S21"], buildingIds: [], rackIds: [], includeNoRack: false });
    expect(request.ancestors).toHaveLength(3);
    expect(request.includeNoGroup).toBe(true);
  });
  it.each([
    { buildingIds: [7n], includeNoRack: true },
    { buildingIds: [7n], rackIds: [8n], includeNoBuilding: true, includeNoRack: true },
    { includeNoBuilding: true, includeNoRack: true },
  ])("preserves native placement semantics for conventional-only filters (case %#)", (placement) => {
    const filter = create(MinerListFilterSchema, { ...placement, siteIds: [4n], searchQuery: "rig" });
    const request = miningInventoryRequest(filter);
    expect(request.minerFilter).toMatchObject({ ...filter, pairingStatuses: expect.any(Array) });
    expect(request.minerFilter?.pairingStatuses).not.toHaveLength(0);
    expect(request.ancestors).toEqual([]);
    expect(request.includeNoFacility).toBe(false);
    expect(request.includeNoGroup).toBe(false);
  });
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
