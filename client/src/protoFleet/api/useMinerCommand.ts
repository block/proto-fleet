import { useCallback, useMemo } from "react";
import { ConnectError } from "@connectrpc/connect";
import { minerCommandClient } from "@/protoFleet/api/clients";
import {
  StartMiningRequest,
  StartMiningResponse,
  StopMiningRequest,
  StopMiningResponse,
  StreamCommandBatchUpdatesRequest,
  StreamCommandBatchUpdatesResponse,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoFleet/features/auth/contexts/AuthContext";

interface StartMiningProps {
  startMiningRequest: StartMiningRequest;
  onSuccess: (value: StartMiningResponse) => void;
  onError?: (error: string) => void;
}

interface StopMiningProps {
  stopMiningRequest: StopMiningRequest;
  onSuccess: (value: StopMiningResponse) => void;
  onError?: (error: string) => void;
}

interface StreamCommandBatchUpdatesProps {
  streamRequest: StreamCommandBatchUpdatesRequest;
  streamAbortController?: AbortController;
  onStreamData: (response: StreamCommandBatchUpdatesResponse) => void;
  onError?: (error: string) => void;
}

const useMinerCommand = () => {
  const { authTokens } = useAuthContext();

  const startMining = useCallback(
    async ({ startMiningRequest, onSuccess, onError }: StartMiningProps) => {
      await minerCommandClient
        .startMining(startMiningRequest, getAuthHeader(authTokens))
        .then((response) => onSuccess(response))
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
        .then((response) => onSuccess(response))
        .catch((err) => {
          onError?.(err?.message ?? err);
        });
    },
    [authTokens],
  );

  const streamCommandBatchUpdates = useCallback(
    async ({
      streamRequest,
      streamAbortController,
      onStreamData,
      onError,
    }: StreamCommandBatchUpdatesProps) => {
      try {
        for await (const updateResponse of minerCommandClient.streamCommandBatchUpdates(
          streamRequest,
          {
            ...getAuthHeader(authTokens),
            signal: streamAbortController?.signal,
          },
        )) {
          onStreamData(updateResponse);
        }
      } catch (error) {
        if (
          (error instanceof DOMException && error.name === "AbortError") ||
          (streamAbortController && streamAbortController.signal.aborted)
        ) {
          // The stream was aborted, do nothing
          return;
        } else if (error instanceof ConnectError) {
          onError?.(error.message);
        } else if (typeof error === "string") {
          onError?.(error);
        }
      }
    },
    [authTokens],
  );

  return useMemo(
    () => ({
      startMining,
      stopMining,
      streamCommandBatchUpdates,
    }),
    [startMining, stopMining, streamCommandBatchUpdates],
  );
};

export { useMinerCommand };
