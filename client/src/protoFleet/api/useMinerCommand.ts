import { useCallback, useMemo } from "react";
import { create } from "@bufbuild/protobuf";
import { ConnectError } from "@connectrpc/connect";
import { fleetManagementClient, minerCommandClient } from "@/protoFleet/api/clients";
import { CoolingMode } from "@/protoFleet/api/generated/common/v1/cooling_pb";
import {
  type DeleteMinersRequest,
  type DeleteMinersResponse,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  BlinkLEDRequest,
  BlinkLEDResponse,
  CheckCommandCapabilitiesRequestSchema,
  CheckCommandCapabilitiesResponse,
  CommandType,
  DeviceSelector,
  DownloadLogsRequest,
  DownloadLogsResponse,
  FirmwareUpdateRequest,
  FirmwareUpdateResponse,
  GetCommandBatchLogBundleRequest,
  GetCommandBatchLogBundleResponse,
  PerformanceMode,
  type PoolSlotConfig,
  PoolSlotConfigSchema,
  RawPoolInfoSchema,
  RebootRequest,
  RebootResponse,
  SetCoolingModeRequestSchema,
  SetCoolingModeResponse,
  SetPowerTargetRequestSchema,
  SetPowerTargetResponse,
  StartMiningRequest,
  StartMiningResponse,
  StopMiningRequest,
  StopMiningResponse,
  StreamCommandBatchUpdatesRequest,
  StreamCommandBatchUpdatesResponse,
  UpdateMinerPasswordRequestSchema,
  UpdateMinerPasswordResponse,
  UpdateMiningPoolsRequestSchema,
  UpdateMiningPoolsResponse,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

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

interface DeleteMinersProps {
  deleteMinersRequest: DeleteMinersRequest;
  onSuccess: (value: DeleteMinersResponse) => void;
  onError?: (error: string) => void;
}

interface RebootProps {
  rebootRequest: RebootRequest;
  onSuccess: (value: RebootResponse) => void;
  onError?: (error: string) => void;
}

interface StreamCommandBatchUpdatesProps {
  streamRequest: StreamCommandBatchUpdatesRequest;
  streamAbortController?: AbortController;
  onStreamData: (response: StreamCommandBatchUpdatesResponse) => void;
  onError?: (error: string) => void;
}

// Configuration for a single pool slot - either a known pool ID or raw pool info
export type PoolSlotSource = { type: "poolId"; poolId: string } | { type: "rawPool"; url: string; username: string };

export interface PoolConfig {
  defaultPool: PoolSlotSource;
  backup1Pool?: PoolSlotSource;
  backup2Pool?: PoolSlotSource;
}

interface UpdateMiningPoolsProps {
  deviceSelector: DeviceSelector;
  poolConfig: PoolConfig;
  userUsername: string;
  userPassword: string;
  onSuccess: (value: UpdateMiningPoolsResponse) => void;
  onError?: (error: string) => void;
}

interface SetPowerTargetProps {
  deviceSelector: DeviceSelector;
  performanceMode: PerformanceMode;
  onSuccess: (value: SetPowerTargetResponse) => void;
  onError?: (error: string) => void;
}

interface SetCoolingModeProps {
  deviceSelector: DeviceSelector;
  coolingMode: CoolingMode;
  onSuccess: (value: SetCoolingModeResponse) => void;
  onError?: (error: string) => void;
}

interface CheckCommandCapabilitiesProps {
  deviceSelector: DeviceSelector;
  commandType: CommandType;
  onSuccess: (value: CheckCommandCapabilitiesResponse) => void;
  onError?: (error: string) => void;
}

interface UpdateMinerPasswordProps {
  deviceSelector: DeviceSelector;
  newPassword: string;
  currentPassword: string;
  userUsername: string;
  userPassword: string;
  onSuccess: (value: UpdateMinerPasswordResponse) => void;
  onError?: (error: string) => void;
}

interface DownloadLogsProps {
  downloadLogsRequest: DownloadLogsRequest;
  onSuccess: (value: DownloadLogsResponse) => void;
  onError?: (error: string) => void;
}

interface FirmwareUpdateProps {
  firmwareUpdateRequest: FirmwareUpdateRequest;
  onSuccess: (value: FirmwareUpdateResponse) => void;
  onError?: (error: string) => void;
}

interface GetCommandBatchLogBundleProps {
  request: GetCommandBatchLogBundleRequest;
  onSuccess: (value: GetCommandBatchLogBundleResponse) => void;
  onError?: (error: string) => void;
}

const useMinerCommand = () => {
  const { handleAuthErrors } = useAuthErrors();

  const blinkLED = useCallback(
    async ({ blinkLEDRequest, onSuccess, onError }: BlinkLEDProps) => {
      await minerCommandClient
        .blinkLED(blinkLEDRequest)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const startMining = useCallback(
    async ({ startMiningRequest, onSuccess, onError }: StartMiningProps) => {
      await minerCommandClient
        .startMining(startMiningRequest)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const stopMining = useCallback(
    async ({ stopMiningRequest, onSuccess, onError }: StopMiningProps) => {
      await minerCommandClient
        .stopMining(stopMiningRequest)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const deleteMiners = useCallback(
    async ({ deleteMinersRequest, onSuccess, onError }: DeleteMinersProps) => {
      await fleetManagementClient
        .deleteMiners(deleteMinersRequest)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const reboot = useCallback(
    async ({ rebootRequest, onSuccess, onError }: RebootProps) => {
      await minerCommandClient
        .reboot(rebootRequest)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const streamCommandBatchUpdates = useCallback(
    async ({ streamRequest, streamAbortController, onStreamData, onError }: StreamCommandBatchUpdatesProps) => {
      try {
        for await (const updateResponse of minerCommandClient.streamCommandBatchUpdates(streamRequest, {
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
              onError?.(getErrorMessage(error, "An unexpected error occurred"));
            },
          });
        } else if (typeof error === "string") {
          onError?.(error);
        } else {
          onError?.(getErrorMessage(error, "An unexpected error occurred"));
        }
      }
    },
    [handleAuthErrors],
  );

  const updateMiningPools = useCallback(
    async ({ deviceSelector, poolConfig, userUsername, userPassword, onSuccess, onError }: UpdateMiningPoolsProps) => {
      const createPoolSlotConfig = (source: PoolSlotSource): PoolSlotConfig => {
        if (source.type === "poolId") {
          return create(PoolSlotConfigSchema, {
            poolSource: { case: "poolId", value: BigInt(source.poolId) },
          });
        }
        return create(PoolSlotConfigSchema, {
          poolSource: {
            case: "rawPool",
            value: create(RawPoolInfoSchema, {
              url: source.url,
              username: source.username,
            }),
          },
        });
      };

      const updateMiningPoolsRequest = create(UpdateMiningPoolsRequestSchema, {
        deviceSelector,
        defaultPool: createPoolSlotConfig(poolConfig.defaultPool),
        backup1Pool: poolConfig.backup1Pool ? createPoolSlotConfig(poolConfig.backup1Pool) : undefined,
        backup2Pool: poolConfig.backup2Pool ? createPoolSlotConfig(poolConfig.backup2Pool) : undefined,
        userUsername,
        userPassword,
      });

      await minerCommandClient
        .updateMiningPools(updateMiningPoolsRequest)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const setPowerTarget = useCallback(
    async ({ deviceSelector, performanceMode, onSuccess, onError }: SetPowerTargetProps) => {
      const setPowerTargetRequest = create(SetPowerTargetRequestSchema, {
        deviceSelector,
        performanceMode,
      });

      await minerCommandClient
        .setPowerTarget(setPowerTargetRequest)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const setCoolingMode = useCallback(
    async ({ deviceSelector, coolingMode, onSuccess, onError }: SetCoolingModeProps) => {
      const setCoolingModeRequest = create(SetCoolingModeRequestSchema, {
        deviceSelector,
        mode: coolingMode,
      });

      await minerCommandClient
        .setCoolingMode(setCoolingModeRequest)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const checkCommandCapabilities = useCallback(
    async ({ deviceSelector, commandType, onSuccess, onError }: CheckCommandCapabilitiesProps) => {
      const request = create(CheckCommandCapabilitiesRequestSchema, {
        deviceSelector,
        commandType,
      });

      await minerCommandClient
        .checkCommandCapabilities(request)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const updateMinerPassword = useCallback(
    async ({
      deviceSelector,
      newPassword,
      currentPassword,
      userUsername,
      userPassword,
      onSuccess,
      onError,
    }: UpdateMinerPasswordProps) => {
      const request = create(UpdateMinerPasswordRequestSchema, {
        deviceSelector,
        newPassword,
        currentPassword,
        userUsername,
        userPassword,
      });

      await minerCommandClient
        .updateMinerPassword(request)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const downloadLogs = useCallback(
    async ({ downloadLogsRequest, onSuccess, onError }: DownloadLogsProps) => {
      await minerCommandClient
        .downloadLogs(downloadLogsRequest)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const firmwareUpdate = useCallback(
    async ({ firmwareUpdateRequest, onSuccess, onError }: FirmwareUpdateProps) => {
      await minerCommandClient
        .firmwareUpdate(firmwareUpdateRequest)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const getCommandBatchLogBundle = useCallback(
    async ({ request, onSuccess, onError }: GetCommandBatchLogBundleProps) => {
      await minerCommandClient
        .getCommandBatchLogBundle(request)
        .then((response) => onSuccess(response))
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  return useMemo(
    () => ({
      blinkLED,
      startMining,
      stopMining,
      deleteMiners,
      reboot,
      streamCommandBatchUpdates,
      updateMiningPools,
      setPowerTarget,
      setCoolingMode,
      checkCommandCapabilities,
      updateMinerPassword,
      downloadLogs,
      firmwareUpdate,
      getCommandBatchLogBundle,
    }),
    [
      blinkLED,
      startMining,
      stopMining,
      deleteMiners,
      reboot,
      streamCommandBatchUpdates,
      updateMiningPools,
      setPowerTarget,
      setCoolingMode,
      checkCommandCapabilities,
      updateMinerPassword,
      downloadLogs,
      firmwareUpdate,
      getCommandBatchLogBundle,
    ],
  );
};

export { useMinerCommand };
