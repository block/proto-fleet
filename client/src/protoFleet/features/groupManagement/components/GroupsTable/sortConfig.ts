import type { GroupColumn } from "./constants";
import { groupCols } from "./constants";
import { SORT_ASC, SORT_DESC, type SortDirection } from "@/shared/components/List/types";

type GroupSortConfig = {
  defaultDirection: SortDirection;
};

// Only columns with server-side sort support are sortable.
// Stats-based columns (issues, hashrate, efficiency, power, temperature) cannot be
// sorted globally across pages because they are fetched separately from the collection list.
const SORT_CONFIG: Partial<Record<GroupColumn, GroupSortConfig>> = {
  [groupCols.name]: {
    defaultDirection: SORT_ASC,
  },
  [groupCols.miners]: {
    defaultDirection: SORT_DESC,
  },
};

export const SORTABLE_COLUMNS = new Set(Object.keys(SORT_CONFIG) as GroupColumn[]);

export function getDefaultSortDirection(column: GroupColumn): SortDirection {
  return SORT_CONFIG[column]?.defaultDirection ?? SORT_ASC;
}
