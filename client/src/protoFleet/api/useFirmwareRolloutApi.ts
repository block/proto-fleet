import { useCallback } from "react";
import { create } from "@bufbuild/protobuf";
import { firmwareRolloutClient } from "@/protoFleet/api/clients";
import type { DeviceSelector } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import {
  CreateFirmwareRolloutRequestSchema,
  type FirmwareRollout,
  type FirmwareRolloutEvent,
  type FirmwareRolloutTarget,
  type FirmwareRolloutTargetState,
} from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface CreateRolloutInput {
  name: string;
  firmwareFileId: string;
  deviceSelector: DeviceSelector;
  batchSize: number;
  batchIntervalSeconds: number;
}

async function withFriendlyError<T>(operation: Promise<T>, fallback: string): Promise<T> {
  try {
    return await operation;
  } catch (err) {
    throw Object.assign(new Error(getErrorMessage(err, fallback)), { cause: err });
  }
}

export function useFirmwareRolloutApi() {
  const { handleAuthErrors } = useAuthErrors();

  const handle = useCallback(
    async <T>(operation: Promise<T>, fallback: string): Promise<T> => {
      try {
        return await withFriendlyError(operation, fallback);
      } catch (err) {
        return await new Promise<T>((_, reject) => {
          handleAuthErrors({
            error: err,
            onError: (e) => reject(new Error(getErrorMessage(e, fallback))),
          });
        });
      }
    },
    [handleAuthErrors],
  );

  const createRollout = useCallback(
    async (input: CreateRolloutInput): Promise<FirmwareRollout> => {
      const request = create(CreateFirmwareRolloutRequestSchema, {
        name: input.name,
        firmwareFileId: input.firmwareFileId,
        deviceSelector: input.deviceSelector,
        batchSize: input.batchSize,
        batchIntervalSeconds: input.batchIntervalSeconds,
      });
      const response = await handle(firmwareRolloutClient.createFirmwareRollout(request), "Failed to create rollout");
      if (!response.rollout) throw new Error("Rollout response was empty");
      return response.rollout;
    },
    [handle],
  );

  const startRollout = useCallback(
    async (rolloutId: string): Promise<FirmwareRollout> => {
      const response = await handle(
        firmwareRolloutClient.startFirmwareRollout({ rolloutId }),
        "Failed to start rollout",
      );
      if (!response.rollout) throw new Error("Rollout response was empty");
      return response.rollout;
    },
    [handle],
  );

  const pauseRollout = useCallback(
    async (rolloutId: string): Promise<FirmwareRollout> => {
      const response = await handle(
        firmwareRolloutClient.pauseFirmwareRollout({ rolloutId }),
        "Failed to pause rollout",
      );
      if (!response.rollout) throw new Error("Rollout response was empty");
      return response.rollout;
    },
    [handle],
  );

  const resumeRollout = useCallback(
    async (rolloutId: string): Promise<FirmwareRollout> => {
      const response = await handle(
        firmwareRolloutClient.resumeFirmwareRollout({ rolloutId }),
        "Failed to resume rollout",
      );
      if (!response.rollout) throw new Error("Rollout response was empty");
      return response.rollout;
    },
    [handle],
  );

  const cancelRollout = useCallback(
    async (rolloutId: string): Promise<FirmwareRollout> => {
      const response = await handle(
        firmwareRolloutClient.cancelFirmwareRollout({ rolloutId }),
        "Failed to cancel rollout",
      );
      if (!response.rollout) throw new Error("Rollout response was empty");
      return response.rollout;
    },
    [handle],
  );

  const retryFailedTargets = useCallback(
    async (rolloutId: string): Promise<FirmwareRollout> => {
      const response = await handle(
        firmwareRolloutClient.retryFailedFirmwareRolloutTargets({ rolloutId }),
        "Failed to retry failed miners",
      );
      if (!response.rollout) throw new Error("Rollout response was empty");
      return response.rollout;
    },
    [handle],
  );

  const listRollouts = useCallback(
    async (pageToken = "", pageSize = 50): Promise<{ rollouts: FirmwareRollout[]; nextPageToken: string }> => {
      const response = await handle(
        firmwareRolloutClient.listFirmwareRollouts({ pageToken, pageSize }),
        "Failed to load firmware rollouts",
      );
      return { rollouts: response.rollouts, nextPageToken: response.nextPageToken };
    },
    [handle],
  );

  const getRollout = useCallback(
    async (rolloutId: string): Promise<FirmwareRollout> => {
      const response = await handle(
        firmwareRolloutClient.getFirmwareRollout({ rolloutId }),
        "Failed to load firmware rollout",
      );
      if (!response.rollout) throw new Error("Rollout response was empty");
      return response.rollout;
    },
    [handle],
  );

  const listTargets = useCallback(
    async ({
      rolloutId,
      pageToken = "",
      pageSize = 100,
      stateFilter,
    }: {
      rolloutId: string;
      pageToken?: string;
      pageSize?: number;
      stateFilter?: FirmwareRolloutTargetState;
    }): Promise<{ targets: FirmwareRolloutTarget[]; nextPageToken: string }> => {
      const response = await handle(
        firmwareRolloutClient.listFirmwareRolloutTargets({
          rolloutId,
          pageToken,
          pageSize,
          stateFilter,
        }),
        "Failed to load rollout miners",
      );
      return { targets: response.targets, nextPageToken: response.nextPageToken };
    },
    [handle],
  );

  const listEvents = useCallback(
    async (rolloutId: string): Promise<FirmwareRolloutEvent[]> => {
      const response = await handle(
        firmwareRolloutClient.listFirmwareRolloutEvents({ rolloutId }),
        "Failed to load rollout timeline",
      );
      return response.events;
    },
    [handle],
  );

  return {
    createRollout,
    startRollout,
    pauseRollout,
    resumeRollout,
    cancelRollout,
    retryFailedTargets,
    listRollouts,
    getRollout,
    listTargets,
    listEvents,
  };
}
