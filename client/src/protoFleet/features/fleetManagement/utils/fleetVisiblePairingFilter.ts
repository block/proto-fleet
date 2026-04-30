import { create } from "@bufbuild/protobuf";
import {
  type MinerListFilter,
  MinerListFilterSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

export const FLEET_VISIBLE_PAIRING_STATUSES: PairingStatus[] = [
  PairingStatus.PAIRED,
  PairingStatus.AUTHENTICATION_NEEDED,
];

export const FLEET_SELECTABLE_PAIRING_STATUSES: PairingStatus[] = [PairingStatus.PAIRED];

const fleetVisiblePairingStatusSet = new Set<number>(FLEET_VISIBLE_PAIRING_STATUSES);
const fleetSelectablePairingStatusSet = new Set<number>(FLEET_SELECTABLE_PAIRING_STATUSES);

const applyAllowedPairingStatuses = (
  filter: MinerListFilter | undefined,
  allowedPairingStatuses: PairingStatus[],
  allowedPairingStatusSet: Set<number>,
): MinerListFilter => {
  const requestedPairingStatuses = filter?.pairingStatuses ?? [];
  const pairingStatuses = requestedPairingStatuses.filter((status) => allowedPairingStatusSet.has(status));
  const hasExplicitPairingStatuses = requestedPairingStatuses.length > 0;

  return create(MinerListFilterSchema, {
    deviceStatus: filter?.deviceStatus ?? [],
    errorComponentTypes: filter?.errorComponentTypes ?? [],
    models: filter?.models ?? [],
    pairingStatuses:
      pairingStatuses.length > 0 || hasExplicitPairingStatuses ? pairingStatuses : [...allowedPairingStatuses],
    groupIds: filter?.groupIds ?? [],
    rackIds: filter?.rackIds ?? [],
    firmwareVersions: filter?.firmwareVersions ?? [],
    zones: filter?.zones ?? [],
  });
};

export const isFleetSelectablePairingStatus = (pairingStatus: PairingStatus): boolean =>
  fleetSelectablePairingStatusSet.has(pairingStatus);

export const applyFleetVisiblePairingStatuses = (filter?: MinerListFilter): MinerListFilter =>
  applyAllowedPairingStatuses(filter, FLEET_VISIBLE_PAIRING_STATUSES, fleetVisiblePairingStatusSet);

export const applyFleetSelectablePairingStatuses = (filter?: MinerListFilter): MinerListFilter =>
  applyAllowedPairingStatuses(filter, FLEET_SELECTABLE_PAIRING_STATUSES, fleetSelectablePairingStatusSet);
