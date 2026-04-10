import { useCallback, useMemo, useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { pairingClient } from "@/protoFleet/api/clients";
import { Device, DiscoverRequest, PairRequest } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
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

const useMinerPairing = () => {
  const { handleAuthErrors } = useAuthErrors();

  const [discoverPending, setDiscoverPending] = useState(false);
  const [pairingPending, setPairingPending] = useState(false);

  const discover = useCallback(
    async ({ discoverRequest, discoverAbortController, onStreamData, onError }: DiscoverMinersProps) => {
      setDiscoverPending(true);
      try {
        for await (const discoveryResponse of pairingClient.discover(discoverRequest, {
          signal: discoverAbortController?.signal,
        })) {
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
    [handleAuthErrors],
  );

  const pair = useCallback(
    async ({ pairRequest, onSuccess, onError }: PairMinersProps) => {
      setPairingPending(true);
      await pairingClient
        .pair(pairRequest)
        .then((response) => {
          onSuccess(response.failedDeviceIds || []);
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        })
        .finally(() => {
          setPairingPending(false);
        });
    },
    [handleAuthErrors],
  );

  return useMemo(
    () => ({ discoverPending, discover, pairingPending, pair }),
    [discoverPending, discover, pairingPending, pair],
  );
};

export { useMinerPairing };
