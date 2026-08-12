import { isContainerModel } from "./foundMinersLabels";
import { createModelFilter, filterByModel } from "@/protoFleet/utils/minerFilters";
import type { ActiveFilters, DropdownFilterItem } from "@/shared/components/List/Filters/types";

const CONTAINER_MODEL_FILTER_ID = "CU";

function modelFilterId(model: string): string {
  return isContainerModel(model) ? CONTAINER_MODEL_FILTER_ID : model;
}

/** Creates one logical model option for every discovered container variant. */
export function createFoundMinersModelFilter(models: string[]): DropdownFilterItem {
  const modelIds = Array.from(new Set(models.map(modelFilterId)));

  return createModelFilter(modelIds, (model) => (model === CONTAINER_MODEL_FILTER_ID ? "Proto Container" : model));
}

/** Matches every raw container model when the logical container option is selected. */
export function filterFoundMinerByModel<T extends { model: string }>(item: T, filters: ActiveFilters): boolean {
  return filterByModel({ model: modelFilterId(item.model) }, filters);
}
