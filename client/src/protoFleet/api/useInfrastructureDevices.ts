import { useCallback, useEffect, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { infrastructureClient } from "@/protoFleet/api/clients";
import {
  type InfrastructureDevice as ApiInfrastructureDevice,
  CreateInfrastructureDeviceRequestSchema,
  DeleteInfrastructureDeviceRequestSchema,
  ListInfrastructureDevicesRequestSchema,
  UpdateInfrastructureDeviceRequestSchema,
} from "@/protoFleet/api/generated/infrastructure/v1/infrastructure_pb";
import { assertNotAborted, isAbortError, toError } from "@/protoFleet/api/requestErrors";
import { getSiteDisplayName } from "@/protoFleet/api/siteNames";
import type { InfraDeviceItem, InfraDeviceKind } from "@/protoFleet/features/infrastructure/types";
import { useAuthErrors } from "@/protoFleet/store";

// Create payload with the site already resolved to an ID (the add modal
// works with catalog site names; the page translates before calling).
export interface InfrastructureDeviceCreate {
  siteId: string;
  buildingName: string;
  name: string;
  deviceKind: InfraDeviceKind;
  fanCount: number;
  driverType: string;
  driverConfig: string;
}

// Full-row update; the server requires every field except enabled,
// which preserves the stored value when omitted.
export interface InfrastructureDeviceUpdate extends InfrastructureDeviceCreate {
  id: string;
  enabled?: boolean;
}

export type UseInfrastructureDevicesResult = {
  devices: InfraDeviceItem[];
  isLoading: boolean;
  loadError: string | null;
  updatingDeviceIds: ReadonlySet<string>;
  listDevices: (signal?: AbortSignal) => Promise<InfraDeviceItem[]>;
  createDevice: (params: InfrastructureDeviceCreate) => Promise<InfraDeviceItem>;
  updateDevice: (params: InfrastructureDeviceUpdate) => Promise<InfraDeviceItem>;
  setDeviceEnabled: (device: InfraDeviceItem, enabled: boolean) => Promise<InfraDeviceItem>;
  deleteDevice: (deviceId: string) => Promise<void>;
};

function mapApiDevice(device: ApiInfrastructureDevice): InfraDeviceItem {
  return {
    id: device.id.toString(),
    siteId: device.siteId.toString(),
    siteName: device.siteLabel || getSiteDisplayName(device.siteId),
    buildingName: device.buildingName,
    name: device.name,
    deviceKind: device.deviceKind === "fan_group" ? "fan_group" : "single_fan",
    fanCount: device.fanCount,
    enabled: device.enabled,
    driverType: device.driverType,
    driverConfig: device.driverConfig,
  };
}

function parseDeviceId(value: string, label: string): bigint {
  if (!/^[1-9]\d*$/.test(value)) {
    throw new Error(`Invalid ${label}.`);
  }
  return BigInt(value);
}

function updateRequestFromDevice(device: InfraDeviceItem) {
  return {
    id: parseDeviceId(device.id, "device ID"),
    siteId: parseDeviceId(device.siteId, "site ID"),
    buildingName: device.buildingName,
    name: device.name,
    deviceKind: device.deviceKind,
    fanCount: device.fanCount,
    driverType: device.driverType,
    driverConfig: device.driverConfig,
  };
}

export default function useInfrastructureDevices(enabled = true): UseInfrastructureDevicesResult {
  const { handleAuthErrors } = useAuthErrors();
  const [apiDevices, setApiDevices] = useState<ApiInfrastructureDevice[]>([]);
  const [isLoading, setIsLoading] = useState(enabled);
  const [updatingDeviceIds, setUpdatingDeviceIds] = useState<Set<string>>(() => new Set());
  const [loadError, setLoadError] = useState<string | null>(null);

  const devices = useMemo(() => apiDevices.map(mapApiDevice), [apiDevices]);

  const handleFailure = useCallback(
    (error: unknown, fallbackMessage: string): Error => {
      const resolvedError = toError(error, fallbackMessage);
      handleAuthErrors({ error });
      return resolvedError;
    },
    [handleAuthErrors],
  );

  const upsertApiDevice = useCallback((device: ApiInfrastructureDevice) => {
    setApiDevices((currentDevices) => [
      device,
      ...currentDevices.filter((currentDevice) => currentDevice.id !== device.id),
    ]);
  }, []);

  const replaceApiDevice = useCallback((device: ApiInfrastructureDevice) => {
    setApiDevices((currentDevices) =>
      currentDevices.map((currentDevice) => (currentDevice.id === device.id ? device : currentDevice)),
    );
  }, []);

  const withUpdatingDevice = useCallback(async <T>(deviceId: string, run: () => Promise<T>): Promise<T> => {
    setUpdatingDeviceIds((currentIds) => new Set(currentIds).add(deviceId));
    try {
      return await run();
    } finally {
      setUpdatingDeviceIds((currentIds) => {
        const nextIds = new Set(currentIds);
        nextIds.delete(deviceId);
        return nextIds;
      });
    }
  }, []);

  const listDevices = useCallback(
    async (signal?: AbortSignal): Promise<InfraDeviceItem[]> => {
      setIsLoading(true);
      try {
        assertNotAborted(signal);
        const response = await infrastructureClient.listInfrastructureDevices(
          create(ListInfrastructureDevicesRequestSchema, {}),
          signal ? { signal } : undefined,
        );
        assertNotAborted(signal);

        setApiDevices(response.devices);
        setLoadError(null);
        return response.devices.map(mapApiDevice);
      } catch (error) {
        if (isAbortError(error, signal)) {
          throw error;
        }

        const resolvedError = handleFailure(error, "Failed to load infrastructure devices.");
        setLoadError(resolvedError.message);
        throw resolvedError;
      } finally {
        setIsLoading(false);
      }
    },
    [handleFailure],
  );

  useEffect(() => {
    if (!enabled) {
      return;
    }

    const abortController = new AbortController();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch on mount; setState inside async fetch is the external-sync pattern
    void listDevices(abortController.signal).catch(() => {});

    return () => {
      abortController.abort();
    };
  }, [enabled, listDevices]);

  const createDevice = useCallback(
    async (params: InfrastructureDeviceCreate): Promise<InfraDeviceItem> => {
      try {
        const response = await infrastructureClient.createInfrastructureDevice(
          create(CreateInfrastructureDeviceRequestSchema, {
            siteId: parseDeviceId(params.siteId, "site ID"),
            buildingName: params.buildingName,
            name: params.name,
            deviceKind: params.deviceKind,
            fanCount: params.fanCount,
            driverType: params.driverType,
            driverConfig: params.driverConfig,
          }),
        );
        if (!response.device) {
          throw new Error("Created infrastructure device response was missing a device.");
        }

        upsertApiDevice(response.device);
        return mapApiDevice(response.device);
      } catch (error) {
        throw handleFailure(error, "Failed to add infrastructure device.");
      }
    },
    [handleFailure, upsertApiDevice],
  );

  const updateDevice = useCallback(
    async (params: InfrastructureDeviceUpdate): Promise<InfraDeviceItem> =>
      withUpdatingDevice(params.id, async () => {
        try {
          const response = await infrastructureClient.updateInfrastructureDevice(
            create(UpdateInfrastructureDeviceRequestSchema, {
              id: parseDeviceId(params.id, "device ID"),
              siteId: parseDeviceId(params.siteId, "site ID"),
              buildingName: params.buildingName,
              name: params.name,
              deviceKind: params.deviceKind,
              fanCount: params.fanCount,
              enabled: params.enabled,
              driverType: params.driverType,
              driverConfig: params.driverConfig,
            }),
          );
          if (!response.device) {
            throw new Error("Updated infrastructure device response was missing a device.");
          }

          replaceApiDevice(response.device);
          return mapApiDevice(response.device);
        } catch (error) {
          throw handleFailure(error, "Failed to update infrastructure device.");
        }
      }),
    [handleFailure, replaceApiDevice, withUpdatingDevice],
  );

  // The update RPC is a full-row write, so the toggle resends the
  // device's current fields with the new enabled value. Requires
  // site:manage (which also means driverConfig arrived unredacted).
  const setDeviceEnabled = useCallback(
    async (device: InfraDeviceItem, nextEnabled: boolean): Promise<InfraDeviceItem> =>
      withUpdatingDevice(device.id, async () => {
        try {
          const response = await infrastructureClient.updateInfrastructureDevice(
            create(UpdateInfrastructureDeviceRequestSchema, {
              ...updateRequestFromDevice(device),
              enabled: nextEnabled,
            }),
          );
          if (!response.device) {
            throw new Error("Updated infrastructure device response was missing a device.");
          }

          replaceApiDevice(response.device);
          return mapApiDevice(response.device);
        } catch (error) {
          throw handleFailure(error, "Failed to update infrastructure device.");
        }
      }),
    [handleFailure, replaceApiDevice, withUpdatingDevice],
  );

  const deleteDevice = useCallback(
    async (deviceId: string): Promise<void> =>
      withUpdatingDevice(deviceId, async () => {
        try {
          await infrastructureClient.deleteInfrastructureDevice(
            create(DeleteInfrastructureDeviceRequestSchema, {
              id: parseDeviceId(deviceId, "device ID"),
            }),
          );
          setApiDevices((currentDevices) =>
            currentDevices.filter((currentDevice) => currentDevice.id.toString() !== deviceId),
          );
        } catch (error) {
          throw handleFailure(error, "Failed to delete infrastructure device.");
        }
      }),
    [handleFailure, withUpdatingDevice],
  );

  return useMemo(
    () => ({
      devices,
      isLoading: enabled ? isLoading : false,
      loadError,
      updatingDeviceIds,
      listDevices,
      createDevice,
      updateDevice,
      setDeviceEnabled,
      deleteDevice,
    }),
    [
      devices,
      enabled,
      isLoading,
      loadError,
      updatingDeviceIds,
      listDevices,
      createDevice,
      updateDevice,
      setDeviceEnabled,
      deleteDevice,
    ],
  );
}
