import { useCallback } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { buildingsClient } from "@/protoFleet/api/clients";
import { type Building, type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface ListBuildingsBySiteProps {
  siteId: bigint;
  signal?: AbortSignal;
  onSuccess?: (buildings: BuildingWithCounts[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListAllBuildingsProps {
  signal?: AbortSignal;
  onSuccess?: (buildings: BuildingWithCounts[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface GetBuildingProps {
  id: bigint;
  signal?: AbortSignal;
  onSuccess?: (building: Building | undefined) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

const useBuildings = () => {
  const { handleAuthErrors } = useAuthErrors();

  const listBuildingsBySite = useCallback(
    async ({ siteId, signal, onSuccess, onError, onFinally }: ListBuildingsBySiteProps) => {
      try {
        const response = await buildingsClient.listBuildings(
          {
            siteFilter: { case: "siteId", value: siteId },
          },
          { signal },
        );
        if (signal?.aborted) return;
        onSuccess?.(response.buildings);
      } catch (err) {
        if (signal?.aborted) return;
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

  // Lists every building visible to the caller in one round-trip. Connect-RPC
  // accepts an absent siteFilter oneof (case: undefined) and the server treats
  // that as "all buildings". Used by /sites to avoid N+1 ListBuildings calls
  // when rendering per-site overview sections.
  const listAllBuildings = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: ListAllBuildingsProps = {}) => {
      try {
        const response = await buildingsClient.listBuildings({}, { signal });
        if (signal?.aborted) return;
        onSuccess?.(response.buildings);
      } catch (err) {
        if (signal?.aborted) return;
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

  // Fetches a single building by id. NotFound responses map to onSuccess with
  // `undefined` so callers can render their not-found state without inspecting
  // error codes; every other failure (PermissionDenied, transport / network,
  // server 5xx) flows through onError so the consumer can surface a real
  // error UI instead of misclassifying it as "missing building".
  const getBuilding = useCallback(
    async ({ id, signal, onSuccess, onError, onFinally }: GetBuildingProps) => {
      try {
        const response = await buildingsClient.getBuilding({ id }, { signal });
        if (signal?.aborted) return;
        onSuccess?.(response.building);
      } catch (err) {
        if (signal?.aborted) return;
        if (err instanceof ConnectError && err.code === Code.NotFound) {
          onSuccess?.(undefined);
          return;
        }
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

  return { listBuildingsBySite, listAllBuildings, getBuilding };
};

export { useBuildings };
