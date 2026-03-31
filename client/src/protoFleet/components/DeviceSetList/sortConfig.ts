import type { DeviceSetColumn } from "./constants";
import { deviceSetCols } from "./constants";
import { SORT_ASC, SORT_DESC, type SortDirection } from "@/shared/components/List/types";

type DeviceSetSortConfig = {
  defaultDirection: SortDirection;
};

// Only name and miners are sortable. Stats-based columns (issues, hashrate, efficiency,
// power, temperature) cannot be sorted globally across pages because they are fetched
// separately from the device set list.
const SORT_CONFIG: Partial<Record<DeviceSetColumn, DeviceSetSortConfig>> = {
  [deviceSetCols.name]: {
    defaultDirection: SORT_ASC,
  },
  [deviceSetCols.zone]: {
    defaultDirection: SORT_ASC,
  },
  [deviceSetCols.miners]: {
    defaultDirection: SORT_DESC,
  },
};

export const SORTABLE_COLUMNS = new Set(Object.keys(SORT_CONFIG) as DeviceSetColumn[]);

export function getDefaultSortDirection(column: DeviceSetColumn): SortDirection {
  return SORT_CONFIG[column]?.defaultDirection ?? SORT_ASC;
}
