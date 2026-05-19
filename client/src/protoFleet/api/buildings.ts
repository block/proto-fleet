import { useCallback } from "react";

import { buildingsClient } from "@/protoFleet/api/clients";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface ListBuildingsBySiteProps {
  siteId: bigint;
  onSuccess?: (buildings: BuildingWithCounts[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

// BuildingService today exposes only List/Create/Update/Delete; there is no
// GetBuilding(id) RPC. /buildings/:id therefore takes a `?site=<id>` query
// param so it can call ListBuildings against the parent site and pick out
// the matching row. A follow-up should add GetBuilding so the page works on
// a bare URL without the param.
const useBuildings = () => {
  const { handleAuthErrors } = useAuthErrors();

  const listBuildingsBySite = useCallback(
    async ({ siteId, onSuccess, onError, onFinally }: ListBuildingsBySiteProps) => {
      try {
        const response = await buildingsClient.listBuildings({
          siteFilter: { case: "siteId", value: siteId },
        });
        onSuccess?.(response.buildings);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: (error) => {
            onError?.(getErrorMessage(error));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  return { listBuildingsBySite };
};

export { useBuildings };
