import { useMemo } from "react";
import { useContainers } from "./useContainers";
import type { FilterLabelSource } from "@/protoFleet/features/fleetManagement/views/viewSummary";

const EMPTY_OPTIONS: FilterLabelSource[] = [];

// Keep the readable catalog independent of list filters so changing site scope
// does not remove selected containers from the selector or broaden the query.
export function useContainerFilterOptions(enabled: boolean) {
  const { containers, loading } = useContainers([], false, enabled);
  return useMemo(() => {
    if (!enabled || loading) return EMPTY_OPTIONS;
    return containers.map((container) => ({
      id: container.id.toString(),
      label: `${container.name || container.deviceIdentifier} · ${container.site?.label || "Unassigned"}`,
    }));
  }, [containers, enabled, loading]);
}
