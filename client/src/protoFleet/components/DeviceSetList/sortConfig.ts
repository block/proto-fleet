import type { DeviceSetColumn } from "./constants";
import { deviceSetCols } from "./constants";
import { SORT_ASC, SORT_DESC, type SortDirection } from "@/shared/components/List/types";

type DeviceSetSortConfig = {
  defaultDirection: SortDirection;
};

type DeviceSetSortState = {
  field: DeviceSetColumn;
  direction: SortDirection;
};

type DeviceSetSortOption = {
  id: DeviceSetColumn;
  label: string;
};

// Only fields backed by the list query are sortable. Telemetry-based columns still
// cannot be sorted globally across pages because they are fetched separately.
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
  [deviceSetCols.issues]: {
    defaultDirection: SORT_DESC,
  },
};

export const RACK_SORT_OPTIONS: DeviceSetSortOption[] = [
  { id: deviceSetCols.name, label: "Name" },
  { id: deviceSetCols.zone, label: "Zone" },
  { id: deviceSetCols.miners, label: "Miners" },
  { id: deviceSetCols.issues, label: "Issues" },
];

export const SORTABLE_COLUMNS = new Set(Object.keys(SORT_CONFIG) as DeviceSetColumn[]);

function toggleSortDirection(direction: SortDirection): SortDirection {
  return direction === SORT_ASC ? SORT_DESC : SORT_ASC;
}

function isSortableColumn(value: string): value is DeviceSetColumn {
  return SORTABLE_COLUMNS.has(value as DeviceSetColumn);
}

function getDropdownSortDirection(column: DeviceSetColumn): SortDirection {
  return column === deviceSetCols.issues ? SORT_DESC : SORT_ASC;
}

function getSelectedSortField(selected: string[], currentField: DeviceSetColumn): DeviceSetColumn {
  return (
    selected.find((value): value is DeviceSetColumn => isSortableColumn(value) && value !== currentField) ??
    selected.find(isSortableColumn) ??
    currentField
  );
}

export function getDefaultSortDirection(column: DeviceSetColumn): SortDirection {
  return SORT_CONFIG[column]?.defaultDirection ?? SORT_ASC;
}

export function getNextSortFromSelection(selected: string[], currentSort: DeviceSetSortState): DeviceSetSortState {
  if (selected.length === 0) {
    return {
      field: currentSort.field,
      direction: toggleSortDirection(currentSort.direction),
    };
  }

  const field = getSelectedSortField(selected, currentSort.field);
  const direction =
    field === currentSort.field ? toggleSortDirection(currentSort.direction) : getDropdownSortDirection(field);

  return {
    field,
    direction,
  };
}
