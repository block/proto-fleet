import { useCallback, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { ConnectError } from "@connectrpc/connect";

import { fleetNodeAdminClient } from "@/protoFleet/api/clients";
import {
  type DevicePairingResult,
  type FleetNodeDeviceSummary,
  type FleetNodeDiscoveredDevice,
  FleetNodeDiscoveredDeviceSchema,
  type FleetNodeSummary,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { CredentialsSchema, type Device, type DiscoverRequest } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface ListFleetNodesProps {
  onSuccess?: (nodes: FleetNodeSummary[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface CreateEnrollmentCodeProps {
  onSuccess?: (code: string, expiresAt?: Date) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ConfirmFleetNodeProps {
  fleetNodeId: bigint;
  onSuccess?: (apiKey: string) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface RevokeFleetNodeProps {
  fleetNodeId: bigint;
  onSuccess?: () => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface DiscoverOnFleetNodeProps {
  fleetNodeId: bigint;
  request: DiscoverRequest;
  abortController?: AbortController;
  onStreamData: (devices: FleetNodeDiscoveredDevice[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListDiscoveredDevicesProps {
  fleetNodeId: bigint;
  cursor?: bigint;
  limit?: number;
  onSuccess?: (devices: FleetNodeDiscoveredDevice[], nextCursor: bigint) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface PairDiscoveredDevicesProps {
  fleetNodeId: bigint;
  deviceIdentifiers?: string[];
  pairAllUnpaired?: boolean;
  username?: string;
  password?: string;
  abortController?: AbortController;
  onResult: (results: DevicePairingResult[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListFleetNodeDevicesProps {
  fleetNodeId: bigint;
  onSuccess?: (pairs: FleetNodeDeviceSummary[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

// DiscoverOnFleetNode streams pairing.v1.DiscoverResponse, whose Device shape
// differs from the FleetNodeDiscoveredDevice rows the list endpoint returns.
// Project the streamed devices onto the row shape so the UI renders one type.
const discoveredFromStream = (fleetNodeId: bigint, devices: Device[]): FleetNodeDiscoveredDevice[] =>
  devices.map((d) =>
    create(FleetNodeDiscoveredDeviceSchema, {
      fleetNodeId,
      deviceIdentifier: d.deviceIdentifier,
      ipAddress: d.ipAddress,
      port: d.port,
      urlScheme: d.urlScheme,
      driverName: d.driverName,
      model: d.model,
      manufacturer: d.manufacturer,
      firmwareVersion: d.firmwareVersion,
    }),
  );

const useFleetNodes = () => {
  const { handleAuthErrors } = useAuthErrors();

  const [discoverPending, setDiscoverPending] = useState(false);
  const [pairingPending, setPairingPending] = useState(false);

  const handleStreamError = useCallback(
    (error: unknown, abortController: AbortController | undefined, onError?: (message: string) => void) => {
      if ((error instanceof DOMException && error.name === "AbortError") || abortController?.signal.aborted) {
        return;
      }
      if (error instanceof ConnectError) {
        handleAuthErrors({
          error,
          onError: () => onError?.(getErrorMessage(error, "An unexpected error occurred")),
        });
      } else {
        onError?.(getErrorMessage(error, "An unexpected error occurred"));
      }
    },
    [handleAuthErrors],
  );

  const listFleetNodes = useCallback(
    async ({ onSuccess, onError, onFinally }: ListFleetNodesProps) => {
      try {
        const response = await fleetNodeAdminClient.listFleetNodes({});
        onSuccess?.(response.fleetNodes);
      } catch (err) {
        handleAuthErrors({ error: err, onError: () => onError?.(getErrorMessage(err)) });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const createEnrollmentCode = useCallback(
    async ({ onSuccess, onError, onFinally }: CreateEnrollmentCodeProps) => {
      try {
        const response = await fleetNodeAdminClient.createEnrollmentCode({});
        const expiresAt = response.expiresAt ? new Date(Number(response.expiresAt.seconds) * 1000) : undefined;
        onSuccess?.(response.code, expiresAt);
      } catch (err) {
        handleAuthErrors({ error: err, onError: () => onError?.(getErrorMessage(err)) });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const confirmFleetNode = useCallback(
    async ({ fleetNodeId, onSuccess, onError, onFinally }: ConfirmFleetNodeProps) => {
      try {
        const response = await fleetNodeAdminClient.confirmFleetNode({ fleetNodeId });
        onSuccess?.(response.apiKey);
      } catch (err) {
        handleAuthErrors({ error: err, onError: () => onError?.(getErrorMessage(err)) });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const revokeFleetNode = useCallback(
    async ({ fleetNodeId, onSuccess, onError, onFinally }: RevokeFleetNodeProps) => {
      try {
        await fleetNodeAdminClient.revokeFleetNode({ fleetNodeId });
        onSuccess?.();
      } catch (err) {
        handleAuthErrors({ error: err, onError: () => onError?.(getErrorMessage(err)) });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const discoverOnFleetNode = useCallback(
    async ({ fleetNodeId, request, abortController, onStreamData, onError, onFinally }: DiscoverOnFleetNodeProps) => {
      setDiscoverPending(true);
      try {
        for await (const response of fleetNodeAdminClient.discoverOnFleetNode(
          { fleetNodeId, request },
          { signal: abortController?.signal },
        )) {
          if (response.response?.error) {
            onError?.(response.response.error);
            break;
          }
          if (response.response?.devices?.length) {
            onStreamData(discoveredFromStream(fleetNodeId, response.response.devices));
          }
        }
      } catch (error) {
        handleStreamError(error, abortController, onError);
      } finally {
        setDiscoverPending(false);
        onFinally?.();
      }
    },
    [handleStreamError],
  );

  const listDiscoveredDevices = useCallback(
    async ({ fleetNodeId, cursor, limit, onSuccess, onError, onFinally }: ListDiscoveredDevicesProps) => {
      try {
        const response = await fleetNodeAdminClient.listFleetNodeDiscoveredDevices({
          fleetNodeId,
          cursor: cursor ?? 0n,
          limit: limit ?? 0,
        });
        onSuccess?.(response.devices, response.nextCursor);
      } catch (err) {
        handleAuthErrors({ error: err, onError: () => onError?.(getErrorMessage(err)) });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const pairDiscoveredDevices = useCallback(
    async ({
      fleetNodeId,
      deviceIdentifiers,
      pairAllUnpaired,
      username,
      password,
      abortController,
      onResult,
      onError,
      onFinally,
    }: PairDiscoveredDevicesProps) => {
      setPairingPending(true);
      // Credentials are only relevant for basic-auth devices; omit entirely
      // when no username is given so asymmetric-auth drivers pair as-is.
      const credentials = username ? create(CredentialsSchema, { username, password }) : undefined;
      try {
        for await (const response of fleetNodeAdminClient.pairDiscoveredDevicesOnFleetNode(
          {
            fleetNodeId,
            deviceIdentifiers: deviceIdentifiers ?? [],
            pairAllUnpaired: pairAllUnpaired ?? false,
            credentials,
          },
          { signal: abortController?.signal },
        )) {
          if (response.results?.length) {
            onResult(response.results);
          }
        }
      } catch (error) {
        handleStreamError(error, abortController, onError);
      } finally {
        setPairingPending(false);
        onFinally?.();
      }
    },
    [handleStreamError],
  );

  const listFleetNodeDevices = useCallback(
    async ({ fleetNodeId, onSuccess, onError, onFinally }: ListFleetNodeDevicesProps) => {
      try {
        const response = await fleetNodeAdminClient.listFleetNodeDevices({ fleetNodeId });
        onSuccess?.(response.pairs);
      } catch (err) {
        handleAuthErrors({ error: err, onError: () => onError?.(getErrorMessage(err)) });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  return useMemo(
    () => ({
      listFleetNodes,
      createEnrollmentCode,
      confirmFleetNode,
      revokeFleetNode,
      discoverPending,
      discoverOnFleetNode,
      listDiscoveredDevices,
      pairingPending,
      pairDiscoveredDevices,
      listFleetNodeDevices,
    }),
    [
      listFleetNodes,
      createEnrollmentCode,
      confirmFleetNode,
      revokeFleetNode,
      discoverPending,
      discoverOnFleetNode,
      listDiscoveredDevices,
      pairingPending,
      pairDiscoveredDevices,
      listFleetNodeDevices,
    ],
  );
};

export { useFleetNodes };
