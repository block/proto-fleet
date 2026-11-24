import { DeviceClass } from "@/protoFleet/api/generated/common/v1/common_pb";
import { createModelFilter, filterByModel } from "@/protoFleet/utils/minerFilters";
import type { ActiveFilters, DropdownFilterItem } from "@/shared/components/List/Filters/types";

type DiscoveredModel = { model: string; deviceClass: DeviceClass };
const CONTAINER_FILTER_ID = "container";

function modelFilterId(device: DiscoveredModel): string {
  return device.deviceClass === DeviceClass.CONTAINER ? CONTAINER_FILTER_ID : `model:${device.model}`;
}

/** Containers share an option; rig model IDs remain distinct even if their labels match. */
export function createFoundMinersModelFilter(devices: DiscoveredModel[]): DropdownFilterItem {
  const modelIds = Array.from(new Set(devices.map(modelFilterId)));
  return createModelFilter(modelIds, (id) =>
    id === CONTAINER_FILTER_ID ? "Proto Container" : id.slice("model:".length),
  );
}

export function filterFoundMinerByModel(item: DiscoveredModel, filters: ActiveFilters): boolean {
  return filterByModel({ model: modelFilterId(item) }, filters);
}
