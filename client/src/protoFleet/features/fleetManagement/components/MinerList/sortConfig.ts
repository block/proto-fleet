import { minerCols, type MinerColumn } from "./constants";

import { SortField } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

/** Maps UI column keys to proto SortField enum values. */
export const COLUMN_TO_SORT_FIELD: Partial<Record<MinerColumn, SortField>> = {
  [minerCols.name]: SortField.NAME,
  [minerCols.type]: SortField.DEVICE_TYPE,
  [minerCols.macAddress]: SortField.MAC_ADDRESS,
  [minerCols.ipAddress]: SortField.IP_ADDRESS,
  [minerCols.status]: SortField.STATUS,
  [minerCols.hashrate]: SortField.HASHRATE,
  [minerCols.efficiency]: SortField.EFFICIENCY,
  [minerCols.powerUsage]: SortField.POWER,
  [minerCols.temperature]: SortField.TEMPERATURE,
  [minerCols.issues]: SortField.ISSUES,
  [minerCols.firmware]: SortField.FIRMWARE,
};

/**
 * Reverse mapping from SortField to column key.
 * Used when parsing sort from URL.
 */
export const SORT_FIELD_TO_COLUMN: Partial<Record<SortField, MinerColumn>> = {
  [SortField.NAME]: minerCols.name,
  [SortField.DEVICE_TYPE]: minerCols.type,
  [SortField.MAC_ADDRESS]: minerCols.macAddress,
  [SortField.IP_ADDRESS]: minerCols.ipAddress,
  [SortField.STATUS]: minerCols.status,
  [SortField.HASHRATE]: minerCols.hashrate,
  [SortField.EFFICIENCY]: minerCols.efficiency,
  [SortField.POWER]: minerCols.powerUsage,
  [SortField.TEMPERATURE]: minerCols.temperature,
  [SortField.ISSUES]: minerCols.issues,
  [SortField.FIRMWARE]: minerCols.firmware,
};

/** Columns that support sorting (derived from mapping) */
export const SORTABLE_COLUMNS = Object.keys(COLUMN_TO_SORT_FIELD) as MinerColumn[];
