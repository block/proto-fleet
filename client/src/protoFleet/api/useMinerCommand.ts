import { useCallback, useMemo } from "react";
import { minerCommandClient } from "@/protoFleet/api/clients";
import {
  StartMiningRequest,
  StopMiningRequest,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoFleet/features/auth/contexts/AuthContext";

interface StartMiningProps {
  startMiningRequest: StartMiningRequest;
  onSuccess: () => void;
  onError?: (error: string) => void;
}

interface StopMiningProps {
  stopMiningRequest: StopMiningRequest;
  onSuccess: () => void;
  onError?: (error: string) => void;
}

const useMinerCommand = () => {
  const { authTokens } = useAuthContext();

  const startMining = useCallback(
    async ({ startMiningRequest, onSuccess, onError }: StartMiningProps) => {
      await minerCommandClient
        .startMining(startMiningRequest, getAuthHeader(authTokens))
        .then(() => onSuccess())
        .catch((err) => {
          onError?.(err?.message ?? err);
        });
    },
    [authTokens],
  );

  const stopMining = useCallback(
    async ({ stopMiningRequest, onSuccess, onError }: StopMiningProps) => {
      await minerCommandClient
        .stopMining(stopMiningRequest, getAuthHeader(authTokens))
        .then(() => onSuccess())
        .catch((err) => {
          onError?.(err?.message ?? err);
        });
    },
    [authTokens],
  );

  return useMemo(
    () => ({ startMining, stopMining }),
    [startMining, stopMining],
  );
};

export { useMinerCommand };
