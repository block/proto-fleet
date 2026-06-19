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
  NumericField,
  NumericRangeFilterSchema,
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

    expect(applyFleetSelectablePairingStatuses(filter).pairingStatuses).toEqual([
      PairingStatus.PAIRED,
      PairingStatus.DEFAULT_PASSWORD,
    ]);
  });

  it("preserves an empty selectable intersection for explicit non-selectable filters", () => {
    const filter: MinerListFilter = create(MinerListFilterSchema, {
      pairingStatuses: [PairingStatus.AUTHENTICATION_NEEDED],
    });

    expect(applyFleetSelectablePairingStatuses(filter).pairingStatuses).toEqual([]);
  });

  it("preserves every active filter field and only changes pairing statuses", () => {
    const filter: MinerListFilter = create(MinerListFilterSchema, {
      deviceStatus: [1],
      errorComponentTypes: [1],
      models: ["Rig"],
      pairingStatuses: [PairingStatus.AUTHENTICATION_NEEDED, PairingStatus.DEFAULT_PASSWORD],
      groupIds: [101n],
      rackIds: [202n],
      firmwareVersions: ["v3.5.1"],
      zones: ["Austin, Building 1"],
      numericRanges: [
        create(NumericRangeFilterSchema, {
          field: NumericField.HASHRATE_THS,
          min: 90,
          max: 110,
          minInclusive: true,
          maxInclusive: true,
        }),
      ],
      ipCidrs: ["192.168.1.0/24"],
      siteIds: [303n],
      includeUnassigned: true,
      buildingIds: [404n],
      includeNoBuilding: true,
      zoneKeys: [{ buildingId: 404n, zone: "A1" }],
      includeNoRack: true,
    });

    const result = applyFleetSelectablePairingStatuses(filter);
    expect(result).toMatchObject({
      deviceStatus: [1],
      errorComponentTypes: [1],
      models: ["Rig"],
      pairingStatuses: [PairingStatus.DEFAULT_PASSWORD],
      groupIds: [101n],
      rackIds: [202n],
      firmwareVersions: ["v3.5.1"],
      zones: ["Austin, Building 1"],
      ipCidrs: ["192.168.1.0/24"],
      siteIds: [303n],
      includeUnassigned: true,
      buildingIds: [404n],
      includeNoBuilding: true,
      zoneKeys: [{ buildingId: 404n, zone: "A1" }],
      includeNoRack: true,
    });
    expect(result.numericRanges).toEqual(filter.numericRanges);
    expect(result.numericRanges).not.toBe(filter.numericRanges);
    expect(filter.pairingStatuses).toEqual([PairingStatus.AUTHENTICATION_NEEDED, PairingStatus.DEFAULT_PASSWORD]);
  });
});

describe("isFleetSelectablePairingStatus", () => {
  it("returns true only for pairing statuses that can be selected in the miner list", () => {
    expect(isFleetSelectablePairingStatus(PairingStatus.PAIRED)).toBe(true);
    expect(isFleetSelectablePairingStatus(PairingStatus.AUTHENTICATION_NEEDED)).toBe(false);
    expect(isFleetSelectablePairingStatus(PairingStatus.DEFAULT_PASSWORD)).toBe(true);
  });
});
