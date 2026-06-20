import { useCallback } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { buildingsClient } from "@/protoFleet/api/clients";
import {
  type Building,
  type BuildingRack,
  type BuildingWithCounts,
  RackOrderIndex,
} from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import type { FleetListTelemetryRangeFilter } from "@/protoFleet/api/generated/common/v1/fleet_list_stats_pb";
import type { ComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface ListBuildingsBySiteProps {
  siteId: bigint;
  signal?: AbortSignal;
  onSuccess?: (buildings: BuildingWithCounts[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListBuildingsProps {
  siteIds?: bigint[];
  includeUnassigned?: boolean;
  signal?: AbortSignal;
  errorComponentTypes?: ComponentType[];
  telemetryRanges?: FleetListTelemetryRangeFilter[];
  onSuccess?: (buildings: BuildingWithCounts[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListAllBuildingsProps {
  signal?: AbortSignal;
  errorComponentTypes?: ComponentType[];
  telemetryRanges?: FleetListTelemetryRangeFilter[];
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

// RackPlacementInput is one rack's slot in an AssignRacksToBuilding
// batch. aisleIndex + positionInAisle must be paired (both set or both
// unset); the server rejects half-set inputs.
export interface RackPlacementInput {
  rackId: bigint;
  aisleIndex?: number;
  positionInAisle?: number;
}

interface AssignRacksToBuildingProps {
  // Bulk-friendly. Pass a single-element array for the singular case.
  racks: RackPlacementInput[];
  // Unset = unassign every rack in the batch from any building.
  targetBuildingId?: bigint;
  signal?: AbortSignal;
  onSuccess?: (siteReassignedDeviceCount: bigint) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

// AssignDevicesToBuildingConflict carries server-reported per-device
// conflicts surfaced when the batch rejects (e.g. a device is in a
// rack at a different building). Mirrors PerDeviceBuildingConflict.
export interface AssignDevicesToBuildingConflict {
  deviceIdentifier: string;
  reason: number;
  conflictingBuildingId: bigint;
}

interface AssignDevicesToBuildingProps {
  // Unset = move devices to the "Unassigned" bucket (device.building_id
  // becomes NULL).
  targetBuildingId?: bigint;
  deviceIdentifiers: string[];
  // When true, server force-clears any rack memberships that put a
  // device in a different building (mirrors AssignDevicesToSite).
  forceClearConflictingRackMembership?: boolean;
  signal?: AbortSignal;
  onSuccess?: (reassignedCount: bigint, siteReassignedDeviceCount: bigint) => void;
  onError?: (message: string, conflicts: AssignDevicesToBuildingConflict[]) => void;
  onFinally?: () => void;
}

// BuildingFormValues is the FE-side draft shape carried by
// BuildingDetailsModal + ManageBuildingModal. Power values live in MW
// to match the form's surface units; the API maps them to kW on
// submit (proto stores power_kw). Layout fields (aisles, racks per
// aisle) are owned by ManageBuildingModal and ride along on
// UpdateBuilding writes.
//
// The trailing four fields (physicalRackCount + default rack rows /
// columns / order_index) are not currently editable in any FE form —
// they're carried through so an UpdateBuilding call doesn't clobber
// values another caller (API or another client) wrote. The edit
// surface seeds them from the server's Building snapshot via
// buildingFormValuesFromBuilding and sends them back unchanged.
export interface BuildingFormValues {
  name: string;
  description: string;
  powerCapacityMw: number;
  overheadKw: number;
  aisles: number;
  racksPerAisle: number;
  physicalRackCount: number;
  defaultRackRows: number;
  defaultRackColumns: number;
  defaultRackOrderIndex: RackOrderIndex;
}

export const emptyBuildingFormValues = (): BuildingFormValues => ({
  name: "",
  description: "",
  powerCapacityMw: 0,
  overheadKw: 0,
  aisles: 0,
  racksPerAisle: 0,
  physicalRackCount: 0,
  defaultRackRows: 0,
  defaultRackColumns: 0,
  defaultRackOrderIndex: RackOrderIndex.UNSPECIFIED,
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
  physicalRackCount: building.physicalRackCount,
  defaultRackRows: building.defaultRackRows,
  defaultRackColumns: building.defaultRackColumns,
  defaultRackOrderIndex: building.defaultRackOrderIndex,
});

const useBuildings = () => {
  const { handleAuthErrors } = useAuthErrors();

  // Lists buildings under a specific site. Thin wrapper over the generic
  // listBuildings that passes a single-element site_ids array.
  const listBuildingsBySite = useCallback(
    async ({ siteId, signal, onSuccess, onError, onFinally }: ListBuildingsBySiteProps) => {
      try {
        const response = await buildingsClient.listBuildings(
          {
            siteIds: [siteId],
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

  // Generic list: optionally scope to one or more sites and/or the
  // unassigned bucket. Both empty + false = every building in the org.
  // Used by the Buildings tab to push the SitePicker selection
  // server-side instead of client-filtering the full org list.
  const listBuildings = useCallback(
    async ({
      siteIds,
      includeUnassigned,
      signal,
      errorComponentTypes,
      telemetryRanges,
      onSuccess,
      onError,
      onFinally,
    }: ListBuildingsProps = {}) => {
      try {
        const response = await buildingsClient.listBuildings(
          {
            siteIds: siteIds ?? [],
            includeUnassigned: includeUnassigned ?? false,
            errorComponentTypes: errorComponentTypes ?? [],
            telemetryRanges: telemetryRanges ?? [],
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

  // Lists every building visible to the caller in one round-trip. Used by
  // /sites to avoid N+1 ListBuildings calls when rendering per-site overview
  // sections.
  const listAllBuildings = useCallback(
    async ({
      signal,
      errorComponentTypes,
      telemetryRanges,
      onSuccess,
      onError,
      onFinally,
    }: ListAllBuildingsProps = {}) => {
      try {
        const response = await buildingsClient.listBuildings(
          {
            errorComponentTypes: errorComponentTypes ?? [],
            telemetryRanges: telemetryRanges ?? [],
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
        // Server paginates at 50 by default (matches device-list
        // ergonomics) and caps at 1000. Every caller in this file
        // wants the complete working set (ManageBuildingModal seeds
        // from it; BuildingPage uses it to derive cascade rack count),
        // so we loop with the max page size until next_page_token is
        // empty — matching the useDeviceSets.listRacks pattern.
        const rows: BuildingRack[] = [];
        let pageToken = "";
        do {
          const response = await buildingsClient.listBuildingRacks(
            { buildingId, pageSize: 1000, pageToken },
            { signal },
          );
          if (signal?.aborted) return;
          rows.push(...response.racks);
          pageToken = response.nextPageToken;
        } while (pageToken !== "");
        onSuccess?.(rows);
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
            // Pass-through fields — not editable in any current form
            // but carried so the server-side snapshot is preserved.
            physicalRackCount: values.physicalRackCount,
            defaultRackRows: values.defaultRackRows,
            defaultRackColumns: values.defaultRackColumns,
            defaultRackOrderIndex: values.defaultRackOrderIndex,
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

  // assignRacksToBuilding wraps the bulk rack-positioning RPC.
  // Unset `targetBuildingId` unassigns every rack in the batch;
  // each rack's `aisleIndex` + `positionInAisle` must be paired.
  // The server rejects half-set inputs and out-of-bounds positions.
  const assignRacksToBuilding = useCallback(
    async ({ racks, targetBuildingId, signal, onSuccess, onError, onFinally }: AssignRacksToBuildingProps) => {
      try {
        const response = await buildingsClient.assignRacksToBuilding(
          {
            racks,
            targetBuildingId,
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

  // assignDevicesToBuilding wraps the atomic device→building reassignment
  // RPC introduced alongside the device.building_id column. Mirrors
  // sites.assignDevicesToSite — when the response carries conflicts the
  // call surfaces them through onError so the picker can prompt for
  // force-clear (or show the conflict list). targetBuildingId unset =
  // move to "Unassigned".
  const assignDevicesToBuilding = useCallback(
    async ({
      targetBuildingId,
      deviceIdentifiers,
      forceClearConflictingRackMembership,
      signal,
      onSuccess,
      onError,
      onFinally,
    }: AssignDevicesToBuildingProps) => {
      try {
        const response = await buildingsClient.assignDevicesToBuilding(
          {
            targetBuildingId,
            deviceIdentifiers,
            forceClearConflictingRackMembership,
          },
          { signal },
        );
        if (signal?.aborted) return;
        if (response.conflicts.length > 0) {
          const conflicts: AssignDevicesToBuildingConflict[] = response.conflicts.map((c) => ({
            deviceIdentifier: c.deviceIdentifier,
            reason: c.reason,
            conflictingBuildingId: c.conflictingBuildingId,
          }));
          onError?.("Some devices could not be reassigned", conflicts);
          return;
        }
        onSuccess?.(response.reassignedCount, response.siteReassignedDeviceCount);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error) => {
            onError?.(getErrorMessage(error), []);
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
    listBuildings,
    listAllBuildings,
    getBuilding,
    listBuildingRacks,
    createBuilding,
    updateBuilding,
    deleteBuilding,
    assignRacksToBuilding,
    assignDevicesToBuilding,
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
