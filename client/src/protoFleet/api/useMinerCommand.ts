import { useCallback, useMemo } from "react";
import { create } from "@bufbuild/protobuf";
import { ConnectError } from "@connectrpc/connect";
import { minerCommandClient } from "@/protoFleet/api/clients";
import {
  BlinkLEDRequest,
  BlinkLEDResponse,
  DeviceListSchema,
  DeviceSelectorSchema,
  StartMiningRequest,
  StartMiningResponse,
  StopMiningRequest,
  StopMiningResponse,
  StreamCommandBatchUpdatesRequest,
  StreamCommandBatchUpdatesResponse,
  UnpairRequest,
  UnpairResponse,
  UpdateMiningPoolsRequestSchema,
  UpdateMiningPoolsResponse,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { useAuthErrors, useAuthHeader } from "@/protoFleet/store";

interface BlinkLEDProps {
  blinkLEDRequest: BlinkLEDRequest;
  onSuccess: (value: BlinkLEDResponse) => void;
  onError?: (error: string) => void;
}

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

interface UnpairProps {
  unpairRequest: UnpairRequest;
  onSuccess: (value: UnpairResponse) => void;
  onError?: (error: string) => void;
}

interface StreamCommandBatchUpdatesProps {
  streamRequest: StreamCommandBatchUpdatesRequest;
  streamAbortController?: AbortController;
  onStreamData: (response: StreamCommandBatchUpdatesResponse) => void;
  onError?: (error: string) => void;
}

export interface PoolConfig {
  defaultPoolId?: string;
  backup1PoolId?: string;
  backup2PoolId?: string;
}

interface UpdateMiningPoolsProps {
  deviceIdentifiers: string[];
  poolConfig: PoolConfig;
  onSuccess: (value: UpdateMiningPoolsResponse) => void;
  onError?: (error: string) => void;
}

const useMinerCommand = () => {
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  const blinkLED = useCallback(
    async ({ blinkLEDRequest, onSuccess, onError }: BlinkLEDProps) => {
      await minerCommandClient
        .blinkLED(blinkLEDRequest, authHeader)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.message ?? String(err));
            },
          });
        });
    },
    [authHeader, handleAuthErrors],
  );

  const startMining = useCallback(
    async ({ startMiningRequest, onSuccess, onError }: StartMiningProps) => {
      await minerCommandClient
        .startMining(startMiningRequest, authHeader)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.message ?? String(err));
            },
          });
        });
    },
    [authHeader, handleAuthErrors],
  );

  const stopMining = useCallback(
    async ({ stopMiningRequest, onSuccess, onError }: StopMiningProps) => {
      await minerCommandClient
        .stopMining(stopMiningRequest, authHeader)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.message ?? String(err));
            },
          });
        });
    },
    [authHeader, handleAuthErrors],
  );

  const unpair = useCallback(
    async ({ unpairRequest, onSuccess, onError }: UnpairProps) => {
      await minerCommandClient
        .unpair(unpairRequest, authHeader)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.message ?? String(err));
            },
          });
        });
    },
    [authHeader, handleAuthErrors],
  );

  const streamCommandBatchUpdates = useCallback(
    async ({ streamRequest, streamAbortController, onStreamData, onError }: StreamCommandBatchUpdatesProps) => {
      try {
        for await (const updateResponse of minerCommandClient.streamCommandBatchUpdates(streamRequest, {
          ...authHeader,
          signal: streamAbortController?.signal,
        })) {
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
          handleAuthErrors({
            error,
            onError: () => {
              onError?.(error.message);
            },
          });
        } else if (typeof error === "string") {
          onError?.(error);
        }
      }
    },
    [authHeader, handleAuthErrors],
  );

  const updateMiningPools = useCallback(
    async ({ deviceIdentifiers, poolConfig, onSuccess, onError }: UpdateMiningPoolsProps) => {
      const updateMiningPoolsRequest = create(UpdateMiningPoolsRequestSchema, {
        deviceSelector: create(DeviceSelectorSchema, {
          selectionType: {
            case: "includeDevices",
            value: create(DeviceListSchema, {
              deviceIdentifiers,
            }),
          },
        }),
        defaultPoolId: poolConfig.defaultPoolId ? BigInt(poolConfig.defaultPoolId) : undefined,
        backup1PoolId: poolConfig.backup1PoolId ? BigInt(poolConfig.backup1PoolId) : undefined,
        backup2PoolId: poolConfig.backup2PoolId ? BigInt(poolConfig.backup2PoolId) : undefined,
      });

      await minerCommandClient
        .updateMiningPools(updateMiningPoolsRequest, authHeader)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.message ?? String(err));
            },
          });
        });
    },
    [authHeader, handleAuthErrors],
  );

  return useMemo(
    () => ({
      blinkLED,
      startMining,
      stopMining,
      unpair,
      streamCommandBatchUpdates,
      updateMiningPools,
    }),
    [blinkLED, startMining, stopMining, unpair, streamCommandBatchUpdates, updateMiningPools],
  );
};

export { useMinerCommand };
