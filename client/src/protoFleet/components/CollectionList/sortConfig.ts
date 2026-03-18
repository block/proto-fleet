import type { CollectionColumn } from "./constants";
import { collectionCols } from "./constants";
import { SORT_ASC, SORT_DESC, type SortDirection } from "@/shared/components/List/types";

type CollectionSortConfig = {
  defaultDirection: SortDirection;
};

// Only name and miners are sortable. Stats-based columns (issues, hashrate, efficiency,
// power, temperature) cannot be sorted globally across pages because they are fetched
// separately from the collection list.
const SORT_CONFIG: Partial<Record<CollectionColumn, CollectionSortConfig>> = {
  [collectionCols.name]: {
    defaultDirection: SORT_ASC,
  },
  [collectionCols.miners]: {
    defaultDirection: SORT_DESC,
  },
};

export const SORTABLE_COLUMNS = new Set(Object.keys(SORT_CONFIG) as CollectionColumn[]);

export function getDefaultSortDirection(column: CollectionColumn): SortDirection {
  return SORT_CONFIG[column]?.defaultDirection ?? SORT_ASC;
}
