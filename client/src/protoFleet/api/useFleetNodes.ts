import { useCallback } from "react";

import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { fleetNodeAdminClient } from "@/protoFleet/api/clients";
import type {
  DevicePairingResult,
  FleetNodeDeviceSummary,
  FleetNodeDiscoveredDevice,
  FleetNodeEnrollmentStatus,
  FleetNodeSummary,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { Credentials, DiscoverRequest, DiscoverResponse } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { toError } from "@/protoFleet/api/requestErrors";
import { useAuthErrors } from "@/protoFleet/store";

export interface FleetNodeItem {
  fleetNodeId: string;
  pendingEnrollmentId: string | null;
  name: string;
  enrollmentStatus: FleetNodeEnrollmentStatus;
  identityFingerprint: string;
  commandProtocolUpgradeRequired: boolean;
  controlStreamConnected: boolean;
  pairedDeviceCount: number;
  createdAt: Date | null;
  lastSeenAt: Date | null;
}

export interface EnrollmentCode {
  code: string;
  pendingEnrollmentId: string;
  expiresAt: Date | null;
}

const toDate = (timestamp?: Timestamp): Date | null =>
  timestamp && timestamp.seconds > 0 ? new Date(Number(timestamp.seconds) * 1000) : null;

function toFleetNodeItem(summary: FleetNodeSummary): FleetNodeItem {
  return {
    fleetNodeId: summary.fleetNodeId.toString(),
    pendingEnrollmentId: summary.pendingEnrollmentId?.toString() ?? null,
    name: summary.name,
    enrollmentStatus: summary.enrollmentStatus,
    identityFingerprint: summary.identityFingerprint,
    commandProtocolUpgradeRequired: summary.commandProtocolUpgradeRequired,
    controlStreamConnected: summary.controlStreamConnected,
    pairedDeviceCount: Number(summary.pairedDeviceCount),
    createdAt: toDate(summary.createdAt),
    lastSeenAt: toDate(summary.lastSeenAt),
  };
}

const useFleetNodes = () => {
  const { handleAuthErrors } = useAuthErrors();

  const listFleetNodes = useCallback(async (): Promise<FleetNodeItem[]> => {
    try {
      const response = await fleetNodeAdminClient.listFleetNodes({});
      return response.fleetNodes.map(toFleetNodeItem);
    } catch (err) {
      handleAuthErrors({ error: err });
      throw toError(err, "Failed to load nodes.");
    }
  }, [handleAuthErrors]);

  // Plaintext code is one-time only.
  const createEnrollmentCode = useCallback(async (): Promise<EnrollmentCode> => {
    try {
      const response = await fleetNodeAdminClient.createEnrollmentCode({});
      return {
        code: response.code,
        pendingEnrollmentId: response.pendingEnrollmentId.toString(),
        expiresAt: toDate(response.expiresAt),
      };
    } catch (err) {
      handleAuthErrors({ error: err });
      throw toError(err, "Failed to create an enrollment code.");
    }
  }, [handleAuthErrors]);

  // Returns one-time api_key plaintext.
  const confirmFleetNode = useCallback(
    async (fleetNodeId: string, pendingEnrollmentId: string): Promise<string> => {
      try {
        const response = await fleetNodeAdminClient.confirmFleetNode({
          fleetNodeId: BigInt(fleetNodeId),
          pendingEnrollmentId: BigInt(pendingEnrollmentId),
        });
        return response.apiKey;
      } catch (err) {
        handleAuthErrors({ error: err });
        throw toError(err, "Failed to confirm the node.");
      }
    },
    [handleAuthErrors],
  );

  const revokeFleetNode = useCallback(
    async (fleetNodeId: string, pendingEnrollmentId?: string | null): Promise<void> => {
      try {
        await fleetNodeAdminClient.revokeFleetNode({
          fleetNodeId: BigInt(fleetNodeId),
          pendingEnrollmentId: pendingEnrollmentId ? BigInt(pendingEnrollmentId) : undefined,
        });
      } catch (err) {
        handleAuthErrors({ error: err });
        throw toError(err, "Failed to revoke the node.");
      }
    },
    [handleAuthErrors],
  );

  const listFleetNodeDevices = useCallback(
    async (fleetNodeId: string): Promise<FleetNodeDeviceSummary[]> => {
      try {
        const response = await fleetNodeAdminClient.listFleetNodeDevices({ fleetNodeId: BigInt(fleetNodeId) });
        return response.pairs;
      } catch (err) {
        handleAuthErrors({ error: err });
        throw toError(err, "Failed to load paired miners.");
      }
    },
    [handleAuthErrors],
  );

  const listFleetNodeDiscoveredDevices = useCallback(
    async (fleetNodeId: string, cursor = 0n): Promise<{ devices: FleetNodeDiscoveredDevice[]; nextCursor: bigint }> => {
      try {
        const response = await fleetNodeAdminClient.listFleetNodeDiscoveredDevices({
          fleetNodeId: BigInt(fleetNodeId),
          cursor,
          limit: 100,
        });
        return { devices: response.devices, nextCursor: response.nextCursor };
      } catch (err) {
        handleAuthErrors({ error: err });
        throw toError(err, "Failed to load discovered miners.");
      }
    },
    [handleAuthErrors],
  );

  const discoverOnFleetNode = useCallback(
    async (fleetNodeId: string, request: DiscoverRequest, onResponse: (response: DiscoverResponse) => void) => {
      try {
        for await (const update of fleetNodeAdminClient.discoverOnFleetNode({
          fleetNodeId: BigInt(fleetNodeId),
          request,
        })) {
          if (update.response) onResponse(update.response);
        }
      } catch (err) {
        handleAuthErrors({ error: err });
        throw toError(err, "Discovery failed.");
      }
    },
    [handleAuthErrors],
  );

  const pairDiscoveredDevicesOnFleetNode = useCallback(
    async (
      fleetNodeId: string,
      deviceIdentifiers: string[],
      credentials: Credentials | undefined,
      onResults: (results: DevicePairingResult[]) => void,
    ) => {
      try {
        for await (const update of fleetNodeAdminClient.pairDiscoveredDevicesOnFleetNode({
          fleetNodeId: BigInt(fleetNodeId),
          deviceIdentifiers,
          credentials,
        })) {
          if (update.results.length > 0) onResults(update.results);
        }
      } catch (err) {
        handleAuthErrors({ error: err });
        throw toError(err, "Pairing failed or its result is unknown. Refresh the miner lists before retrying.");
      }
    },
    [handleAuthErrors],
  );

  return {
    listFleetNodes,
    createEnrollmentCode,
    confirmFleetNode,
    revokeFleetNode,
    listFleetNodeDevices,
    listFleetNodeDiscoveredDevices,
    discoverOnFleetNode,
    pairDiscoveredDevicesOnFleetNode,
  };
};

export { useFleetNodes };
