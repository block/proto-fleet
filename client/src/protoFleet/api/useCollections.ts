import { useCallback } from "react";
import { create } from "@bufbuild/protobuf";

import { collectionClient } from "@/protoFleet/api/clients";
import { CollectionType, type DeviceCollection } from "@/protoFleet/api/generated/collection/v1/collection_pb";
import {
  DeviceIdentifierListSchema,
  DeviceSelectorSchema,
} from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import { useAuthErrors } from "@/protoFleet/store";

interface CreateGroupProps {
  label: string;
  deviceIdentifiers?: string[];
  allDevices?: boolean;
  onSuccess?: (collection: DeviceCollection) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListCollectionsProps {
  onSuccess?: (collections: DeviceCollection[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

const useCollections = () => {
  const { handleAuthErrors } = useAuthErrors();

  const createGroup = useCallback(
    async ({ label, deviceIdentifiers = [], allDevices = false, onSuccess, onError, onFinally }: CreateGroupProps) => {
      try {
        // Build device selector if devices are specified
        let deviceSelector;
        if (allDevices) {
          deviceSelector = create(DeviceSelectorSchema, {
            selectionType: {
              case: "allDevices",
              value: true,
            },
          });
        } else if (deviceIdentifiers.length > 0) {
          deviceSelector = create(DeviceSelectorSchema, {
            selectionType: {
              case: "deviceList",
              value: create(DeviceIdentifierListSchema, {
                deviceIdentifiers,
              }),
            },
          });
        }

        // Create collection with devices atomically in a single request
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

  const listGroups = useCallback(
    async ({ onSuccess, onError, onFinally }: ListCollectionsProps) => {
      try {
        const response = await collectionClient.listCollections({
          type: CollectionType.GROUP,
          pageSize: 100,
        });
        onSuccess?.(response.collections);
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
        onSuccess?.(response.collections);
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
    listGroups,
    listRacks,
  };
};

export { useCollections };
