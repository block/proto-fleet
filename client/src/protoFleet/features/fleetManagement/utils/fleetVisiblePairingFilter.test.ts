import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  applyFleetSelectablePairingStatuses,
  applyFleetVisiblePairingStatuses,
  FLEET_SELECTABLE_PAIRING_STATUSES,
  FLEET_VISIBLE_PAIRING_STATUSES,
  isFleetSelectablePairingStatus,
} from "./fleetVisiblePairingFilter";
import {
  type MinerListFilter,
  MinerListFilterSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

describe("applyFleetVisiblePairingStatuses", () => {
  it("defaults to the fleet-visible pairing statuses when the filter is undefined", () => {
    expect(applyFleetVisiblePairingStatuses().pairingStatuses).toEqual([...FLEET_VISIBLE_PAIRING_STATUSES]);
  });

  it("preserves existing visible pairing statuses", () => {
    const filter: MinerListFilter = create(MinerListFilterSchema, {
      pairingStatuses: [PairingStatus.AUTHENTICATION_NEEDED, PairingStatus.DEFAULT_PASSWORD],
    });

    expect(applyFleetVisiblePairingStatuses(filter).pairingStatuses).toEqual([
      PairingStatus.AUTHENTICATION_NEEDED,
      PairingStatus.DEFAULT_PASSWORD,
    ]);
  });

  it("filters out non-visible pairing statuses", () => {
    const filter: MinerListFilter = create(MinerListFilterSchema, {
      pairingStatuses: [PairingStatus.PAIRED, PairingStatus.DEFAULT_PASSWORD, PairingStatus.PENDING],
    });

    expect(applyFleetVisiblePairingStatuses(filter).pairingStatuses).toEqual([
      PairingStatus.PAIRED,
      PairingStatus.DEFAULT_PASSWORD,
    ]);
  });

  it("preserves an empty intersection when an explicit filter contains no visible statuses", () => {
    const filter: MinerListFilter = create(MinerListFilterSchema, {
      pairingStatuses: [PairingStatus.PENDING],
    });

    expect(applyFleetVisiblePairingStatuses(filter).pairingStatuses).toEqual([]);
  });
});

describe("applyFleetSelectablePairingStatuses", () => {
  it("defaults to the fleet-selectable pairing statuses when the filter is undefined", () => {
    expect(applyFleetSelectablePairingStatuses().pairingStatuses).toEqual([...FLEET_SELECTABLE_PAIRING_STATUSES]);
  });

  it("filters out non-selectable pairing statuses", () => {
    const filter: MinerListFilter = create(MinerListFilterSchema, {
      pairingStatuses: [PairingStatus.PAIRED, PairingStatus.AUTHENTICATION_NEEDED, PairingStatus.DEFAULT_PASSWORD],
    });

    expect(applyFleetSelectablePairingStatuses(filter).pairingStatuses).toEqual([PairingStatus.PAIRED]);
  });

  it("preserves an empty selectable intersection for explicit non-selectable filters", () => {
    const filter: MinerListFilter = create(MinerListFilterSchema, {
      pairingStatuses: [PairingStatus.AUTHENTICATION_NEEDED],
    });

    expect(applyFleetSelectablePairingStatuses(filter).pairingStatuses).toEqual([]);
  });

  it("copies active fleet filters through so bulk actions respect them", () => {
    const filter: MinerListFilter = create(MinerListFilterSchema, {
      firmwareVersions: ["v3.5.1"],
      zones: ["Austin, Building 1"],
      siteIds: [1n],
      buildingIds: [2n],
      rackIds: [3n],
      groupIds: [4n],
      ipCidrs: ["172.16.0.0/16"],
      includeUnassigned: true,
      includeNoBuilding: true,
      includeNoRack: true,
    });

    const result = applyFleetSelectablePairingStatuses(filter);
    expect(result.firmwareVersions).toEqual(["v3.5.1"]);
    expect(result.zones).toEqual(["Austin, Building 1"]);
    expect(result.siteIds).toEqual([1n]);
    expect(result.buildingIds).toEqual([2n]);
    expect(result.rackIds).toEqual([3n]);
    expect(result.groupIds).toEqual([4n]);
    expect(result.ipCidrs).toEqual(["172.16.0.0/16"]);
    expect(result.includeUnassigned).toBe(true);
    expect(result.includeNoBuilding).toBe(true);
    expect(result.includeNoRack).toBe(true);
  });
});

describe("isFleetSelectablePairingStatus", () => {
  it("returns true only for pairing statuses that can be selected in the miner list", () => {
    expect(isFleetSelectablePairingStatus(PairingStatus.PAIRED)).toBe(true);
    expect(isFleetSelectablePairingStatus(PairingStatus.AUTHENTICATION_NEEDED)).toBe(false);
    expect(isFleetSelectablePairingStatus(PairingStatus.DEFAULT_PASSWORD)).toBe(false);
  });
});
