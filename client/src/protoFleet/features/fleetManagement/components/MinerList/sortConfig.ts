import { minerCols, type MinerColumn } from "./constants";

import { SortField } from "@/protoFleet/api/generated/common/v1/sort_pb";
import { SORT_ASC, SORT_DESC, type SortDirection } from "@/shared/components/List/types";

type SortColumnConfig = {
  field: SortField;
  defaultDirection: SortDirection;
};

/** Single source of truth for sortable column configuration. */
const SORT_CONFIG: Partial<Record<MinerColumn, SortColumnConfig>> = {
  [minerCols.name]: { field: SortField.NAME, defaultDirection: SORT_ASC },
  [minerCols.workerName]: { field: SortField.WORKER_NAME, defaultDirection: SORT_ASC },
  [minerCols.model]: { field: SortField.MODEL, defaultDirection: SORT_ASC },
  [minerCols.macAddress]: { field: SortField.MAC_ADDRESS, defaultDirection: SORT_ASC },
  [minerCols.ipAddress]: { field: SortField.IP_ADDRESS, defaultDirection: SORT_ASC },
  [minerCols.hashrate]: { field: SortField.HASHRATE, defaultDirection: SORT_DESC },
  [minerCols.efficiency]: { field: SortField.EFFICIENCY, defaultDirection: SORT_DESC },
  [minerCols.powerUsage]: { field: SortField.POWER, defaultDirection: SORT_DESC },
  [minerCols.temperature]: { field: SortField.TEMPERATURE, defaultDirection: SORT_DESC },
  [minerCols.firmware]: { field: SortField.FIRMWARE, defaultDirection: SORT_ASC },
};

/** Columns that support sorting. */
export const SORTABLE_COLUMNS = Object.keys(SORT_CONFIG) as MinerColumn[];

/** Gets the SortField for a column, or undefined if not sortable. */
export function getSortField(column: MinerColumn): SortField | undefined {
  return SORT_CONFIG[column]?.field;
}

/** Gets the column for a SortField, or undefined if not found. Used when parsing sort from URL. */
export function getColumnForSortField(field: SortField): MinerColumn | undefined {
  const entry = Object.entries(SORT_CONFIG).find(([, config]) => config.field === field);
  return entry?.[0] as MinerColumn | undefined;
}

/** Gets the default sort direction for a column. */
export function getDefaultSortDirection(column: MinerColumn): SortDirection {
  return SORT_CONFIG[column]?.defaultDirection ?? SORT_ASC;
}
