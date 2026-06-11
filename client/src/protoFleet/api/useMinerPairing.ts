import { useCallback, useMemo, useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { fleetNodeAdminClient, pairingClient } from "@/protoFleet/api/clients";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { Device, DiscoverRequest, PairRequest } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isConnected } from "@/protoFleet/features/fleetNodes/utils/fleetNodeStatus";
import { useAuthErrors } from "@/protoFleet/store";

interface DiscoverMinersProps {
  discoverRequest: DiscoverRequest;
  discoverAbortController?: AbortController;
  onStreamData: (devices: Device[]) => void;
  onError?: (error: string) => void;
}

interface PairMinersProps {
  pairRequest: PairRequest;
  onSuccess: (failedDeviceIds: string[]) => void;
  onError?: (error: string) => void;
}

// When a fleet node is enrolled and connected, discovery and pairing route
// through it (the node scans/pairs on its local network); otherwise the
// cloud scans directly. Listing nodes is best-effort: callers without
// fleetnode:read, or any listing failure, fall back to the cloud path.
const connectedFleetNodeId = async (): Promise<bigint | null> => {
  try {
    const response = await fleetNodeAdminClient.listFleetNodes({});
    const node = response.fleetNodes.find(
      (n) => n.enrollmentStatus === FleetNodeEnrollmentStatus.CONFIRMED && isConnected(n.lastSeenAt?.seconds),
    );
    return node?.fleetNodeId ?? null;
  } catch {
    return null;
  }
};

// Translate the cloud PairRequest selector into the fleet-node RPC's shape.
// Returns null for selector kinds the node path can't express, so the caller
// stays on the cloud path.
const fleetNodePairArgs = (
  pairRequest: PairRequest,
): { deviceIdentifiers: string[]; pairAllUnpaired: boolean } | null => {
  const selection = pairRequest.deviceSelector?.selectionType;
  if (selection?.case === "includeDevices") {
    return { deviceIdentifiers: selection.value.deviceIdentifiers, pairAllUnpaired: false };
  }
  if (selection?.case === "allDevices") {
    return { deviceIdentifiers: [], pairAllUnpaired: true };
  }
  return null;
};

const useMinerPairing = () => {
  const { handleAuthErrors } = useAuthErrors();

  const [discoverPending, setDiscoverPending] = useState(false);
  const [pairingPending, setPairingPending] = useState(false);

  const cloudDiscover = useCallback(
    async ({ discoverRequest, discoverAbortController, onStreamData, onError }: DiscoverMinersProps) => {
      for await (const discoveryResponse of pairingClient.discover(discoverRequest, {
        signal: discoverAbortController?.signal,
      })) {
        if (discoveryResponse.error) {
          onError?.(discoveryResponse.error);
          break;
        }

        onStreamData(discoveryResponse.devices);
      }
    },
    [],
  );

  const discover = useCallback(
    async (props: DiscoverMinersProps) => {
      const { discoverRequest, discoverAbortController, onStreamData, onError } = props;
      setDiscoverPending(true);
      try {
        const fleetNodeId = await connectedFleetNodeId();
        if (fleetNodeId !== null) {
          let receivedAny = false;
          try {
            for await (const response of fleetNodeAdminClient.discoverOnFleetNode(
              { fleetNodeId, request: discoverRequest },
              { signal: discoverAbortController?.signal },
            )) {
              if (response.response?.error) {
                onError?.(response.response.error);
                receivedAny = true;
                break;
              }
              if (response.response?.devices?.length) {
                receivedAny = true;
                onStreamData(response.response.devices);
              }
            }
            return;
          } catch (nodeError) {
            // A node-path failure before any results (node raced offline,
            // request mode it can't run, ...) falls back to cloud discovery;
            // mid-stream failures surface to the caller.
            if (receivedAny || discoverAbortController?.signal.aborted) {
              throw nodeError;
            }
          }
        }
        await cloudDiscover(props);
      } catch (error) {
        if (
          (error instanceof DOMException && error.name === "AbortError") ||
          (discoverAbortController && discoverAbortController.signal.aborted)
        ) {
          // The discovery was aborted, do nothing
          return;
        } else if (error instanceof ConnectError) {
          handleAuthErrors({
            error: error,
            onError: () => {
              onError?.(getErrorMessage(error, "An unexpected error occurred"));
            },
          });
        } else if (typeof error === "string") {
          onError?.(error);
        } else {
          onError?.(getErrorMessage(error, "An unexpected error occurred"));
        }
      } finally {
        setDiscoverPending(false);
      }
    },
    [cloudDiscover, handleAuthErrors],
  );

  const fleetNodePair = useCallback(
    async (
      fleetNodeId: bigint,
      pairRequest: PairRequest,
      args: { deviceIdentifiers: string[]; pairAllUnpaired: boolean },
    ): Promise<string[]> => {
      // Every selected device starts as failed; PAIRED results clear them. The
      // server synthesizes ERROR results for unreported targets, so pair-all
      // failures also stream back explicitly.
      const failed = new Set(args.deviceIdentifiers);
      let receivedAny = false;
      try {
        for await (const response of fleetNodeAdminClient.pairDiscoveredDevicesOnFleetNode({
          fleetNodeId,
          deviceIdentifiers: args.deviceIdentifiers,
          pairAllUnpaired: args.pairAllUnpaired,
          credentials: pairRequest.credentials,
        })) {
          for (const result of response.results) {
            receivedAny = true;
            if (result.pairingStatus === PairingStatus.PAIRED) {
              failed.delete(result.deviceIdentifier);
            } else {
              failed.add(result.deviceIdentifier);
            }
          }
        }
      } catch (error) {
        // Pre-stream rejection ("no pairable devices for the requested
        // selection" — the devices weren't discovered by this node) is the
        // caller's signal to retry on the cloud path. After results have
        // streamed, surface the partial outcome instead of re-pairing.
        if (receivedAny) {
          return [...failed];
        }
        throw error;
      }
      return [...failed];
    },
    [],
  );

  const pair = useCallback(
    async ({ pairRequest, onSuccess, onError }: PairMinersProps) => {
      setPairingPending(true);
      try {
        const fleetNodeId = await connectedFleetNodeId();
        const nodeArgs = fleetNodeId !== null ? fleetNodePairArgs(pairRequest) : null;
        if (fleetNodeId !== null && nodeArgs !== null) {
          try {
            onSuccess(await fleetNodePair(fleetNodeId, pairRequest, nodeArgs));
            return;
          } catch (nodeError) {
            // "No pairable devices for the requested selection" (and similar
            // pre-stream rejections) means the devices weren't discovered by
            // this node — retry against the cloud path.
            if (!(nodeError instanceof ConnectError)) {
              throw nodeError;
            }
          }
        }
        const response = await pairingClient.pair(pairRequest);
        onSuccess(response.failedDeviceIds || []);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            onError?.(getErrorMessage(err));
          },
        });
      } finally {
        setPairingPending(false);
      }
    },
    [fleetNodePair, handleAuthErrors],
  );

  return useMemo(
    () => ({ discoverPending, discover, pairingPending, pair }),
    [discoverPending, discover, pairingPending, pair],
  );
};

export { useMinerPairing };
