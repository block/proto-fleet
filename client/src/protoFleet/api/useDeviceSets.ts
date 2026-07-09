import { useCallback } from "react";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { deviceSetClient } from "@/protoFleet/api/clients";
import {
  DeviceIdentifierListSchema,
  DeviceSelectorSchema,
} from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import type { FleetListTelemetryRangeFilter } from "@/protoFleet/api/generated/common/v1/fleet_list_stats_pb";
import { type SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  type DeviceSet,
  type DeviceSetStats,
  DeviceSetType,
  type PerDeviceRackConflict,
  type RackCoolingType,
  RackInfoSchema,
  type RackOrderIndex,
  type RackSlot,
  type RackSlotPosition,
  RackSlotPositionSchema,
  RackSlotSchema,
  type RackType,
} from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isAbortError } from "@/protoFleet/api/requestErrors";
import { useAuthErrors } from "@/protoFleet/store";

interface CreateGroupProps {
  label: string;
  deviceIdentifiers?: string[];
  allDevices?: boolean;
  onSuccess?: (deviceSet: DeviceSet) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface UpdateGroupProps {
  deviceSetId: bigint;
  label?: string;
  deviceIdentifiers?: string[];
  allDevices?: boolean;
  onSuccess?: (deviceSet: DeviceSet) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface DeleteGroupProps {
  deviceSetId: bigint;
  onSuccess?: () => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

export interface ListDeviceSetsProps {
  pageSize?: number;
  pageToken?: string;
  sort?: SortConfig;
  errorComponentTypes?: number[];
  zones?: string[];
  buildingIds?: bigint[];
  includeNoBuilding?: boolean;
  // Rack-list site filter (RACK type only). Mirrors the miner-list
  // shape: siteIds is an OR across sites, includeUnassigned additionally
  // surfaces racks with device_set_rack.site_id IS NULL. Both empty +
  // false = no site filter applied.
  siteIds?: bigint[];
  includeUnassigned?: boolean;
  telemetryRanges?: FleetListTelemetryRangeFilter[];
  onSuccess?: (deviceSets: DeviceSet[], nextPageToken: string, totalCount: number) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface AddDevicesToGroupProps {
  targetGroupId: bigint;
  deviceIdentifiers?: string[];
  allDevices?: boolean;
  signal?: AbortSignal;
  onSuccess?: (addedCount: bigint) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface GetDeviceSetProps {
  deviceSetId: bigint;
  onSuccess?: (deviceSet: DeviceSet) => void;
  onNotFound?: () => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface GetDeviceSetStatsProps {
  deviceSetIds: bigint[];
  onSuccess?: (stats: DeviceSetStats[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface CreateRackProps {
  label: string;
  zone: string;
  rows: number;
  columns: number;
  orderIndex: RackOrderIndex;
  coolingType: RackCoolingType;
  onSuccess?: (deviceSet: DeviceSet) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListRackZonesProps {
  onSuccess?: (zones: string[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListRackTypesProps {
  onSuccess?: (rackTypes: RackType[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListGroupMembersProps {
  deviceSetId: bigint;
  siteIds?: bigint[];
  includeUnassigned?: boolean;
  signal?: AbortSignal;
  onSuccess?: (deviceIdentifiers: string[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface RemoveDevicesFromGroupProps {
  targetGroupId: bigint;
  deviceIdentifiers?: string[];
  allDevices?: boolean;
  signal?: AbortSignal;
  onSuccess?: (removedCount: bigint) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface UpdateRackProps {
  deviceSetId: bigint;
  label?: string;
  zone?: string;
  rows?: number;
  columns?: number;
  orderIndex?: RackOrderIndex;
  coolingType?: RackCoolingType;
  // Placement (site:manage). Encoded like saveRack: a building fully
  // determines placement (site derived server-side); an explicit 0n unassigns
  // that level; undefined omits placement entirely (server preserves the
  // rack's current site/building). Omit both for a rack:manage-only settings
  // save that must not move the rack.
  siteId?: bigint;
  buildingId?: bigint;
  onSuccess?: (deviceSet: DeviceSet) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface GetRackSlotsProps {
  deviceSetId: bigint;
  onSuccess?: (slots: RackSlot[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface SetRackSlotPositionProps {
  deviceSetId: bigint;
  deviceIdentifier: string;
  position: RackSlotPosition;
  onSuccess?: (slot: RackSlot) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ClearRackSlotPositionProps {
  deviceSetId: bigint;
  deviceIdentifier: string;
  onSuccess?: () => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface AssignDevicesToRackProps {
  // Unset clears rack membership without re-assigning (site/building
  // stay intact).
  targetRackId?: bigint;
  deviceIdentifiers: string[];
  // When adding to a site-less rack, proceed and strip the conflicting
  // miners' site. Default false: the server returns conflicts (surfaced
  // via onConflicts) and writes nothing.
  forceClearConflictingSite?: boolean;
  signal?: AbortSignal;
  onSuccess?: (assignedCount: bigint, siteReassignedCount: bigint, removedCount: bigint) => void;
  // Fires when the server returns site-strip conflicts (no write
  // happened). The caller confirms and retries with
  // forceClearConflictingSite=true.
  onConflicts?: (conflicts: PerDeviceRackConflict[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface SaveRackProps {
  deviceSetId?: bigint;
  label: string;
  zone: string;
  rows: number;
  columns: number;
  orderIndex: RackOrderIndex;
  coolingType: RackCoolingType;
  deviceIdentifiers: string[];
  allDevices?: boolean;
  slotAssignments: { deviceIdentifier: string; row: number; column: number }[];
  // Rack placement. Encoded onto RackInfo per its proto contract: when
  // buildingId is set we send only building_id and let the server derive
  // site_id; otherwise we send explicit site_id + building_id (0 = unassign)
  // so an edit that clears placement takes effect. Leave both undefined to
  // preserve the current placement on an update.
  siteId?: bigint;
  buildingId?: bigint;
  onSuccess?: (deviceSet: DeviceSet, assignedCount: number) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

const memberPageSize = 250;

function buildDeviceSelector(deviceIdentifiers: string[] | undefined, allDevices: boolean | undefined) {
  if (allDevices) {
    return create(DeviceSelectorSchema, {
      selectionType: {
        case: "allDevices",
        value: true,
      },
    });
  }
  // When deviceIdentifiers is provided (even empty), build a device list selector
  if (deviceIdentifiers !== undefined) {
    return create(DeviceSelectorSchema, {
      selectionType: {
        case: "deviceList",
        value: create(DeviceIdentifierListSchema, {
          deviceIdentifiers,
        }),
      },
    });
  }
  return undefined;
}

function getDeviceSetErrorMessage(err: unknown, kind: "group" | "rack"): string {
  if (err instanceof ConnectError && err.code === Code.AlreadyExists) {
    return `A ${kind} with this name already exists`;
  }
  return getErrorMessage(err);
}

const useDeviceSets = () => {
  const { handleAuthErrors } = useAuthErrors();

  const createGroup = useCallback(
    async ({ label, deviceIdentifiers = [], allDevices = false, onSuccess, onError, onFinally }: CreateGroupProps) => {
      try {
        const deviceSelector =
          allDevices || deviceIdentifiers.length > 0 ? buildDeviceSelector(deviceIdentifiers, allDevices) : undefined;

        const createResponse = await deviceSetClient.createDeviceSet({
          type: DeviceSetType.GROUP,
          label,
          deviceSelector,
        });

        const deviceSet = createResponse.deviceSet;
        if (!deviceSet) {
          onError?.("Failed to create group");
          return;
        }

        onSuccess?.(deviceSet);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: (error) => {
            onError?.(getDeviceSetErrorMessage(error, "group"));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const updateGroup = useCallback(
    async ({ deviceSetId, label, deviceIdentifiers, allDevices, onSuccess, onError, onFinally }: UpdateGroupProps) => {
      try {
        const deviceSelector = buildDeviceSelector(deviceIdentifiers, allDevices);

        const response = await deviceSetClient.updateDeviceSet({
          deviceSetId,
          label,
          deviceSelector,
        });

        const deviceSet = response.deviceSet;
        if (!deviceSet) {
          onError?.("Failed to update group");
          return;
        }

        onSuccess?.(deviceSet);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: (error) => {
            onError?.(getDeviceSetErrorMessage(error, "group"));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const deleteGroup = useCallback(
    async ({ deviceSetId, onSuccess, onError, onFinally }: DeleteGroupProps) => {
      try {
        await deviceSetClient.deleteDeviceSet({ deviceSetId });
        onSuccess?.();
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const listGroups = useCallback(
    async ({
      pageSize,
      pageToken,
      sort,
      errorComponentTypes,
      siteIds,
      includeUnassigned,
      telemetryRanges,
      onSuccess,
      onError,
      onFinally,
    }: ListDeviceSetsProps) => {
      try {
        if (pageSize) {
          const response = await deviceSetClient.listDeviceSets({
            type: DeviceSetType.GROUP,
            pageSize,
            pageToken: pageToken ?? "",
            sort,
            errorComponentTypes: errorComponentTypes ?? [],
            siteIds: siteIds ?? [],
            includeUnassigned: includeUnassigned ?? false,
            telemetryRanges: telemetryRanges ?? [],
          });
          onSuccess?.(response.deviceSets, response.nextPageToken, response.totalCount);
        } else {
          // Server caps pageSize at 1000, so we page through all results
          // to support callers that need the full unpaginated list.
          const all: DeviceSet[] = [];
          let nextToken = "";
          do {
            const response = await deviceSetClient.listDeviceSets({
              type: DeviceSetType.GROUP,
              pageSize: 1000,
              pageToken: nextToken,
              sort,
              errorComponentTypes: errorComponentTypes ?? [],
              siteIds: siteIds ?? [],
              includeUnassigned: includeUnassigned ?? false,
              telemetryRanges: telemetryRanges ?? [],
            });
            all.push(...response.deviceSets);
            nextToken = response.nextPageToken;
          } while (nextToken);
          onSuccess?.(all, "", all.length);
        }
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const listRacks = useCallback(
    async ({
      pageSize,
      pageToken,
      sort,
      errorComponentTypes,
      zones,
      buildingIds,
      includeNoBuilding,
      siteIds,
      includeUnassigned,
      telemetryRanges,
      onSuccess,
      onError,
      onFinally,
    }: ListDeviceSetsProps) => {
      try {
        if (pageSize) {
          const response = await deviceSetClient.listDeviceSets({
            type: DeviceSetType.RACK,
            pageSize,
            pageToken: pageToken ?? "",
            sort,
            errorComponentTypes: errorComponentTypes ?? [],
            zones: zones ?? [],
            buildingIds: buildingIds ?? [],
            includeNoBuilding: includeNoBuilding ?? false,
            siteIds: siteIds ?? [],
            includeUnassigned: includeUnassigned ?? false,
            telemetryRanges: telemetryRanges ?? [],
          });
          onSuccess?.(response.deviceSets, response.nextPageToken, response.totalCount);
        } else {
          // Server caps pageSize at 1000, so we page through all results
          // to support callers that need the full unpaginated list.
          const all: DeviceSet[] = [];
          let nextToken = "";
          do {
            const response = await deviceSetClient.listDeviceSets({
              type: DeviceSetType.RACK,
              pageSize: 1000,
              pageToken: nextToken,
              sort,
              errorComponentTypes: errorComponentTypes ?? [],
              zones: zones ?? [],
              buildingIds: buildingIds ?? [],
              includeNoBuilding: includeNoBuilding ?? false,
              siteIds: siteIds ?? [],
              includeUnassigned: includeUnassigned ?? false,
              telemetryRanges: telemetryRanges ?? [],
            });
            all.push(...response.deviceSets);
            nextToken = response.nextPageToken;
          } while (nextToken);
          onSuccess?.(all, "", all.length);
        }
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const getDeviceSet = useCallback(
    async ({ deviceSetId, onSuccess, onNotFound, onError, onFinally }: GetDeviceSetProps) => {
      try {
        const response = await deviceSetClient.getDeviceSet({ deviceSetId });
        const deviceSet = response.deviceSet;
        if (!deviceSet) {
          onNotFound?.();
          return;
        }
        onSuccess?.(deviceSet);
      } catch (err) {
        if (err instanceof ConnectError && err.code === Code.NotFound) {
          onNotFound?.();
        } else {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        }
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const listGroupMembers = useCallback(
    async ({
      deviceSetId,
      siteIds,
      includeUnassigned,
      signal,
      onSuccess,
      onError,
      onFinally,
    }: ListGroupMembersProps) => {
      try {
        const allIdentifiers: string[] = [];
        let pageToken = "";

        do {
          const response = await deviceSetClient.listDeviceSetMembers(
            {
              deviceSetId,
              pageSize: memberPageSize,
              pageToken,
              siteIds: siteIds ?? [],
              includeUnassigned: includeUnassigned ?? false,
            },
            { signal },
          );
          for (const member of response.members) {
            allIdentifiers.push(member.deviceIdentifier);
          }
          pageToken = response.nextPageToken;
        } while (pageToken !== "");

        onSuccess?.(allIdentifiers);
      } catch (err) {
        if (isAbortError(err, signal)) {
          return;
        }

        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const getDeviceSetStats = useCallback(
    async ({ deviceSetIds, onSuccess, onError, onFinally }: GetDeviceSetStatsProps) => {
      try {
        const response = await deviceSetClient.getDeviceSetStats({ deviceSetIds });
        onSuccess?.(response.stats);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  // addDevicesToGroup adds devices to a group (many-to-many). The
  // server rejects non-group targets with InvalidArgument; for rack
  // adds use assignDevicesToRack, which atomically clears any prior
  // rack membership and cascades the rack's site onto the device.
  const addDevicesToGroup = useCallback(
    async ({
      targetGroupId,
      deviceIdentifiers,
      allDevices,
      signal,
      onSuccess,
      onError,
      onFinally,
    }: AddDevicesToGroupProps) => {
      if (!allDevices && (!deviceIdentifiers || deviceIdentifiers.length === 0)) {
        onError?.("No devices selected.");
        onFinally?.();
        return;
      }
      try {
        const deviceSelector = buildDeviceSelector(deviceIdentifiers, allDevices);

        const response = await deviceSetClient.addDevicesToGroup(
          {
            targetGroupId,
            deviceSelector,
          },
          { signal },
        );

        if (signal?.aborted) return;
        onSuccess?.(response.addedCount);
      } catch (err) {
        if (isAbortError(err, signal)) {
          return;
        }
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const createRack = useCallback(
    async ({ label, zone, rows, columns, orderIndex, coolingType, onSuccess, onError, onFinally }: CreateRackProps) => {
      try {
        const rackInfo = create(RackInfoSchema, {
          rows,
          columns,
          zone,
          orderIndex,
          coolingType,
        });

        const createResponse = await deviceSetClient.createDeviceSet({
          type: DeviceSetType.RACK,
          label,
          typeDetails: {
            case: "rackInfo",
            value: rackInfo,
          },
        });

        const deviceSet = createResponse.deviceSet;
        if (!deviceSet) {
          onError?.("Failed to create rack");
          return;
        }

        onSuccess?.(deviceSet);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: (error) => {
            onError?.(getDeviceSetErrorMessage(error, "rack"));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const listRackZones = useCallback(
    async ({ onSuccess, onError, onFinally }: ListRackZonesProps) => {
      try {
        const response = await deviceSetClient.listRackZones({});
        onSuccess?.(response.zones);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const listRackTypes = useCallback(
    async ({ onSuccess, onError, onFinally }: ListRackTypesProps) => {
      try {
        const response = await deviceSetClient.listRackTypes({});
        onSuccess?.(response.rackTypes);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  // assignDevicesToRack wraps the atomic rack-reassignment RPC.
  // Replaces the old client-side remove + add orchestration so a
  // server error / network blip between the two calls can't orphan
  // miners from rack assignment (issue #420). Pass targetRackId
  // unset to clear rack membership without re-assigning.
  const assignDevicesToRack = useCallback(
    async ({
      targetRackId,
      deviceIdentifiers,
      forceClearConflictingSite,
      signal,
      onSuccess,
      onConflicts,
      onError,
      onFinally,
    }: AssignDevicesToRackProps) => {
      try {
        // Always construct the device_list variant of DeviceSelector — the
        // server rejects all_devices for AssignDevicesToRack (moving every
        // paired device into a single rack is never the intended op). The
        // hook contract is `deviceIdentifiers: string[]`, which the caller
        // is responsible for ensuring is non-empty; an empty array still
        // produces InvalidArgument from the server's identifier validation.
        const deviceSelector = create(DeviceSelectorSchema, {
          selectionType: {
            case: "deviceList",
            value: create(DeviceIdentifierListSchema, {
              deviceIdentifiers,
            }),
          },
        });

        const response = await deviceSetClient.assignDevicesToRack(
          {
            targetRackId,
            deviceSelector,
            forceClearConflictingSite,
          },
          { signal },
        );
        if (signal?.aborted) return;
        // Site-strip conflicts: the server wrote nothing and returned the
        // per-device list so the caller can confirm + retry with force.
        if (response.conflicts.length > 0) {
          onConflicts?.(response.conflicts);
          return;
        }
        onSuccess?.(response.assignedCount, response.siteReassignedCount, response.removedCount);
      } catch (err) {
        if (isAbortError(err, signal)) {
          return;
        }
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  // removeDevicesFromGroup drops devices from a group. The server
  // rejects non-group targets with InvalidArgument; for rack removal
  // use assignDevicesToRack with targetRackId unset, which clears rack
  // membership in a single transaction (site/building stay intact).
  const removeDevicesFromGroup = useCallback(
    async ({
      targetGroupId,
      deviceIdentifiers,
      allDevices,
      signal,
      onSuccess,
      onError,
      onFinally,
    }: RemoveDevicesFromGroupProps) => {
      if (!allDevices && (!deviceIdentifiers || deviceIdentifiers.length === 0)) {
        onError?.("No devices selected.");
        onFinally?.();
        return;
      }
      try {
        const deviceSelector = buildDeviceSelector(deviceIdentifiers, allDevices);

        const response = await deviceSetClient.removeDevicesFromGroup(
          {
            targetGroupId,
            deviceSelector,
          },
          { signal },
        );

        if (signal?.aborted) return;
        onSuccess?.(response.removedCount);
      } catch (err) {
        if (isAbortError(err, signal)) {
          return;
        }
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const updateRack = useCallback(
    async ({
      deviceSetId,
      label,
      zone,
      rows,
      columns,
      orderIndex,
      coolingType,
      siteId,
      buildingId,
      onSuccess,
      onError,
      onFinally,
    }: UpdateRackProps) => {
      try {
        // Placement encoding mirrors saveRack: a building fully determines
        // placement (send only building_id), otherwise send whichever of
        // site_id / building_id was specified (0n unassigns, undefined omits).
        const placement: { siteId?: bigint; buildingId?: bigint } = {};
        if (buildingId !== undefined && buildingId > 0n) {
          placement.buildingId = buildingId;
        } else {
          if (siteId !== undefined) placement.siteId = siteId;
          if (buildingId !== undefined) placement.buildingId = buildingId;
        }
        const hasPlacement = placement.siteId !== undefined || placement.buildingId !== undefined;

        const rackInfo =
          hasPlacement ||
          zone !== undefined ||
          rows !== undefined ||
          columns !== undefined ||
          orderIndex !== undefined ||
          coolingType !== undefined
            ? create(RackInfoSchema, {
                ...(zone !== undefined && { zone }),
                ...(rows !== undefined && { rows }),
                ...(columns !== undefined && { columns }),
                ...(orderIndex !== undefined && { orderIndex }),
                ...(coolingType !== undefined && { coolingType }),
                ...placement,
              })
            : undefined;

        const response = await deviceSetClient.updateDeviceSet({
          deviceSetId,
          label,
          ...(rackInfo && {
            typeDetails: {
              case: "rackInfo" as const,
              value: rackInfo,
            },
          }),
        });

        const deviceSet = response.deviceSet;
        if (!deviceSet) {
          onError?.("Failed to update rack");
          return;
        }

        onSuccess?.(deviceSet);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: (error) => {
            onError?.(getDeviceSetErrorMessage(error, "rack"));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const getRackSlots = useCallback(
    async ({ deviceSetId, onSuccess, onError, onFinally }: GetRackSlotsProps) => {
      try {
        const response = await deviceSetClient.getRackSlots({ deviceSetId });
        onSuccess?.(response.slots);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const setRackSlotPosition = useCallback(
    async ({ deviceSetId, deviceIdentifier, position, onSuccess, onError, onFinally }: SetRackSlotPositionProps) => {
      try {
        const response = await deviceSetClient.setRackSlotPosition({
          deviceSetId,
          deviceIdentifier,
          position,
        });

        const slot = response.slot;
        if (!slot) {
          onError?.("Failed to set slot position");
          return;
        }

        onSuccess?.(slot);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const clearRackSlotPosition = useCallback(
    async ({ deviceSetId, deviceIdentifier, onSuccess, onError, onFinally }: ClearRackSlotPositionProps) => {
      try {
        await deviceSetClient.clearRackSlotPosition({
          deviceSetId,
          deviceIdentifier,
        });
        onSuccess?.();
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const saveRack = useCallback(
    async ({
      deviceSetId,
      label,
      zone,
      rows,
      columns,
      orderIndex,
      coolingType,
      deviceIdentifiers,
      allDevices,
      slotAssignments,
      siteId,
      buildingId,
      onSuccess,
      onError,
      onFinally,
    }: SaveRackProps) => {
      try {
        // Placement encoding (see RackInfo proto): a building fully determines
        // placement, so send only building_id and let the server derive
        // site_id. Otherwise send whichever of site_id / building_id the
        // caller specified — an explicit 0 unassigns that level, undefined
        // leaves it untouched (preserved on update).
        const placement: { siteId?: bigint; buildingId?: bigint } = {};
        if (buildingId !== undefined && buildingId > 0n) {
          placement.buildingId = buildingId;
        } else {
          if (siteId !== undefined) placement.siteId = siteId;
          if (buildingId !== undefined) placement.buildingId = buildingId;
        }

        const rackInfo = create(RackInfoSchema, {
          rows,
          columns,
          zone,
          orderIndex,
          coolingType,
          ...placement,
        });

        const deviceSelector = buildDeviceSelector(deviceIdentifiers, allDevices);

        const rackSlots = slotAssignments.map((sa) =>
          create(RackSlotSchema, {
            deviceIdentifier: sa.deviceIdentifier,
            position: create(RackSlotPositionSchema, {
              row: sa.row,
              column: sa.column,
            }),
          }),
        );

        const response = await deviceSetClient.saveRack({
          deviceSetId,
          label,
          rackInfo,
          deviceSelector,
          slotAssignments: rackSlots,
        });

        const deviceSet = response.deviceSet;
        if (!deviceSet) {
          onError?.("Failed to save rack");
          return;
        }

        onSuccess?.(deviceSet, response.assignedCount);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: (error) => {
            onError?.(getDeviceSetErrorMessage(error, "rack"));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  return {
    createGroup,
    createRack,
    updateGroup,
    updateRack,
    deleteGroup,
    getDeviceSet,
    listGroups,
    listRacks,
    listRackZones,
    listRackTypes,
    listGroupMembers,
    getDeviceSetStats,
    addDevicesToGroup,
    assignDevicesToRack,
    removeDevicesFromGroup,
    getRackSlots,
    setRackSlotPosition,
    clearRackSlotPosition,
    saveRack,
  };
};

export { useDeviceSets };
