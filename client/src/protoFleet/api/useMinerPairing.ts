import { useCallback, useMemo, useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { pairingClient } from "@/protoFleet/api/clients";
import {
  Device,
  DiscoverRequest,
  PairRequest,
} from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { useAuthErrors, useAuthHeader } from "@/protoFleet/store";

interface DiscoverMinersProps {
  discoverRequest: DiscoverRequest;
  discoverAbortController?: AbortController;
  onStreamData: (devices: Device[]) => void;
  onError?: (error: string) => void;
}

interface PairMinersProps {
  pairRequest: PairRequest;
  onSuccess: () => void;
  onError?: (error: string) => void;
}

const useMinerPairing = () => {
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  const [discoverPending, setDiscoverPending] = useState(false);
  const [pairingPending, setPairingPending] = useState(false);

  const discover = useCallback(
    async ({
      discoverRequest,
      discoverAbortController,
      onStreamData,
      onError,
    }: DiscoverMinersProps) => {
      setDiscoverPending(true);
      try {
        for await (const discoveryResponse of pairingClient.discover(
          discoverRequest,
          {
            ...authHeader,
            signal: discoverAbortController?.signal,
          },
        )) {
          if (discoveryResponse.error) {
            onError?.(discoveryResponse.error);
            break;
          }

          onStreamData(discoveryResponse.devices);
        }
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
              onError?.(error.message);
            },
          });
        } else if (typeof error === "string") {
          onError?.(error);
        }
      } finally {
        setDiscoverPending(false);
      }
    },
    [authHeader, handleAuthErrors],
  );

  const pair = useCallback(
    async ({ pairRequest, onSuccess, onError }: PairMinersProps) => {
      setPairingPending(true);
      await pairingClient
        .pair(pairRequest, authHeader)
        .then(() => {
          onSuccess();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.message ?? String(err));
            },
          });
        })
        .finally(() => {
          setPairingPending(false);
        });
    },
    [authHeader, handleAuthErrors],
  );

  return useMemo(
    () => ({ discoverPending, discover, pairingPending, pair }),
    [discoverPending, discover, pairingPending, pair],
  );
};

export { useMinerPairing };
