import type { ActiveFilters, DropdownFilterItem } from "@/shared/components/List/Filters/types";

/**
 * Creates a dropdown filter for miner models.
 * All models are selected by default.
 *
 * Option ids use the model strings supplied by the caller. Callers can
 * optionally provide display labels for their UI.
 */
export function createModelFilter(
  models: string[],
  getLabel: (model: string) => string = (model) => model,
): DropdownFilterItem {
  const options = models.map((model) => ({
    id: model,
    label: getLabel(model),
  }));

  return {
    type: "dropdown",
    title: "Model",
    value: "model",
    options: [...options],
    defaultOptionIds: [...options.map((o) => o.id)],
  };
}

/**
 * Filters items by model based on active dropdown filters.
 * Returns true if the item should be displayed.
 */
export function filterByModel<T extends { model: string }>(item: T, filters: ActiveFilters): boolean {
  const modelFilters = filters.dropdownFilters?.["model"];

  // If no model filter is applied (empty array or undefined), show all items
  if (!modelFilters || modelFilters.length === 0) {
    return true;
  }

  // If model filters are applied, only show items that match
  return modelFilters.includes(item.model);
}
