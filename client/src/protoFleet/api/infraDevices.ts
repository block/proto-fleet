import { useCallback } from "react";

// import { infraDeviceClient } from "@/protoFleet/api/clients";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface ListDevicesProps {
  filter?: Record<string, unknown>;
  pageSize?: number;
  pageToken?: string;
  signal?: AbortSignal;
  onSuccess?: (devices: unknown[], nextPageToken: string, totalCount: number) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface GetDeviceProps {
  id: bigint;
  signal?: AbortSignal;
  onSuccess?: (device: unknown) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface CreateDeviceProps {
  name: string;
  deviceType: number;
  subtype?: string;
  siteId?: bigint;
  buildingId?: bigint;
  ipAddress?: string;
  controlMode?: number;
  protocol?: string;
  signal?: AbortSignal;
  onSuccess?: (device: unknown) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface UpdateDeviceProps {
  id: bigint;
  name?: string;
  ipAddress?: string;
  controlMode?: number;
  signal?: AbortSignal;
  onSuccess?: (device: unknown) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface DeleteDeviceProps {
  id: bigint;
  signal?: AbortSignal;
  onSuccess?: () => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface BulkProps {
  deviceIds: bigint[];
  controlMode?: number;
  signal?: AbortSignal;
  onSuccess?: (count: number) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface TestConnectionProps {
  id: bigint;
  signal?: AbortSignal;
  onSuccess?: (reachable: boolean, latencyMs: number) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ScanNetworkProps {
  siteId?: bigint;
  buildingId?: bigint;
  signal?: AbortSignal;
  onSuccess?: (discovered: unknown[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface GetStatsProps {
  siteId?: bigint;
  buildingId?: bigint;
  signal?: AbortSignal;
  onSuccess?: (stats: unknown) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

export const useInfraDeviceApi = () => {
  const { handleAuthErrors } = useAuthErrors();

  const listDevices = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: ListDevicesProps) => {
      try {
        // TODO: wire to infraDeviceClient.listInfraDevices
        if (signal?.aborted) return;
        onSuccess?.([], "", 0);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const getDevice = useCallback(
    async ({ id, signal, onSuccess, onError, onFinally }: GetDeviceProps) => {
      try {
        // TODO: wire to infraDeviceClient.getInfraDevice
        if (signal?.aborted) return;
        onSuccess?.(undefined);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const createDevice = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: CreateDeviceProps) => {
      try {
        // TODO: wire to infraDeviceClient.createInfraDevice
        if (signal?.aborted) return;
        onSuccess?.(undefined);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const updateDevice = useCallback(
    async ({ id, signal, onSuccess, onError, onFinally }: UpdateDeviceProps) => {
      try {
        // TODO: wire to infraDeviceClient.updateInfraDevice
        if (signal?.aborted) return;
        onSuccess?.(undefined);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const deleteDevice = useCallback(
    async ({ id, signal, onSuccess, onError, onFinally }: DeleteDeviceProps) => {
      try {
        // TODO: wire to infraDeviceClient.deleteInfraDevice
        if (signal?.aborted) return;
        onSuccess?.();
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const bulkUpdateControlMode = useCallback(
    async ({ deviceIds, controlMode, signal, onSuccess, onError, onFinally }: BulkProps) => {
      try {
        // TODO: wire to infraDeviceClient.bulkUpdateControlMode
        if (signal?.aborted) return;
        onSuccess?.(0);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const bulkDelete = useCallback(
    async ({ deviceIds, signal, onSuccess, onError, onFinally }: BulkProps) => {
      try {
        // TODO: wire to infraDeviceClient.bulkDeleteInfraDevices
        if (signal?.aborted) return;
        onSuccess?.(0);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const testConnection = useCallback(
    async ({ id, signal, onSuccess, onError, onFinally }: TestConnectionProps) => {
      try {
        // TODO: wire to infraDeviceClient.testInfraDeviceConnection
        if (signal?.aborted) return;
        onSuccess?.(true, 0);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const scanNetwork = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: ScanNetworkProps) => {
      try {
        // TODO: wire to infraDeviceClient.scanNetwork
        if (signal?.aborted) return;
        onSuccess?.([]);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const getStats = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: GetStatsProps) => {
      try {
        // TODO: wire to infraDeviceClient.getInfraDeviceStats
        if (signal?.aborted) return;
        onSuccess?.(undefined);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  return {
    listDevices,
    getDevice,
    createDevice,
    updateDevice,
    deleteDevice,
    bulkUpdateControlMode,
    bulkDelete,
    testConnection,
    scanNetwork,
    getStats,
  };
};
