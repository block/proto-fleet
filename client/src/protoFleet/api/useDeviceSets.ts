import { useCallback } from "react";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { deviceSetClient } from "@/protoFleet/api/clients";
import {
  DeviceIdentifierListSchema,
  DeviceSelectorSchema,
} from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import { type SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  type DeviceSet,
  type DeviceSetStats,
  DeviceSetType,
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

interface ListDeviceSetsProps {
  pageSize?: number;
  pageToken?: string;
  sort?: SortConfig;
  errorComponentTypes?: number[];
  zones?: string[];
  onSuccess?: (deviceSets: DeviceSet[], nextPageToken: string, totalCount: number) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface AddDevicesToDeviceSetProps {
  deviceSetId: bigint;
  deviceIdentifiers?: string[];
  allDevices?: boolean;
  onSuccess?: (addedCount: number) => void;
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
  signal?: AbortSignal;
  onSuccess?: (deviceIdentifiers: string[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface RemoveDevicesFromDeviceSetProps {
  deviceSetId: bigint;
  deviceIdentifiers?: string[];
  allDevices?: boolean;
  onSuccess?: (removedCount: number) => void;
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
    async ({ pageSize, pageToken, sort, errorComponentTypes, onSuccess, onError, onFinally }: ListDeviceSetsProps) => {
      try {
        if (pageSize) {
          const response = await deviceSetClient.listDeviceSets({
            type: DeviceSetType.GROUP,
            pageSize,
            pageToken: pageToken ?? "",
            sort,
            errorComponentTypes: errorComponentTypes ?? [],
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
              zones: zones ?? [],
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
    async ({ deviceSetId, signal, onSuccess, onError, onFinally }: ListGroupMembersProps) => {
      try {
        const allIdentifiers: string[] = [];
        let pageToken = "";

        do {
          const response = await deviceSetClient.listDeviceSetMembers(
            {
              deviceSetId,
              pageSize: memberPageSize,
              pageToken,
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
        if (
          (err instanceof DOMException && err.name === "AbortError") ||
          (err instanceof ConnectError && err.code === Code.Canceled && signal?.aborted)
        ) {
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

  const addDevicesToDeviceSet = useCallback(
    async ({
      deviceSetId,
      deviceIdentifiers,
      allDevices,
      onSuccess,
      onError,
      onFinally,
    }: AddDevicesToDeviceSetProps) => {
      try {
        const deviceSelector =
          allDevices || (deviceIdentifiers && deviceIdentifiers.length > 0)
            ? buildDeviceSelector(deviceIdentifiers, allDevices)
            : undefined;

        const response = await deviceSetClient.addDevicesToDeviceSet({
          deviceSetId,
          deviceSelector,
        });

        onSuccess?.(response.addedCount);
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

  const removeDevicesFromDeviceSet = useCallback(
    async ({
      deviceSetId,
      deviceIdentifiers,
      allDevices,
      onSuccess,
      onError,
      onFinally,
    }: RemoveDevicesFromDeviceSetProps) => {
      try {
        const deviceSelector =
          allDevices || (deviceIdentifiers && deviceIdentifiers.length > 0)
            ? buildDeviceSelector(deviceIdentifiers, allDevices)
            : undefined;

        const response = await deviceSetClient.removeDevicesFromDeviceSet({
          deviceSetId,
          deviceSelector,
        });

        onSuccess?.(response.removedCount);
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

  const updateRack = useCallback(
    async ({
      deviceSetId,
      label,
      zone,
      rows,
      columns,
      orderIndex,
      coolingType,
      onSuccess,
      onError,
      onFinally,
    }: UpdateRackProps) => {
      try {
        const rackInfo =
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
      onSuccess,
      onError,
      onFinally,
    }: SaveRackProps) => {
      try {
        const rackInfo = create(RackInfoSchema, {
          rows,
          columns,
          zone,
          orderIndex,
          coolingType,
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
    addDevicesToDeviceSet,
    removeDevicesFromDeviceSet,
    getRackSlots,
    setRackSlotPosition,
    clearRackSlotPosition,
    saveRack,
  };
};

export { useDeviceSets };
export type { ListDeviceSetsProps };
