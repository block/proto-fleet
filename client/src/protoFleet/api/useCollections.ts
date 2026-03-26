import { useCallback } from "react";
import { create } from "@bufbuild/protobuf";

import { collectionClient } from "@/protoFleet/api/clients";
import {
  type CollectionStats,
  CollectionType,
  type DeviceCollection,
  type RackCoolingType,
  RackInfoSchema,
  type RackOrderIndex,
  type RackSlot,
  type RackSlotPosition,
  RackSlotPositionSchema,
  RackSlotSchema,
  type RackType,
} from "@/protoFleet/api/generated/collection/v1/collection_pb";
import {
  DeviceIdentifierListSchema,
  DeviceSelectorSchema,
} from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import { type SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import { useAuthErrors } from "@/protoFleet/store";

interface CreateGroupProps {
  label: string;
  deviceIdentifiers?: string[];
  allDevices?: boolean;
  onSuccess?: (collection: DeviceCollection) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface UpdateGroupProps {
  collectionId: bigint;
  label?: string;
  deviceIdentifiers?: string[];
  allDevices?: boolean;
  onSuccess?: (collection: DeviceCollection) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface DeleteGroupProps {
  collectionId: bigint;
  onSuccess?: () => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListCollectionsProps {
  pageSize?: number;
  pageToken?: string;
  sort?: SortConfig;
  errorComponentTypes?: number[];
  locations?: string[];
  onSuccess?: (collections: DeviceCollection[], nextPageToken: string, totalCount: number) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface AddDevicesToCollectionProps {
  collectionId: bigint;
  deviceIdentifiers?: string[];
  allDevices?: boolean;
  onSuccess?: (addedCount: number) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface GetCollectionStatsProps {
  collectionIds: bigint[];
  onSuccess?: (stats: CollectionStats[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface CreateRackProps {
  label: string;
  location: string;
  rows: number;
  columns: number;
  orderIndex: RackOrderIndex;
  coolingType: RackCoolingType;
  onSuccess?: (collection: DeviceCollection) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListRackLocationsProps {
  onSuccess?: (locations: string[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListRackTypesProps {
  onSuccess?: (rackTypes: RackType[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListGroupMembersProps {
  collectionId: bigint;
  onSuccess?: (deviceIdentifiers: string[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface RemoveDevicesFromCollectionProps {
  collectionId: bigint;
  deviceIdentifiers?: string[];
  allDevices?: boolean;
  onSuccess?: (removedCount: number) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface UpdateRackProps {
  collectionId: bigint;
  label?: string;
  location?: string;
  rows?: number;
  columns?: number;
  orderIndex?: RackOrderIndex;
  coolingType?: RackCoolingType;
  onSuccess?: (collection: DeviceCollection) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface GetRackSlotsProps {
  collectionId: bigint;
  onSuccess?: (slots: RackSlot[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface SetRackSlotPositionProps {
  collectionId: bigint;
  deviceIdentifier: string;
  position: RackSlotPosition;
  onSuccess?: (slot: RackSlot) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ClearRackSlotPositionProps {
  collectionId: bigint;
  deviceIdentifier: string;
  onSuccess?: () => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface SaveRackProps {
  collectionId?: bigint;
  label: string;
  location: string;
  rows: number;
  columns: number;
  orderIndex: RackOrderIndex;
  coolingType: RackCoolingType;
  deviceIdentifiers: string[];
  allDevices?: boolean;
  slotAssignments: { deviceIdentifier: string; row: number; column: number }[];
  onSuccess?: (collection: DeviceCollection, assignedCount: number) => void;
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

const useCollections = () => {
  const { handleAuthErrors } = useAuthErrors();

  const createGroup = useCallback(
    async ({ label, deviceIdentifiers = [], allDevices = false, onSuccess, onError, onFinally }: CreateGroupProps) => {
      try {
        const deviceSelector =
          allDevices || deviceIdentifiers.length > 0 ? buildDeviceSelector(deviceIdentifiers, allDevices) : undefined;

        const createResponse = await collectionClient.createCollection({
          type: CollectionType.GROUP,
          label,
          deviceSelector,
        });

        const collection = createResponse.collection;
        if (!collection) {
          onError?.("Failed to create group");
          return;
        }

        onSuccess?.(collection);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const updateGroup = useCallback(
    async ({ collectionId, label, deviceIdentifiers, allDevices, onSuccess, onError, onFinally }: UpdateGroupProps) => {
      try {
        const deviceSelector = buildDeviceSelector(deviceIdentifiers, allDevices);

        const response = await collectionClient.updateCollection({
          collectionId,
          label,
          deviceSelector,
        });

        const collection = response.collection;
        if (!collection) {
          onError?.("Failed to update group");
          return;
        }

        onSuccess?.(collection);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const deleteGroup = useCallback(
    async ({ collectionId, onSuccess, onError, onFinally }: DeleteGroupProps) => {
      try {
        await collectionClient.deleteCollection({ collectionId });
        onSuccess?.();
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const listGroups = useCallback(
    async ({ pageSize, pageToken, sort, errorComponentTypes, onSuccess, onError, onFinally }: ListCollectionsProps) => {
      try {
        if (pageSize) {
          const response = await collectionClient.listCollections({
            type: CollectionType.GROUP,
            pageSize,
            pageToken: pageToken ?? "",
            sort,
            errorComponentTypes: errorComponentTypes ?? [],
          });
          onSuccess?.(response.collections, response.nextPageToken, response.totalCount);
        } else {
          // Server caps pageSize at 1000, so we page through all results
          // to support callers that need the full unpaginated list.
          const all: DeviceCollection[] = [];
          let nextToken = "";
          do {
            const response = await collectionClient.listCollections({
              type: CollectionType.GROUP,
              pageSize: 1000,
              pageToken: nextToken,
              sort,
            });
            all.push(...response.collections);
            nextToken = response.nextPageToken;
          } while (nextToken);
          onSuccess?.(all, "", all.length);
        }
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
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
      locations,
      onSuccess,
      onError,
      onFinally,
    }: ListCollectionsProps) => {
      try {
        const response = await collectionClient.listCollections({
          type: CollectionType.RACK,
          pageSize: pageSize ?? 100,
          pageToken: pageToken ?? "",
          sort,
          errorComponentTypes: errorComponentTypes ?? [],
          locations: locations ?? [],
        });
        onSuccess?.(response.collections, response.nextPageToken, response.totalCount);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const listGroupMembers = useCallback(
    async ({ collectionId, onSuccess, onError, onFinally }: ListGroupMembersProps) => {
      try {
        const allIdentifiers: string[] = [];
        let pageToken = "";

        do {
          const response = await collectionClient.listCollectionMembers({
            collectionId,
            pageSize: memberPageSize,
            pageToken,
          });
          for (const member of response.members) {
            allIdentifiers.push(member.deviceIdentifier);
          }
          pageToken = response.nextPageToken;
        } while (pageToken !== "");

        onSuccess?.(allIdentifiers);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const getCollectionStats = useCallback(
    async ({ collectionIds, onSuccess, onError, onFinally }: GetCollectionStatsProps) => {
      try {
        const response = await collectionClient.getCollectionStats({ collectionIds });
        onSuccess?.(response.stats);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const addDevicesToCollection = useCallback(
    async ({
      collectionId,
      deviceIdentifiers,
      allDevices,
      onSuccess,
      onError,
      onFinally,
    }: AddDevicesToCollectionProps) => {
      try {
        const deviceSelector =
          allDevices || (deviceIdentifiers && deviceIdentifiers.length > 0)
            ? buildDeviceSelector(deviceIdentifiers, allDevices)
            : undefined;

        const response = await collectionClient.addDevicesToCollection({
          collectionId,
          deviceSelector,
        });

        onSuccess?.(response.addedCount);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const createRack = useCallback(
    async ({
      label,
      location,
      rows,
      columns,
      orderIndex,
      coolingType,
      onSuccess,
      onError,
      onFinally,
    }: CreateRackProps) => {
      try {
        const rackInfo = create(RackInfoSchema, {
          rows,
          columns,
          location,
          orderIndex,
          coolingType,
        });

        const createResponse = await collectionClient.createCollection({
          type: CollectionType.RACK,
          label,
          typeDetails: {
            case: "rackInfo",
            value: rackInfo,
          },
        });

        const collection = createResponse.collection;
        if (!collection) {
          onError?.("Failed to create rack");
          return;
        }

        onSuccess?.(collection);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const listRackLocations = useCallback(
    async ({ onSuccess, onError, onFinally }: ListRackLocationsProps) => {
      try {
        const response = await collectionClient.listRackLocations({});
        onSuccess?.(response.locations);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
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
        const response = await collectionClient.listRackTypes({});
        onSuccess?.(response.rackTypes);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const removeDevicesFromCollection = useCallback(
    async ({
      collectionId,
      deviceIdentifiers,
      allDevices,
      onSuccess,
      onError,
      onFinally,
    }: RemoveDevicesFromCollectionProps) => {
      try {
        const deviceSelector =
          allDevices || (deviceIdentifiers && deviceIdentifiers.length > 0)
            ? buildDeviceSelector(deviceIdentifiers, allDevices)
            : undefined;

        const response = await collectionClient.removeDevicesFromCollection({
          collectionId,
          deviceSelector,
        });

        onSuccess?.(response.removedCount);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
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
      collectionId,
      label,
      location,
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
          location !== undefined ||
          rows !== undefined ||
          columns !== undefined ||
          orderIndex !== undefined ||
          coolingType !== undefined
            ? create(RackInfoSchema, {
                ...(location !== undefined && { location }),
                ...(rows !== undefined && { rows }),
                ...(columns !== undefined && { columns }),
                ...(orderIndex !== undefined && { orderIndex }),
                ...(coolingType !== undefined && { coolingType }),
              })
            : undefined;

        const response = await collectionClient.updateCollection({
          collectionId,
          label,
          ...(rackInfo && {
            typeDetails: {
              case: "rackInfo" as const,
              value: rackInfo,
            },
          }),
        });

        const collection = response.collection;
        if (!collection) {
          onError?.("Failed to update rack");
          return;
        }

        onSuccess?.(collection);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const getRackSlots = useCallback(
    async ({ collectionId, onSuccess, onError, onFinally }: GetRackSlotsProps) => {
      try {
        const response = await collectionClient.getRackSlots({ collectionId });
        onSuccess?.(response.slots);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const setRackSlotPosition = useCallback(
    async ({ collectionId, deviceIdentifier, position, onSuccess, onError, onFinally }: SetRackSlotPositionProps) => {
      try {
        const response = await collectionClient.setRackSlotPosition({
          collectionId,
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
            onError?.((err as Error)?.message ?? String(err));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const clearRackSlotPosition = useCallback(
    async ({ collectionId, deviceIdentifier, onSuccess, onError, onFinally }: ClearRackSlotPositionProps) => {
      try {
        await collectionClient.clearRackSlotPosition({
          collectionId,
          deviceIdentifier,
        });
        onSuccess?.();
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
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
      collectionId,
      label,
      location,
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
          location,
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

        const response = await collectionClient.saveRack({
          collectionId,
          label,
          rackInfo,
          deviceSelector,
          slotAssignments: rackSlots,
        });

        const collection = response.collection;
        if (!collection) {
          onError?.("Failed to save rack");
          return;
        }

        onSuccess?.(collection, response.assignedCount);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.((err as Error)?.message ?? String(err));
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
    listGroups,
    listRacks,
    listRackLocations,
    listRackTypes,
    listGroupMembers,
    getCollectionStats,
    addDevicesToCollection,
    removeDevicesFromCollection,
    getRackSlots,
    setRackSlotPosition,
    clearRackSlotPosition,
    saveRack,
  };
};

export { useCollections };
export type { ListCollectionsProps };
