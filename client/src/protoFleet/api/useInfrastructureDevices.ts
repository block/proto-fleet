import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { infrastructureClient } from "@/protoFleet/api/clients";
import {
  type InfrastructureDevice as ApiInfrastructureDevice,
  CreateInfrastructureDeviceRequestSchema,
  DeleteInfrastructureDeviceRequestSchema,
  GetInfrastructureDeviceRequestSchema,
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
  rackName: string;
  name: string;
  deviceKind: InfraDeviceKind;
  fanCount: number;
  driverType: string;
  driverConfig: string;
}

// Partial update; only the fields present are changed. The wire update
// RPC is full-row, so updateDevice fetches the device's current row
// first and echoes the fresh values for everything the caller didn't
// touch — replaying a possibly-stale UI snapshot instead could silently
// revert another operator's concurrent edit. (A true concurrency guard
// needs server-side versioning; this shrinks the race window to the
// Get→Update gap.) Requires site:manage, so the fetched driverConfig
// arrives unredacted.
export interface InfrastructureDevicePatch {
  id: string;
  siteId?: string;
  buildingName?: string;
  rackName?: string;
  name?: string;
  enabled?: boolean;
  driverConfig?: string;
}

export type UseInfrastructureDevicesResult = {
  devices: InfraDeviceItem[];
  isLoading: boolean;
  loadError: string | null;
  updatingDeviceIds: ReadonlySet<string>;
  listDevices: (signal?: AbortSignal) => Promise<InfraDeviceItem[]>;
  createDevice: (params: InfrastructureDeviceCreate) => Promise<InfraDeviceItem>;
  updateDevice: (params: InfrastructureDevicePatch) => Promise<InfraDeviceItem>;
  setDeviceEnabled: (device: InfraDeviceItem, enabled: boolean) => Promise<InfraDeviceItem>;
  deleteDevice: (deviceId: string) => Promise<void>;
};

function mapApiDevice(device: ApiInfrastructureDevice): InfraDeviceItem {
  return {
    id: device.id.toString(),
    siteId: device.siteId.toString(),
    siteName: device.siteLabel || getSiteDisplayName(device.siteId),
    buildingName: device.buildingName,
    rackName: device.rackName,
    name: device.name,
    deviceKind: device.deviceKind,
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

// siteIds scopes the list to specific sites (empty/undefined lists the
// whole org). Changing the scope triggers a refetch when the hook is
// enabled.
export default function useInfrastructureDevices(
  enabled = true,
  siteIds?: readonly bigint[],
  requireCurtailmentManage = false,
): UseInfrastructureDevicesResult {
  const { handleAuthErrors } = useAuthErrors();
  const [apiDevices, setApiDevices] = useState<ApiInfrastructureDevice[]>([]);
  const [isLoading, setIsLoading] = useState(enabled);
  const [updatingDeviceIds, setUpdatingDeviceIds] = useState<Set<string>>(() => new Set());
  const [loadError, setLoadError] = useState<string | null>(null);
  // Guards against an older in-flight list call overwriting a newer
  // one's result (e.g. a double-clicked Retry) when responses land
  // out of order.
  const listRequestGenerationRef = useRef(0);
  // Serialized so a caller passing a fresh array identity with the same
  // IDs doesn't churn listDevices (and refetch) every render.
  const siteFilterKey = siteIds?.map((id) => id.toString()).join(",") ?? "";

  const isInScope = useCallback(
    (device: ApiInfrastructureDevice) => !siteFilterKey || siteFilterKey.split(",").includes(device.siteId.toString()),
    [siteFilterKey],
  );

  // Render-time scope filter: the cache can transiently hold rows from
  // another scope — a scope switch renders before the refetch lands,
  // and a mutation started under the previous scope can merge its row
  // through a stale-closure scope check when it resolves late. Filtering
  // here guarantees those rows never surface (driverConfig carries OT
  // connection details, so cross-scope leaks matter).
  const devices = useMemo(() => apiDevices.filter(isInScope).map(mapApiDevice), [apiDevices, isInScope]);

  const handleFailure = useCallback(
    (error: unknown, fallbackMessage: string): Error => {
      const resolvedError = toError(error, fallbackMessage);
      handleAuthErrors({ error });
      return resolvedError;
    },
    [handleAuthErrors],
  );

  // A mutation can return a row outside the active site scope (a create
  // for another site, or an edit that moves the device). The list RPC
  // filters those out, so the cache merge must too — otherwise an
  // out-of-scope device would sit in the cache until the next refetch.
  // The render-time filter above is the visibility guarantee; this keeps
  // the cache itself from accumulating cross-scope rows.

  const upsertApiDevice = useCallback(
    (device: ApiInfrastructureDevice) => {
      setApiDevices((currentDevices) => {
        const otherDevices = currentDevices.filter((currentDevice) => currentDevice.id !== device.id);
        return isInScope(device) ? [device, ...otherDevices] : otherDevices;
      });
    },
    [isInScope],
  );

  const replaceApiDevice = useCallback(
    (device: ApiInfrastructureDevice) => {
      setApiDevices((currentDevices) =>
        isInScope(device)
          ? currentDevices.map((currentDevice) => (currentDevice.id === device.id ? device : currentDevice))
          : currentDevices.filter((currentDevice) => currentDevice.id !== device.id),
      );
    },
    [isInScope],
  );

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
      const generation = ++listRequestGenerationRef.current;
      const isLatest = () => listRequestGenerationRef.current === generation;
      setIsLoading(true);
      setLoadError(null);
      try {
        assertNotAborted(signal);
        const response = await infrastructureClient.listInfrastructureDevices(
          create(ListInfrastructureDevicesRequestSchema, {
            siteIds: siteFilterKey ? siteFilterKey.split(",").map(BigInt) : [],
            requireCurtailmentManage,
          }),
          signal ? { signal } : undefined,
        );
        assertNotAborted(signal);

        if (isLatest()) {
          setApiDevices(response.devices);
          setLoadError(null);
        }
        return response.devices.map(mapApiDevice);
      } catch (error) {
        if (isAbortError(error, signal)) {
          throw error;
        }

        const resolvedError = handleFailure(error, "Failed to load infrastructure devices.");
        if (isLatest()) {
          setLoadError(resolvedError.message);
        }
        throw resolvedError;
      } finally {
        if (isLatest()) {
          setIsLoading(false);
        }
      }
    },
    [handleFailure, requireCurtailmentManage, siteFilterKey],
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
            rackName: params.rackName,
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
    async (params: InfrastructureDevicePatch): Promise<InfraDeviceItem> =>
      withUpdatingDevice(params.id, async () => {
        try {
          const getResponse = await infrastructureClient.getInfrastructureDevice(
            create(GetInfrastructureDeviceRequestSchema, {
              id: parseDeviceId(params.id, "device ID"),
            }),
          );
          const freshDevice = getResponse.device;
          if (!freshDevice) {
            throw new Error("Infrastructure device no longer exists.");
          }

          const response = await infrastructureClient.updateInfrastructureDevice(
            create(UpdateInfrastructureDeviceRequestSchema, {
              id: freshDevice.id,
              siteId: params.siteId !== undefined ? parseDeviceId(params.siteId, "site ID") : freshDevice.siteId,
              buildingName: params.buildingName ?? freshDevice.buildingName,
              rackName: params.rackName,
              name: params.name ?? freshDevice.name,
              // Not patchable from the client: the detail form doesn't
              // edit them, so the fresh row's values are always echoed.
              deviceKind: freshDevice.deviceKind,
              fanCount: freshDevice.fanCount,
              // Omitted (rather than sent as the fetched value) so the
              // server preserves enabled when the caller didn't touch it.
              ...(params.enabled !== undefined ? { enabled: params.enabled } : {}),
              driverType: freshDevice.driverType,
              driverConfig: params.driverConfig ?? freshDevice.driverConfig,
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

  const setDeviceEnabled = useCallback(
    async (device: InfraDeviceItem, nextEnabled: boolean): Promise<InfraDeviceItem> =>
      updateDevice({ id: device.id, enabled: nextEnabled }),
    [updateDevice],
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
