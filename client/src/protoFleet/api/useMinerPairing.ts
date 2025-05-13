import { useCallback, useMemo, useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { pairingClient } from "@/protoFleet/api/clients";
import {
  Device,
  DiscoverRequest,
  PairRequest,
} from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoFleet/features/auth/contexts/AuthContext";

interface DiscoverMinersProps {
  discoverRequest: DiscoverRequest;
  onStreamData: (devices: Device[]) => void;
  onError?: (error: string) => void;
}

interface PairMinersProps {
  pairRequest: PairRequest;
  onSuccess: () => void;
  onError?: (error: string) => void;
}

const useMinerPairing = () => {
  const { authTokens } = useAuthContext();

  const [discoverPending, setDiscoverPending] = useState(false);
  const [pairingPending, setPairingPending] = useState(false);

  const discover = useCallback(
    async ({ discoverRequest, onStreamData, onError }: DiscoverMinersProps) => {
      setDiscoverPending(true);
      try {
        for await (const discoveryResponse of pairingClient.discover(
          discoverRequest,
          getAuthHeader(authTokens),
        )) {
          if (discoveryResponse.error) {
            onError?.(discoveryResponse.error);
            break;
          }

          onStreamData(discoveryResponse.devices);
        }
      } catch (error) {
        if (error instanceof ConnectError) {
          onError?.(error.message);
        } else if (typeof error === "string") {
          onError?.(error);
        }
      } finally {
        setDiscoverPending(false);
      }
    },
    [authTokens],
  );

  const pair = useCallback(
    async ({ pairRequest, onSuccess, onError }: PairMinersProps) => {
      setPairingPending(true);
      await pairingClient
        .pair(pairRequest, getAuthHeader(authTokens))
        .then(() => {
          onSuccess();
        })
        .catch((err) => {
          onError?.(err?.error?.message ?? err);
        })
        .finally(() => {
          setPairingPending(false);
        });
    },
    [authTokens],
  );

  return useMemo(
    () => ({ discoverPending, discover, pairingPending, pair }),
    [discoverPending, discover, pairingPending, pair],
  );
};

export { useMinerPairing };
