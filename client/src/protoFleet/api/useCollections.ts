import { useCallback } from "react";
import { create } from "@bufbuild/protobuf";

import { collectionClient } from "@/protoFleet/api/clients";
import {
  type CollectionStats,
  CollectionType,
  type DeviceCollection,
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

interface ListGroupMembersProps {
  collectionId: bigint;
  onSuccess?: (deviceIdentifiers: string[]) => void;
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
    async ({ pageSize, pageToken, sort, onSuccess, onError, onFinally }: ListCollectionsProps) => {
      try {
        if (pageSize) {
          const response = await collectionClient.listCollections({
            type: CollectionType.GROUP,
            pageSize,
            pageToken: pageToken ?? "",
            sort,
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
    async ({ onSuccess, onError, onFinally }: ListCollectionsProps) => {
      try {
        const response = await collectionClient.listCollections({
          type: CollectionType.RACK,
          pageSize: 100,
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

  return {
    createGroup,
    updateGroup,
    deleteGroup,
    listGroups,
    listRacks,
    listGroupMembers,
    getCollectionStats,
    addDevicesToCollection,
  };
};

export { useCollections };
