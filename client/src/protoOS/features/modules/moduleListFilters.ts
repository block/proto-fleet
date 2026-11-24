import { type ModuleColumn, type ModuleListItem, sortableModuleColumns } from "./moduleListModel";
import { moduleStatuses, statusLabels } from "@/protoOS/features/tanks/tankView";
import type { ActiveFilters, DropdownFilterItem, FilterItem } from "@/shared/components/List/Filters/types";
import type { SortDirection } from "@/shared/components/List/types";

export const moduleStatusLabels = { ...statusLabels, healthy: "Hashing" };
const keys = ["status", "issues", "tank"] as const;
const issueOptions = [
  { id: "present", label: "With issues" },
  { id: "none", label: "No issues" },
  { id: "unknown", label: "Unknown" },
];

export const moduleFilters = (rows: ModuleListItem[]): FilterItem[] => {
  const dropdowns: DropdownFilterItem[] = [
    {
      type: "dropdown",
      value: "status",
      title: "Status",
      pluralTitle: "statuses",
      defaultOptionIds: [],
      options: moduleStatuses.map((id) => ({ id, label: moduleStatusLabels[id] })),
    },
    { type: "dropdown", value: "issues", title: "Issues", defaultOptionIds: [], options: issueOptions },
    {
      type: "dropdown",
      value: "tank",
      title: "Tanks",
      pluralTitle: "tanks",
      defaultOptionIds: [],
      options: [...new Map(rows.flatMap((row) => (row.tank ? [[row.tank.id, row.tank] as const] : []))).values()]
        .sort((a, b) => (a.slot ?? Infinity) - (b.slot ?? Infinity))
        .map((tank) => ({ id: tank.id, label: tank.label || `Tank ${tank.slot ?? "?"}` })),
    },
  ];
  return [{ type: "nestedFilterDropdown", value: "all", title: "All filters", children: dropdowns }, ...dropdowns];
};

export const matchesModuleFilters = (row: ModuleListItem, filters: ActiveFilters) =>
  keys.every((key) => {
    const values = filters.dropdownFilters[key] ?? [];
    return !values.length || values.includes(key === "tank" ? (row.tank?.id ?? "") : row[key]);
  });

export const parseModuleListState = (params: URLSearchParams) => {
  const dropdownFilters: Record<string, string[]> = {};
  for (const key of keys) {
    dropdownFilters[key] = [...new Set(params.getAll(key))].filter((value) => {
      if (key === "status") return moduleStatuses.some((status) => status === value);
      if (key === "issues") return issueOptions.some((option) => option.id === value);
      return value.length > 0;
    });
  }
  const field = [...sortableModuleColumns].find((field) => field === params.get("sort")) ?? "name";
  const direction = params.get("direction") === "desc" ? "desc" : "asc";
  return {
    filters: { buttonFilters: [], dropdownFilters, numericFilters: {}, textareaListFilters: {} } as ActiveFilters,
    sort: { field, direction } as { field: ModuleColumn; direction: SortDirection },
  };
};

export const writeModuleFilters = (params: URLSearchParams, filters: ActiveFilters) => {
  const next = new URLSearchParams(params);
  for (const key of keys) {
    if (JSON.stringify(next.getAll(key)) === JSON.stringify(filters.dropdownFilters[key] ?? [])) continue;
    next.delete(key);
    for (const value of filters.dropdownFilters[key] ?? []) next.append(key, value);
  }
  return next;
};
