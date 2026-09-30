import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { type SortConfig, SortDirection, SortField } from "./generated/common/v1/sort_pb";
import {
  InventorySort,
  InventoryView,
  ListInventoryRequestSchema,
} from "./generated/fleetinventory/v1/fleetinventory_pb";
import { type MinerListFilter, MinerListFilterSchema } from "./generated/fleetmanagement/v1/fleetmanagement_pb";
import { FLEET_VISIBLE_PAIRING_STATUSES } from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";

export function inventorySort(sort?: SortConfig): InventorySort {
  const fields: Partial<Record<SortField, InventorySort>> = {
    [SortField.NAME]: InventorySort.NAME,
    [SortField.HASHRATE]: InventorySort.HASHRATE,
    [SortField.POWER]: InventorySort.POWER,
    [SortField.TEMPERATURE]: InventorySort.TEMPERATURE,
    [SortField.EFFICIENCY]: InventorySort.EFFICIENCY,
    [SortField.DEVICE_COUNT]: InventorySort.ASSET_COUNT,
    [SortField.ISSUE_COUNT]: InventorySort.ISSUE_COUNT,
    [SortField.MODEL]: InventorySort.MODEL,
    [SortField.FIRMWARE]: InventorySort.FIRMWARE,
    [SortField.IP_ADDRESS]: InventorySort.IP_ADDRESS,
    [SortField.MAC_ADDRESS]: InventorySort.MAC_ADDRESS,
    [SortField.WORKER_NAME]: InventorySort.WORKER_NAME,
    [SortField.LOCATION]: InventorySort.ZONE,
  };
  return fields[sort?.field ?? SortField.NAME] ?? InventorySort.NAME;
}

export function miningInventoryRequest(
  filter?: MinerListFilter,
  sort?: SortConfig,
): MessageInitShape<typeof ListInventoryRequestSchema> {
  const f = filter ?? create(MinerListFilterSchema);
  // Native miner statuses include authentication/error semantics that do not
  // correspond one-to-one to module states. Keep them in the native filter.
  const specific =
    f.models.length ||
    f.firmwareVersions.length ||
    f.groupIds.length ||
    f.rackIds.length ||
    f.buildingIds.length ||
    f.zoneKeys.length ||
    f.zones.length ||
    f.ipCidrs.length ||
    f.ipRanges.length ||
    f.errorComponentTypes.length ||
    f.includeNoRack ||
    f.includeNoBuilding ||
    f.pairingStatuses.length ||
    f.deviceStatus.length ||
    f.numericRanges.some((range) => range.field > 4);
  const base = {
    view: InventoryView.MINING_ASSETS,
    sort: inventorySort(sort),
    descending: sort?.direction === SortDirection.DESC,
  };
  if (specific)
    return {
      ...base,
      minerFilter: {
        ...f,
        pairingStatuses: f.pairingStatuses.length ? f.pairingStatuses : FLEET_VISIBLE_PAIRING_STATUSES,
      },
    };
  return {
    ...base,
    siteIds: f.siteIds,
    includeUnassigned: f.includeUnassigned,
    searchQuery: f.searchQuery,
    telemetryRanges: f.numericRanges.map((range) => ({
      field: Number(range.field),
      min: range.min,
      max: range.max,
      minInclusive: range.minInclusive,
      maxInclusive: range.maxInclusive,
    })),
  };
}
