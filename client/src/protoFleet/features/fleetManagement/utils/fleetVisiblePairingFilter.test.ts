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
      pairingStatuses: [PairingStatus.AUTHENTICATION_NEEDED],
    });

    expect(applyFleetVisiblePairingStatuses(filter).pairingStatuses).toEqual([PairingStatus.AUTHENTICATION_NEEDED]);
  });

  it("filters out non-visible pairing statuses", () => {
    const filter: MinerListFilter = create(MinerListFilterSchema, {
      pairingStatuses: [PairingStatus.PAIRED, PairingStatus.PENDING],
    });

    expect(applyFleetVisiblePairingStatuses(filter).pairingStatuses).toEqual([PairingStatus.PAIRED]);
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
      pairingStatuses: [PairingStatus.PAIRED, PairingStatus.AUTHENTICATION_NEEDED],
    });

    expect(applyFleetSelectablePairingStatuses(filter).pairingStatuses).toEqual([PairingStatus.PAIRED]);
  });

  it("preserves an empty selectable intersection for explicit non-selectable filters", () => {
    const filter: MinerListFilter = create(MinerListFilterSchema, {
      pairingStatuses: [PairingStatus.AUTHENTICATION_NEEDED],
    });

    expect(applyFleetSelectablePairingStatuses(filter).pairingStatuses).toEqual([]);
  });
});

describe("isFleetSelectablePairingStatus", () => {
  it("returns true only for pairing statuses that can be selected in the miner list", () => {
    expect(isFleetSelectablePairingStatus(PairingStatus.PAIRED)).toBe(true);
    expect(isFleetSelectablePairingStatus(PairingStatus.AUTHENTICATION_NEEDED)).toBe(false);
  });
});
