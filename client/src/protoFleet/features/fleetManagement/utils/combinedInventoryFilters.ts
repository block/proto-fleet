import type { FilterLabelSource } from "../views/viewSummary";
import type { ActiveFilters } from "@/shared/components/List/Filters/types";

const INVENTORY_FILTER_PAIRS = [
  ["building", "container"],
  ["rack", "tank"],
] as const;

export const combinedInventoryOptions = (
  options: FilterLabelSource[],
  kind: "container" | "tank",
  ct1: FilterLabelSource[] = [],
): FilterLabelSource[] => [
  ...options,
  ...ct1.map(({ id, label }) => ({
    id: kind + ":" + id,
    label: label + (kind === "container" ? " (Container)" : " (Tank)"),
  })),
];

export const splitInventoryValues = (values: string[], kind: "container" | "tank") => ({
  conventional: values.filter((value) => !value.startsWith(kind + ":")),
  ct1: values.filter((value) => value.startsWith(kind + ":")).map((value) => value.slice(kind.length + 1)),
});

export const combineInventoryFilters = (filters: ActiveFilters): ActiveFilters => {
  const dropdownFilters = { ...filters.dropdownFilters };
  for (const [key, kind] of INVENTORY_FILTER_PAIRS) {
    const values = [...(dropdownFilters[key] ?? []), ...(dropdownFilters[kind] ?? []).map((id) => kind + ":" + id)];
    if (values.length) dropdownFilters[key] = values;
    delete dropdownFilters[kind];
  }
  return { ...filters, dropdownFilters };
};

export const separateInventoryFilters = (filters: ActiveFilters): ActiveFilters => {
  const dropdownFilters = { ...filters.dropdownFilters };
  for (const [key, kind] of INVENTORY_FILTER_PAIRS) {
    const { conventional, ct1 } = splitInventoryValues(dropdownFilters[key] ?? [], kind);
    dropdownFilters[key] = conventional;
    dropdownFilters[kind] = ct1;
  }
  return { ...filters, dropdownFilters };
};
