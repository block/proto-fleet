import { useCallback } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { buildingsClient } from "@/protoFleet/api/clients";
import {
  type Building,
  type BuildingRack,
  type BuildingWithCounts,
  RackOrderIndex,
} from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
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

interface ListBuildingRacksProps {
  buildingId: bigint;
  signal?: AbortSignal;
  onSuccess?: (racks: BuildingRack[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface CreateBuildingProps {
  values: BuildingFormValues;
  siteId: bigint;
  signal?: AbortSignal;
  onSuccess?: (building: Building) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface UpdateBuildingProps {
  id: bigint;
  values: BuildingFormValues;
  signal?: AbortSignal;
  onSuccess?: (building: Building) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface DeleteBuildingProps {
  id: bigint;
  signal?: AbortSignal;
  onSuccess?: (unassignedRackCount: bigint) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface AssignRackToBuildingProps {
  rackId: bigint;
  // Unset = unassign from any building.
  buildingId?: bigint;
  // Optional grid cell. Must be paired.
  aisleIndex?: number;
  positionInAisle?: number;
  signal?: AbortSignal;
  onSuccess?: (siteReassignedDeviceCount: bigint) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

// BuildingFormValues is the FE-side draft shape carried by
// BuildingDetailsModal + ManageBuildingModal. Power values live in MW
// to match the form's surface units; the API maps them to kW on
// submit (proto stores power_kw). Layout fields (aisles, racks per
// aisle) are owned by ManageBuildingModal and ride along on
// UpdateBuilding writes.
export interface BuildingFormValues {
  name: string;
  description: string;
  powerCapacityMw: number;
  overheadKw: number;
  aisles: number;
  racksPerAisle: number;
}

export const emptyBuildingFormValues = (): BuildingFormValues => ({
  name: "",
  description: "",
  powerCapacityMw: 0,
  overheadKw: 0,
  aisles: 0,
  racksPerAisle: 0,
});

export const buildingFormValuesFromBuilding = (building: Building): BuildingFormValues => ({
  name: building.name,
  description: building.description,
  // Proto stores kW; UI carries MW so the form units match the site
  // form. Conversion is the inverse of the kW→MW on display.
  powerCapacityMw: building.powerKw > 0 ? building.powerKw / 1000 : 0,
  overheadKw: building.overheadKw,
  aisles: building.aisles,
  racksPerAisle: building.racksPerAisle,
});

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

  const listBuildingRacks = useCallback(
    async ({ buildingId, signal, onSuccess, onError, onFinally }: ListBuildingRacksProps) => {
      try {
        const response = await buildingsClient.listBuildingRacks({ buildingId }, { signal });
        if (signal?.aborted) return;
        onSuccess?.(response.racks);
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

  const createBuilding = useCallback(
    async ({ values, siteId, signal, onSuccess, onError, onFinally }: CreateBuildingProps) => {
      try {
        const response = await buildingsClient.createBuilding(
          {
            siteId,
            name: values.name,
            description: values.description,
            powerKw: mwToKw(values.powerCapacityMw),
            overheadKw: values.overheadKw,
            aisles: values.aisles,
            racksPerAisle: values.racksPerAisle,
            // Layout defaults are not surfaced in the Phase 1a
            // building modals. Send the proto's documented "unset"
            // sentinels so the server stores NULL / UNSPECIFIED.
            physicalRackCount: 0,
            defaultRackRows: 0,
            defaultRackColumns: 0,
            defaultRackOrderIndex: RackOrderIndex.UNSPECIFIED,
          },
          { signal },
        );
        if (signal?.aborted) return;
        if (response.building) onSuccess?.(response.building);
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

  const updateBuilding = useCallback(
    async ({ id, values, signal, onSuccess, onError, onFinally }: UpdateBuildingProps) => {
      try {
        const response = await buildingsClient.updateBuilding(
          {
            id,
            name: values.name,
            description: values.description,
            powerKw: mwToKw(values.powerCapacityMw),
            overheadKw: values.overheadKw,
            aisles: values.aisles,
            racksPerAisle: values.racksPerAisle,
            physicalRackCount: 0,
            defaultRackRows: 0,
            defaultRackColumns: 0,
            defaultRackOrderIndex: RackOrderIndex.UNSPECIFIED,
          },
          { signal },
        );
        if (signal?.aborted) return;
        if (response.building) onSuccess?.(response.building);
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

  const deleteBuilding = useCallback(
    async ({ id, signal, onSuccess, onError, onFinally }: DeleteBuildingProps) => {
      try {
        const response = await buildingsClient.deleteBuilding({ id }, { signal });
        if (signal?.aborted) return;
        onSuccess?.(response.unassignedRackCount);
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

  // assignRackToBuilding wraps the dedicated rack-positioning RPC.
  // Unset `buildingId` unassigns the rack; passing both `aisleIndex`
  // and `positionInAisle` positions the rack at that grid cell.
  // Passing one without the other is rejected by the server; the
  // wrapper preserves the failure surface so callers can react.
  const assignRackToBuilding = useCallback(
    async ({
      rackId,
      buildingId,
      aisleIndex,
      positionInAisle,
      signal,
      onSuccess,
      onError,
      onFinally,
    }: AssignRackToBuildingProps) => {
      try {
        const response = await buildingsClient.assignRackToBuilding(
          {
            rackId,
            buildingId,
            aisleIndex,
            positionInAisle,
          },
          { signal },
        );
        if (signal?.aborted) return;
        onSuccess?.(response.siteReassignedDeviceCount);
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

  return {
    listBuildingsBySite,
    listAllBuildings,
    getBuilding,
    listBuildingRacks,
    createBuilding,
    updateBuilding,
    deleteBuilding,
    assignRackToBuilding,
  };
};

// MW→kW with three-decimal rounding so floating-point drift in the
// MW form input doesn't produce trailing-7 kW values (e.g. 1.234
// MW → 1233.9999999999998 kW becomes 1234 kW on disk). Negative or
// non-finite values surface as 0 because the BE rejects them anyway.
const mwToKw = (mw: number): number => {
  if (!Number.isFinite(mw) || mw <= 0) return 0;
  return Math.round(mw * 1000 * 1000) / 1000;
};

export { useBuildings };
