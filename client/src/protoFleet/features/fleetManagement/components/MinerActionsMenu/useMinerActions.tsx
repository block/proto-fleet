import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import {
  deviceActions,
  getFailureMessage,
  getLoadingMessage,
  getSuccessMessage,
  groupActions,
  loadingMessages,
  minersMessage,
  performanceActions,
  settingsActions,
  successMessages,
  SupportedAction,
} from "./constants";
import { useFleetAuthentication } from "./useFleetAuthentication";
import { useManageSecurityFlow } from "./useManageSecurityFlow";
import { CoolingMode } from "@/protoFleet/api/generated/common/v1/cooling_pb";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import {
  DeleteMinersRequestSchema,
  type DeleteMinersResponse,
  DeviceSelectorSchema,
  type MinerListFilter,
  MinerListFilterSchema,
  type MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  BlinkLEDRequestSchema,
  BlinkLEDResponse,
  CommandBatchUpdateStatus_CommandBatchUpdateStatusType,
  CommandType,
  DeviceSelector,
  DownloadLogsRequestSchema,
  FirmwareUpdateRequestSchema,
  FirmwareUpdateResponse,
  GetCommandBatchLogBundleRequestSchema,
  PerformanceMode,
  RebootRequestSchema,
  RebootResponse,
  SetCoolingModeResponse,
  SetPowerTargetResponse,
  StartMiningRequestSchema,
  StartMiningResponse,
  StopMiningRequestSchema,
  StopMiningResponse,
  StreamCommandBatchUpdatesRequestSchema,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import useMinerCoolingMode from "@/protoFleet/api/useMinerCoolingMode";
import useMinerModelGroups from "@/protoFleet/api/useMinerModelGroups";
import useRenameMiners from "@/protoFleet/api/useRenameMiners";
import {
  BulkAction,
  type UnsupportedMinersInfo,
} from "@/protoFleet/features/fleetManagement/components/BulkActions/types";
import type { BatchOperationInput } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";
import { createDeviceSelector } from "@/protoFleet/features/fleetManagement/utils/deviceSelector";
import {
  Fan,
  Groups,
  LEDIndicator,
  Lock,
  MiningPools,
  Play,
  Power,
  Reboot,
  Settings,
  Speedometer,
  Terminal,
  Unpair,
} from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import { type SelectionMode } from "@/shared/components/List";
import { pushToast, removeToast, STATUSES as TOAST_STATUSES, updateToast } from "@/shared/features/toaster";
import { downloadBlob } from "@/shared/utils/utility";

export interface MinerSelection {
  deviceIdentifier: string;
  deviceStatus?: DeviceStatus;
}

interface UseMinerActionsParams {
  selectedMiners: MinerSelection[];
  selectionMode: SelectionMode;
  /** Total count of all miners in fleet (used for "all" mode confirmation dialogs) */
  totalCount?: number;
  /** Active UI filter — forwarded as device_filter when unpairing in "all" mode */
  currentFilter?: MinerListFilter;
  onActionStart?: () => void;
  onActionComplete?: () => void;
  /** Start tracking a batch operation */
  startBatchOperation?: (batch: BatchOperationInput) => void;
  /** Complete a batch operation */
  completeBatchOperation?: (batchIdentifier: string) => void;
  /** Remove devices from a batch */
  removeDevicesFromBatch?: (batchIdentifier: string, deviceIds: string[]) => void;
  /** The miners map — used for firmware model checks, unpair subtitle, and security grouping */
  miners?: Record<string, MinerStateSnapshot>;
  /** Replaces store-based refetchMiners — called after unpair completes */
  onRefetchMiners?: () => void;
}

/**
 * Metadata for actions that require capability checking.
 * Contains both the description for the unsupported miners modal and the proto CommandType.
 * Actions not in this map don't require capability checking (e.g., unpair).
 */
const actionCapabilityMetadata: Partial<Record<SupportedAction, { description: string; commandType: CommandType }>> = {
  [deviceActions.shutdown]: { description: "Sleep mode changes", commandType: CommandType.STOP_MINING },
  [deviceActions.wakeUp]: { description: "Wake-up", commandType: CommandType.START_MINING },
  [deviceActions.reboot]: { description: "Reboot", commandType: CommandType.REBOOT },
  [deviceActions.blinkLEDs]: { description: "LED blinking", commandType: CommandType.BLINK_LED },
  [deviceActions.factoryReset]: { description: "Factory reset", commandType: CommandType.UNSPECIFIED },
  [deviceActions.downloadLogs]: { description: "Log downloads", commandType: CommandType.DOWNLOAD_LOGS },
  [settingsActions.miningPool]: { description: "Pool switching", commandType: CommandType.UPDATE_MINING_POOLS },
  [settingsActions.updateWorkerNames]: {
    description: "Worker name updates",
    commandType: CommandType.UPDATE_MINING_POOLS,
  },
  [settingsActions.coolingMode]: { description: "Cooling mode changes", commandType: CommandType.SET_COOLING_MODE },
  [settingsActions.security]: { description: "Password updates", commandType: CommandType.UPDATE_MINER_PASSWORD },
  [performanceActions.managePower]: { description: "Power mode changes", commandType: CommandType.SET_POWER_TARGET },
  [deviceActions.firmwareUpdate]: { description: "Firmware updates", commandType: CommandType.FIRMWARE_UPDATE },
};

function getUniqueModels(
  deviceIds: string[],
  miners: Record<string, MinerStateSnapshot>,
): { models: Set<string>; hasMissing: boolean } {
  const models = new Set<string>();
  let hasMissing = false;
  for (const id of deviceIds) {
    const miner = miners[id];
    const model = miner?.model;
    if (model) models.add(model);
    else hasMissing = true;
  }
  return { models, hasMissing };
}

/**
 * Callback for pending actions that may receive a filtered device selector.
 * When called after the unsupported miners modal, receives the filtered selector
 * containing only supported miners.
 */
type PendingActionCallback = (filteredSelector?: DeviceSelector, filteredDeviceIdentifiers?: string[]) => void;

/**
 * Internal state for unsupported miners modal, extends UnsupportedMinersInfo with pendingAction.
 */
interface UnsupportedMinersState extends UnsupportedMinersInfo {
  pendingAction: PendingActionCallback | null;
}

const initialUnsupportedMinersState: UnsupportedMinersState = {
  visible: false,
  unsupportedGroups: [],
  totalUnsupportedCount: 0,
  noneSupported: false,
  pendingAction: null,
  supportedDeviceIdentifiers: [],
};

const protoDriverName = "proto";

/**
 * Determines if a Proto rig is reachable for ClearAuthKey.
 * A device is reachable if it's not offline and has completed authentication (PAIRED).
 */
const isProtoReachable = (deviceStatus: DeviceStatus, pairingStatus: PairingStatus): boolean =>
  deviceStatus !== DeviceStatus.OFFLINE && pairingStatus === PairingStatus.PAIRED;

/**
 * Builds a contextual confirmation subtitle for the unpair action based on the
 * miner types and statuses in the selection (per RFC Option C).
 *
 * @param miners - the fleet miners record, passed explicitly for testability
 */
const hasActiveFilter = (filter?: MinerListFilter): boolean =>
  filter !== undefined &&
  (filter.deviceStatus.length > 0 || filter.errorComponentTypes.length > 0 || filter.models.length > 0);

const buildUnpairConfirmationSubtitle = (
  selectedMiners: MinerSelection[],
  selectionMode: SelectionMode,
  displayCount: number,
  miners: Record<string, { driverName: string; deviceStatus: number; pairingStatus: number }>,
  currentFilter?: MinerListFilter,
): string => {
  // In "all" mode we may not have full miner data loaded — use a generic message
  if (selectionMode === "all") {
    if (hasActiveFilter(currentFilter)) {
      return `${displayCount} matching ${displayCount === 1 ? "miner" : "miners"} will be removed from your fleet. You can re-discover and pair them again later.`;
    }
    return `All ${displayCount} miners will be removed from your fleet. You can re-discover and pair them again later.`;
  }

  let protoReachableCount = 0;
  let protoUnreachableCount = 0;
  let thirdPartyCount = 0;

  for (const { deviceIdentifier } of selectedMiners) {
    const miner = miners[deviceIdentifier];
    if (!miner) {
      thirdPartyCount++;
      continue;
    }

    if (miner.driverName === protoDriverName) {
      if (isProtoReachable(miner.deviceStatus as DeviceStatus, miner.pairingStatus as PairingStatus)) {
        protoReachableCount++;
      } else {
        protoUnreachableCount++;
      }
    } else {
      thirdPartyCount++;
    }
  }

  const isSingle = displayCount === 1;

  // Single miner
  if (isSingle) {
    if (protoReachableCount === 1) {
      return "This miner will be removed from your fleet and its auth key will be cleared.";
    }
    if (protoUnreachableCount === 1) {
      return "This miner will be removed from your fleet. It may need to be factory reset before re-pairing.";
    }
    return "This miner will be removed from your fleet and will stop sending telemetry data.";
  }

  // All same category
  if (thirdPartyCount === 0 && protoUnreachableCount === 0) {
    return "These miners will be removed from your fleet and their auth keys will be cleared.";
  }
  if (thirdPartyCount === 0 && protoReachableCount === 0) {
    return "These miners will be removed from your fleet. They may need to be factory reset before re-pairing.";
  }
  if (protoReachableCount === 0 && protoUnreachableCount === 0) {
    return "These miners will be removed from your fleet and will stop sending telemetry data.";
  }

  // Mixed — summarize with unreachable Proto warning
  const parts: string[] = [];
  parts.push(`${displayCount} miners will be removed from your fleet.`);
  if (protoUnreachableCount > 0) {
    parts.push(
      `${protoUnreachableCount} Proto ${protoUnreachableCount === 1 ? "miner is" : "miners are"} unreachable and may need factory reset to re-pair.`,
    );
  }
  return parts.join(" ");
};

const noop = () => {};

export const useMinerActions = ({
  selectedMiners,
  selectionMode,
  totalCount,
  currentFilter,
  onActionStart,
  onActionComplete,
  startBatchOperation = noop as (batch: BatchOperationInput) => void,
  completeBatchOperation = noop as (batchIdentifier: string) => void,
  removeDevicesFromBatch = noop as (batchIdentifier: string, deviceIds: string[]) => void,
  miners = {} as Record<string, MinerStateSnapshot>,
  onRefetchMiners,
}: UseMinerActionsParams) => {
  const {
    startMining,
    stopMining,
    blinkLED,
    deleteMiners,
    reboot,
    streamCommandBatchUpdates,
    setPowerTarget,
    setCoolingMode,
    checkCommandCapabilities,
    updateMinerPassword,
    downloadLogs,
    firmwareUpdate,
    getCommandBatchLogBundle,
  } = useMinerCommand();

  const { fetchCoolingMode } = useMinerCoolingMode();
  const { getMinerModelGroups } = useMinerModelGroups();
  const { renameSingleMiner } = useRenameMiners();

  const [currentAction, setCurrentAction] = useState<SupportedAction | null>(null);
  const [showRenameDialog, setShowRenameDialog] = useState(false);
  const [showManagePowerModal, setShowManagePowerModal] = useState(false);
  const [filteredSelectorForPowerModal, setFilteredSelectorForPowerModal] = useState<DeviceSelector | undefined>();
  const [managePowerFilteredDeviceIds, setManagePowerFilteredDeviceIds] = useState<string[] | undefined>(undefined);
  const [showCoolingModeModal, setShowCoolingModeModal] = useState(false);
  const [coolingModeFilteredSelector, setCoolingModeFilteredSelector] = useState<DeviceSelector | undefined>(undefined);
  const [coolingModeFilteredDeviceIds, setCoolingModeFilteredDeviceIds] = useState<string[] | undefined>(undefined);
  const [currentCoolingMode, setCurrentCoolingMode] = useState<CoolingMode | undefined>(undefined);
  const [showAddToGroupModal, setShowAddToGroupModal] = useState(false);
  const [showFirmwareUpdateModal, setShowFirmwareUpdateModal] = useState(false);
  const [firmwareUpdateFilteredSelector, setFirmwareUpdateFilteredSelector] = useState<DeviceSelector | undefined>();
  const [firmwareUpdateFilteredDeviceIds, setFirmwareUpdateFilteredDeviceIds] = useState<string[] | undefined>(
    undefined,
  );
  const [showPoolSelectionPage, setShowPoolSelectionPage] = useState(false);
  const [poolFilteredDeviceIds, setPoolFilteredDeviceIds] = useState<string[] | undefined>(undefined);
  const [unsupportedMinersInfo, setUnsupportedMinersInfo] =
    useState<UnsupportedMinersState>(initialUnsupportedMinersState);

  const numberOfMiners = useMemo(() => selectedMiners.length, [selectedMiners]);

  // Display count for confirmation dialogs - use totalCount when in "all" mode
  const displayCount = useMemo(
    () => (selectionMode === "all" && totalCount !== undefined ? totalCount : numberOfMiners),
    [selectionMode, totalCount, numberOfMiners],
  );

  // Extract device identifiers for API calls
  const deviceIdentifiers = useMemo(() => selectedMiners.map((m) => m.deviceIdentifier), [selectedMiners]);

  // Contextual subtitle for unpair confirmation dialog (per RFC Option C)
  const unpairConfirmationSubtitle = useMemo(
    () => buildUnpairConfirmationSubtitle(selectedMiners, selectionMode, displayCount, miners, currentFilter),
    [selectedMiners, selectionMode, displayCount, miners, currentFilter],
  );

  // Create device selector based on selection mode (undefined when nothing selected)
  const deviceSelector = useMemo(
    () => (selectionMode === "none" ? undefined : createDeviceSelector(selectionMode, deviceIdentifiers)),
    [selectionMode, deviceIdentifiers],
  );

  // Determine device status for power state actions
  const deviceStatus = useMemo(() => {
    if (selectedMiners.length === 0) return undefined;

    const firstStatus = selectedMiners[0]?.deviceStatus;
    const allHaveSameStatus = selectedMiners.every((m) => m.deviceStatus === firstStatus);

    return allHaveSameStatus ? firstStatus : undefined;
  }, [selectedMiners]);

  // Check for unsupported miners using server-side capability checking.
  // Returns a promise that resolves to true if the modal was shown.
  const checkAndShowUnsupportedMinersModal = useCallback(
    async (action: SupportedAction, proceedAction: PendingActionCallback): Promise<boolean> => {
      const metadata = actionCapabilityMetadata[action];

      if (!metadata || metadata.commandType === CommandType.UNSPECIFIED || !deviceSelector) {
        return false;
      }

      return new Promise((resolve) => {
        checkCommandCapabilities({
          deviceSelector,
          commandType: metadata.commandType,
          onSuccess: (result) => {
            if (result.allSupported) {
              resolve(false);
              return;
            }

            setUnsupportedMinersInfo({
              visible: true,
              unsupportedGroups: result.unsupportedGroups,
              totalUnsupportedCount: result.unsupportedCount,
              noneSupported: result.noneSupported,
              pendingAction: result.noneSupported ? null : proceedAction,
              supportedDeviceIdentifiers: result.supportedDeviceIdentifiers,
            });

            resolve(true);
          },
          onError: () => {
            // On error, proceed without showing modal (fail-open for capability check)
            resolve(false);
          },
        });
      });
    },
    [deviceSelector, checkCommandCapabilities],
  );

  // Wraps checkAndShowUnsupportedMinersModal with the common proceed pattern:
  // onProceed is called with filtered values when the unsupported miners modal
  // was shown and the user clicked Continue, or with undefined values when all
  // miners support the action (so callers can use `filteredDeviceIds ?? deviceIdentifiers`).
  const withCapabilityCheck = useCallback(
    async (
      action: SupportedAction,
      onProceed: (filteredSelector?: DeviceSelector, filteredDeviceIds?: string[]) => void,
    ): Promise<void> => {
      const modalShown = await checkAndShowUnsupportedMinersModal(action, onProceed);
      if (!modalShown) {
        onProceed(undefined, undefined);
      }
    },
    [checkAndShowUnsupportedMinersModal],
  );

  // Handle continuing from unsupported miners modal
  // Creates a filtered device selector with only supported miners
  const handleUnsupportedMinersContinue = useCallback(() => {
    const { pendingAction, supportedDeviceIdentifiers } = unsupportedMinersInfo;
    const filteredSelector =
      supportedDeviceIdentifiers.length > 0 ? createDeviceSelector("subset", supportedDeviceIdentifiers) : undefined;
    setUnsupportedMinersInfo(initialUnsupportedMinersState);
    pendingAction?.(filteredSelector, supportedDeviceIdentifiers);
  }, [unsupportedMinersInfo]);

  // Handle dismissing unsupported miners modal
  const handleUnsupportedMinersDismiss = useCallback(() => {
    setUnsupportedMinersInfo(initialUnsupportedMinersState);
    setCurrentAction(null);
    onActionComplete?.();
  }, [onActionComplete]);

  const handleSuccess = useCallback(
    (
      action: SupportedAction,
      originalToastId: number,
      batchIdentifier: string,
      onBatchComplete?: (successDeviceIds: string[], failureDeviceIds: string[]) => void,
      retryAction?: (failedDeviceIds: string[]) => void,
    ) => {
      const streamAbortController = new AbortController();

      let errorToastId: number | null = null;
      let successCount = 0;
      let totalCount = 0;
      let successDeviceIds: string[] = [];
      let failureDeviceIds: string[] = [];
      // Only true when we've received results for every expected device. Guards
      // the Retry action below so a premature stream termination (network/auth
      // failure, unmount) cannot offer a retry against a still-in-flight batch.
      let streamCompletedNormally = false;

      streamCommandBatchUpdates({
        streamRequest: create(StreamCommandBatchUpdatesRequestSchema, {
          batchIdentifier,
        }),
        onStreamData: (response) => {
          totalCount = Number(response.status?.commandBatchDeviceCount?.total || 0);
          successCount = Number(response.status?.commandBatchDeviceCount?.success || 0);
          const failureCount = Number(response.status?.commandBatchDeviceCount?.failure || 0);

          successDeviceIds = response.status?.commandBatchDeviceCount?.successDeviceIdentifiers || [];
          failureDeviceIds = response.status?.commandBatchDeviceCount?.failureDeviceIdentifiers || [];

          if (successCount > 0) {
            updateToast(originalToastId, {
              message: getSuccessMessage(action, `${successCount} out of ${totalCount} ${minersMessage}`),
              status: TOAST_STATUSES.success,
            });
          }

          if (failureCount > 0) {
            const failureMsg = getFailureMessage(action, `${failureCount} out of ${totalCount} ${minersMessage}`);
            if (!errorToastId) {
              errorToastId = pushToast({
                message: failureMsg,
                status: TOAST_STATUSES.error,
                longRunning: true,
              });
            } else {
              updateToast(errorToastId, {
                message: failureMsg,
                status: TOAST_STATUSES.error,
              });
            }
          }

          // Close the stream when we've received results for all devices
          // This triggers .finally() to clear loading states immediately
          if (successCount + failureCount === totalCount && totalCount > 0) {
            streamCompletedNormally = true;
            streamAbortController.abort();
          }
        },
        streamAbortController: streamAbortController,
      }).finally(() => {
        if (successCount > 0) {
          updateToast(originalToastId, {
            message: getSuccessMessage(action, `${successCount} out of ${totalCount} ${minersMessage}`),
            status: TOAST_STATUSES.success,
          });
        } else {
          removeToast(originalToastId);
        }

        if (streamCompletedNormally && errorToastId && retryAction && failureDeviceIds.length > 0) {
          const capturedToastId = errorToastId;
          const capturedFailureIds = [...failureDeviceIds];
          // Guard against rapid double-clicks on the Retry button: the toast
          // dismissal and re-render are asynchronous, so a second click can
          // fire the onClick before the button unmounts. Without this flag,
          // that would dispatch the action's API call twice.
          let hasFired = false;
          updateToast(capturedToastId, {
            actions: [
              {
                label: "Retry",
                onClick: () => {
                  if (hasFired) return;
                  hasFired = true;
                  removeToast(capturedToastId);
                  retryAction(capturedFailureIds);
                },
              },
            ],
          });
        }

        onBatchComplete?.(successDeviceIds, failureDeviceIds);

        // Remove failed devices from batch (revert to their original status)
        if (failureDeviceIds.length > 0) {
          removeDevicesFromBatch(batchIdentifier, failureDeviceIds);
        }

        // Actions that change device status (reboot, shutdown, wake-up, pool, firmware)
        // are handled by hasReachedExpectedStatus — keep the batch active so the
        // in-progress state stays until the device transitions. Stale cleanup
        // (5 min) is the safety net. For actions that don't change status
        // (blink LEDs, cooling, security, etc.), complete the batch immediately
        // so the transient state clears.
        const statusChangingActions = new Set<SupportedAction>([
          settingsActions.miningPool,
          deviceActions.shutdown,
          deviceActions.wakeUp,
          deviceActions.reboot,
          deviceActions.firmwareUpdate,
        ]);
        if (!statusChangingActions.has(action)) {
          completeBatchOperation(batchIdentifier);
        }
      });
    },
    [streamCommandBatchUpdates, removeDevicesFromBatch, completeBatchOperation],
  );

  const handleError = useCallback((originalToastId: number, error: string) => {
    updateToast(originalToastId, {
      message: error,
      status: TOAST_STATUSES.error,
    });
  }, []);

  // Centralizes the retry-on-partial-failure loop so every retry toast carries
  // `onClose` and every action wires `handleSuccess` identically.
  const executeBulkActionWithRetry = useCallback(
    (params: {
      action: SupportedAction;
      runAction: (args: {
        deviceSelector: DeviceSelector;
        onSuccess: (batchIdentifier: string) => void;
        onError: (error: string) => void;
      }) => void;
      deviceSelector: DeviceSelector;
      deviceIdentifiers: string[];
      loadingMessage: string;
    }) => {
      const { action, runAction, loadingMessage } = params;

      const pushLoadingToast = () =>
        pushToast({
          message: loadingMessage,
          status: TOAST_STATUSES.loading,
          longRunning: true,
          onClose: () => onActionComplete?.(),
        });

      const execute = (selector: DeviceSelector, deviceIds: string[], toastId: number) => {
        runAction({
          deviceSelector: selector,
          onSuccess: (batchIdentifier) => {
            startBatchOperation({
              batchIdentifier,
              action,
              deviceIdentifiers: deviceIds,
            });
            handleSuccess(action, toastId, batchIdentifier, undefined, (failedIds) => {
              execute(createDeviceSelector("subset", failedIds), failedIds, pushLoadingToast());
            });
          },
          onError: (error) => handleError(toastId, error),
        });
      };

      execute(params.deviceSelector, params.deviceIdentifiers, pushLoadingToast());
    },
    [handleSuccess, handleError, onActionComplete, startBatchOperation],
  );

  const handleMiningPoolSuccess = useCallback(
    (batchIdentifier: string) => {
      startBatchOperation({
        batchIdentifier: batchIdentifier,
        action: settingsActions.miningPool,
        deviceIdentifiers: deviceIdentifiers,
      });

      const toastId = pushToast({
        message: `${loadingMessages[settingsActions.miningPool]} ${minersMessage}`,
        status: TOAST_STATUSES.loading,
        longRunning: true,
        onClose: () => onActionComplete?.(),
      });
      handleSuccess(settingsActions.miningPool, toastId, batchIdentifier);
      setCurrentAction(null);
      onActionComplete?.();
    },
    [handleSuccess, onActionComplete, startBatchOperation, deviceIdentifiers],
  );

  const handleMiningPoolError = useCallback(
    (error: string) => {
      pushToast({
        message: error,
        status: TOAST_STATUSES.error,
        longRunning: true,
      });
      setCurrentAction(null);
      onActionComplete?.();
    },
    [onActionComplete],
  );

  // Partial-success: the batch was created and at least some miners
  // were updated; surface the skip summary as a non-error notification.
  const handleMiningPoolWarning = useCallback((warning: string) => {
    pushToast({
      message: warning,
      status: TOAST_STATUSES.success,
      longRunning: true,
    });
  }, []);

  const handleManagePowerConfirm = useCallback(
    (performanceMode: PerformanceMode) => {
      const selectorToUse = filteredSelectorForPowerModal ?? deviceSelector;
      const deviceIdsToUse = managePowerFilteredDeviceIds ?? deviceIdentifiers;
      if (!selectorToUse) return;
      setShowManagePowerModal(false);
      setFilteredSelectorForPowerModal(undefined);
      setManagePowerFilteredDeviceIds(undefined);

      executeBulkActionWithRetry({
        action: performanceActions.managePower,
        deviceSelector: selectorToUse,
        deviceIdentifiers: deviceIdsToUse,
        loadingMessage: `${loadingMessages[performanceActions.managePower]} ${minersMessage}`,
        runAction: ({ deviceSelector: selector, onSuccess, onError }) =>
          setPowerTarget({
            deviceSelector: selector,
            performanceMode,
            onSuccess: (value: SetPowerTargetResponse) => onSuccess(value.batchIdentifier),
            onError,
          }),
      });

      setCurrentAction(null);
    },
    [
      filteredSelectorForPowerModal,
      managePowerFilteredDeviceIds,
      deviceSelector,
      setPowerTarget,
      executeBulkActionWithRetry,
      deviceIdentifiers,
    ],
  );

  const handleManagePowerDismiss = useCallback(() => {
    setShowManagePowerModal(false);
    setFilteredSelectorForPowerModal(undefined);
    setManagePowerFilteredDeviceIds(undefined);
    setCurrentAction(null);
    onActionComplete?.();
  }, [onActionComplete]);

  const handleFirmwareUpdateConfirm = useCallback(
    (firmwareFileId: string) => {
      const selectorToUse = firmwareUpdateFilteredSelector ?? deviceSelector;
      const deviceIdsToUse = firmwareUpdateFilteredDeviceIds ?? deviceIdentifiers;
      if (!selectorToUse) return;
      setShowFirmwareUpdateModal(false);
      setFirmwareUpdateFilteredSelector(undefined);
      setFirmwareUpdateFilteredDeviceIds(undefined);
      setCurrentAction(null);

      const toastId = pushToast({
        message: `${loadingMessages[deviceActions.firmwareUpdate]} ${minersMessage}`,
        status: TOAST_STATUSES.loading,
        longRunning: true,
        progress: 0,
        onClose: () => onActionComplete?.(),
      });

      const firmwareUpdateRequest = create(FirmwareUpdateRequestSchema, {
        deviceSelector: selectorToUse,
        firmwareFileId,
      });

      firmwareUpdate({
        firmwareUpdateRequest,
        onSuccess: (value: FirmwareUpdateResponse) => {
          startBatchOperation({
            batchIdentifier: value.batchIdentifier,
            action: deviceActions.firmwareUpdate,
            deviceIdentifiers: deviceIdsToUse,
          });

          const streamAbortController = new AbortController();
          let errorToastId: number | null = null;
          let successCount = 0;
          let totalCount = 0;
          let failureIds: string[] = [];
          let completionHandled = false;

          const handleCompletion = () => {
            if (completionHandled) return;
            completionHandled = true;

            if (successCount > 0) {
              updateToast(toastId, {
                message: `${successMessages[deviceActions.firmwareUpdate]} ${successCount} out of ${totalCount} ${minersMessage} — reboot required`,
                status: TOAST_STATUSES.success,
                progress: undefined,
                longRunning: true,
                ttl: false,
              });
            } else {
              removeToast(toastId);
            }

            if (failureIds.length > 0) {
              removeDevicesFromBatch(value.batchIdentifier, failureIds);
            }

            // Don't complete batch — let hasReachedExpectedStatus clear the
            // in-progress state once the device reports REBOOT_REQUIRED.
            // Stale cleanup handles eventual state cleanup.
            onRefetchMiners?.();
            onActionComplete?.();
          };

          streamCommandBatchUpdates({
            streamRequest: create(StreamCommandBatchUpdatesRequestSchema, {
              batchIdentifier: value.batchIdentifier,
            }),
            streamAbortController,
            onStreamData: (response) => {
              totalCount = Number(response.status?.commandBatchDeviceCount?.total || 0);
              successCount = Number(response.status?.commandBatchDeviceCount?.success || 0);
              const failureCount = Number(response.status?.commandBatchDeviceCount?.failure || 0);
              failureIds = response.status?.commandBatchDeviceCount?.failureDeviceIdentifiers || [];
              // successDeviceIdentifiers no longer needed — optimistic mutation removed
              const completed = successCount + failureCount;
              const progress = totalCount > 0 ? Math.round((completed / totalCount) * 100) : 0;

              if (successCount > 0) {
                updateToast(toastId, {
                  message: `${successMessages[deviceActions.firmwareUpdate]} ${successCount} out of ${totalCount} ${minersMessage}`,
                  status: TOAST_STATUSES.success,
                  progress,
                });
              }

              if (failureCount > 0) {
                if (!errorToastId) {
                  errorToastId = pushToast({
                    message: `Firmware update failed on ${failureCount} out of ${totalCount} ${minersMessage}`,
                    status: TOAST_STATUSES.error,
                    longRunning: true,
                  });
                } else {
                  updateToast(errorToastId, {
                    message: `Firmware update failed on ${failureCount} out of ${totalCount} ${minersMessage}`,
                    status: TOAST_STATUSES.error,
                  });
                }
              }

              if (completed === totalCount && totalCount > 0) {
                handleCompletion();
                streamAbortController.abort();
              }
            },
          }).finally(() => {
            handleCompletion();
          });
        },
        onError: (error) => {
          updateToast(toastId, {
            message: `Firmware update failed: ${error}`,
            status: TOAST_STATUSES.error,
            progress: undefined,
          });
          onActionComplete?.();
        },
      });
    },
    [
      firmwareUpdateFilteredSelector,
      firmwareUpdateFilteredDeviceIds,
      deviceSelector,
      firmwareUpdate,
      startBatchOperation,
      removeDevicesFromBatch,
      streamCommandBatchUpdates,
      deviceIdentifiers,
      onActionComplete,
      onRefetchMiners,
    ],
  );

  const handleFirmwareUpdateDismiss = useCallback(() => {
    setShowFirmwareUpdateModal(false);
    setFirmwareUpdateFilteredSelector(undefined);
    setFirmwareUpdateFilteredDeviceIds(undefined);
    setCurrentAction(null);
    onActionComplete?.();
  }, [onActionComplete]);

  const handleCoolingModeConfirm = useCallback(
    (coolingMode: CoolingMode) => {
      const selectorToUse = coolingModeFilteredSelector ?? deviceSelector;
      const deviceIdsToUse = coolingModeFilteredDeviceIds ?? deviceIdentifiers;

      if (!selectorToUse) return;
      setShowCoolingModeModal(false);
      setCoolingModeFilteredSelector(undefined);
      setCoolingModeFilteredDeviceIds(undefined);

      executeBulkActionWithRetry({
        action: settingsActions.coolingMode,
        deviceSelector: selectorToUse,
        deviceIdentifiers: deviceIdsToUse,
        loadingMessage: `${loadingMessages[settingsActions.coolingMode]} ${minersMessage}`,
        runAction: ({ deviceSelector: selector, onSuccess, onError }) =>
          setCoolingMode({
            deviceSelector: selector,
            coolingMode,
            onSuccess: (value: SetCoolingModeResponse) => onSuccess(value.batchIdentifier),
            onError,
          }),
      });

      setCurrentAction(null);
    },
    [
      coolingModeFilteredSelector,
      coolingModeFilteredDeviceIds,
      deviceSelector,
      setCoolingMode,
      executeBulkActionWithRetry,
      deviceIdentifiers,
    ],
  );

  const handleCoolingModeDismiss = useCallback(() => {
    setShowCoolingModeModal(false);
    setCoolingModeFilteredSelector(undefined);
    setCoolingModeFilteredDeviceIds(undefined);
    setCurrentCoolingMode(undefined);
    setCurrentAction(null);
    onActionComplete?.();
  }, [onActionComplete]);

  const handleRenameConfirm = useCallback(
    async (name: string) => {
      const deviceIdentifier = selectedMiners[0]?.deviceIdentifier;
      if (!deviceIdentifier) return;

      setShowRenameDialog(false);
      setCurrentAction(null);

      const id = pushToast({
        message: loadingMessages[settingsActions.rename],
        status: TOAST_STATUSES.loading,
        longRunning: true,
      });

      try {
        await renameSingleMiner(deviceIdentifier, name);
        updateToast(id, { message: successMessages[settingsActions.rename], status: TOAST_STATUSES.success });
        onRefetchMiners?.();
      } catch {
        updateToast(id, { message: "Failed to rename miner", status: TOAST_STATUSES.error });
      } finally {
        onActionComplete?.();
      }
    },
    [selectedMiners, renameSingleMiner, onActionComplete, onRefetchMiners],
  );

  const handleRenameDismiss = useCallback(() => {
    setShowRenameDialog(false);
    setCurrentAction(null);
    onActionComplete?.();
  }, [onActionComplete]);

  const handleRenameOpen = useCallback(() => {
    setCurrentAction(settingsActions.rename);
    setShowRenameDialog(true);
    onActionStart?.();
  }, [onActionStart]);

  const handleAddToGroupDismiss = useCallback(() => {
    setShowAddToGroupModal(false);
    setCurrentAction(null);
    onActionComplete?.();
  }, [onActionComplete]);

  // Ref used to wire handleSecurityAuthenticated into the auth hook's onAuthenticated callback
  // without creating a circular dependency between the two hooks.
  const handleSecurityAuthRef = useRef<((username: string, password: string) => Promise<void>) | null>(null);

  const {
    showAuthenticateFleetModal,
    authenticationPurpose,
    fleetCredentials,
    startAuthentication,
    handleFleetAuthenticated,
    handleAuthDismiss,
    resetAuthState,
  } = useFleetAuthentication({
    onAuthenticated: useCallback((purpose: "security" | "pool", username: string, password: string) => {
      if (purpose === "security") {
        void handleSecurityAuthRef.current?.(username, password);
      } else {
        setShowPoolSelectionPage(true);
      }
    }, []),
    onDismiss: useCallback(() => {
      setPoolFilteredDeviceIds(undefined);
      setShowPoolSelectionPage(false);
      setCurrentAction(null);
      onActionComplete?.();
    }, [onActionComplete]),
  });

  const {
    showManageSecurityModal,
    showUpdatePasswordModal,
    hasThirdPartyMiners,
    minerGroups,
    startManageSecurity,
    handleSecurityAuthenticated,
    handleUpdateGroup,
    handleSecurityModalClose,
    handlePasswordConfirm,
    handlePasswordDismiss,
  } = useManageSecurityFlow({
    deviceIdentifiers,
    selectionMode,
    getMinerModelGroups,
    withCapabilityCheck,
    updateMinerPassword,
    startBatchOperation,
    handleSuccess,
    handleError,
    onActionComplete,
    setCurrentAction,
    fleetCredentials,
    resetAuthState,
    miners,
    currentFilter,
  });

  useEffect(() => {
    handleSecurityAuthRef.current = handleSecurityAuthenticated;
  });

  const handleConfirmation = useCallback(
    async (filteredSelector?: DeviceSelector, filteredDeviceIds?: string[], actionOverride?: SupportedAction) => {
      // Use filtered selector/identifiers if provided (from unsupported miners modal),
      // otherwise use the default selector/identifiers for all selected miners
      const selectorToUse = filteredSelector ?? deviceSelector;
      const deviceIdsToUse = filteredDeviceIds ?? deviceIdentifiers;
      // Use actionOverride when called from unsupported miners modal (where currentAction is null)
      const action = actionOverride ?? currentAction;

      if (action === null || !selectorToUse) return;

      // Handle device action API calls
      switch (action) {
        case deviceActions.shutdown: {
          executeBulkActionWithRetry({
            action: deviceActions.shutdown,
            deviceSelector: selectorToUse,
            deviceIdentifiers: deviceIdsToUse,
            loadingMessage: getLoadingMessage(deviceActions.shutdown, minersMessage),
            runAction: ({ deviceSelector: selector, onSuccess, onError }) =>
              stopMining({
                stopMiningRequest: create(StopMiningRequestSchema, { deviceSelector: selector }),
                onSuccess: (value: StopMiningResponse) => onSuccess(value.batchIdentifier),
                onError,
              }),
          });
          break;
        }
        case deviceActions.wakeUp: {
          executeBulkActionWithRetry({
            action: deviceActions.wakeUp,
            deviceSelector: selectorToUse,
            deviceIdentifiers: deviceIdsToUse,
            loadingMessage: getLoadingMessage(deviceActions.wakeUp, minersMessage),
            runAction: ({ deviceSelector: selector, onSuccess, onError }) =>
              startMining({
                startMiningRequest: create(StartMiningRequestSchema, { deviceSelector: selector }),
                onSuccess: (value: StartMiningResponse) => onSuccess(value.batchIdentifier),
                onError,
              }),
          });
          break;
        }
        case deviceActions.unpair: {
          // Unpair is not retry-eligible (synchronous deletion, not a streamed
          // batch command), so it manages its own toast lifecycle.
          const unpairToastId = pushToast({
            message: getLoadingMessage(action, minersMessage),
            status: TOAST_STATUSES.loading,
            longRunning: true,
            onClose: () => onActionComplete?.(),
          });
          const unpairBatchId = crypto.randomUUID();
          startBatchOperation({
            batchIdentifier: unpairBatchId,
            action: deviceActions.unpair,
            deviceIdentifiers: deviceIdsToUse,
          });

          const deleteRequest = create(DeleteMinersRequestSchema, {
            deviceSelector: create(DeviceSelectorSchema, {
              selectionType:
                selectionMode === "all"
                  ? { case: "allDevices", value: currentFilter ?? create(MinerListFilterSchema) }
                  : {
                      case: "includeDevices",
                      value: create(DeviceIdentifierListSchema, { deviceIdentifiers: deviceIdsToUse }),
                    },
            }),
          });
          deleteMiners({
            deleteMinersRequest: deleteRequest,
            onSuccess: (value: DeleteMinersResponse) => {
              completeBatchOperation(unpairBatchId);
              updateToast(unpairToastId, {
                message: `${successMessages[deviceActions.unpair]} ${value.deletedCount} ${value.deletedCount === 1 ? "miner" : "miners"}`,
                status: TOAST_STATUSES.success,
              });
              onRefetchMiners?.();
              onActionComplete?.();
            },
            onError: (error) => {
              completeBatchOperation(unpairBatchId);
              handleError(unpairToastId, error);
              onActionComplete?.();
            },
          });
          break;
        }
        case deviceActions.reboot: {
          executeBulkActionWithRetry({
            action: deviceActions.reboot,
            deviceSelector: selectorToUse,
            deviceIdentifiers: deviceIdsToUse,
            loadingMessage: getLoadingMessage(deviceActions.reboot, minersMessage),
            runAction: ({ deviceSelector: selector, onSuccess, onError }) =>
              reboot({
                rebootRequest: create(RebootRequestSchema, { deviceSelector: selector }),
                onSuccess: (value: RebootResponse) => onSuccess(value.batchIdentifier),
                onError,
              }),
          });
          break;
        }
        default:
          pushToast({
            message: "Unimplemented action",
            status: TOAST_STATUSES.error,
          });
      }
      setCurrentAction(null);
    },
    [
      currentAction,
      onActionComplete,
      deviceSelector,
      selectionMode,
      startMining,
      stopMining,
      deleteMiners,
      reboot,
      handleError,
      startBatchOperation,
      completeBatchOperation,
      deviceIdentifiers,
      currentFilter,
      onRefetchMiners,
      executeBulkActionWithRetry,
    ],
  );

  const handleCancel = useCallback(() => {
    setCurrentAction(null);
    setShowPoolSelectionPage(false);
    resetAuthState();
    onActionComplete?.();
  }, [resetAuthState, onActionComplete]);

  const popoverActions = useMemo(() => {
    // Device actions handlers
    const handleBlinkLEDs = () => {
      if (!deviceSelector) return;
      setCurrentAction(deviceActions.blinkLEDs);

      executeBulkActionWithRetry({
        action: deviceActions.blinkLEDs,
        deviceSelector,
        deviceIdentifiers,
        loadingMessage: loadingMessages[deviceActions.blinkLEDs],
        runAction: ({ deviceSelector: selector, onSuccess, onError }) =>
          blinkLED({
            blinkLEDRequest: create(BlinkLEDRequestSchema, { deviceSelector: selector }),
            onSuccess: (value: BlinkLEDResponse) => onSuccess(value.batchIdentifier),
            onError,
          }),
      });
    };

    const handleDownloadLogs = async () => {
      if (!deviceSelector) return;
      onActionStart?.();

      await withCapabilityCheck(deviceActions.downloadLogs, (filteredSelector) => {
        const selectorToUse = filteredSelector ?? deviceSelector;

        const id = pushToast({
          message: loadingMessages[deviceActions.downloadLogs],
          status: TOAST_STATUSES.loading,
          longRunning: true,
        });

        const request = create(DownloadLogsRequestSchema, { deviceSelector: selectorToUse });
        downloadLogs({
          downloadLogsRequest: request,
          onSuccess: ({ batchIdentifier }) => {
            const streamAbortController = new AbortController();
            let failureCount = 0;
            let successCount = 0;
            let allDevicesFailed = false;
            let finishedReceived = false;
            streamCommandBatchUpdates({
              streamRequest: create(StreamCommandBatchUpdatesRequestSchema, { batchIdentifier }),
              streamAbortController,
              onStreamData: (response) => {
                if (
                  response.status?.commandBatchUpdateStatus ===
                  CommandBatchUpdateStatus_CommandBatchUpdateStatusType.FINISHED
                ) {
                  failureCount = Number(response.status.commandBatchDeviceCount?.failure ?? 0);
                  successCount = Number(response.status.commandBatchDeviceCount?.success ?? 0);
                  allDevicesFailed = successCount === 0 && failureCount > 0;
                  finishedReceived = true;
                  streamAbortController.abort();
                }
              },
            }).finally(() => {
              if (!finishedReceived) {
                updateToast(id, {
                  message: "Failed to download logs",
                  status: TOAST_STATUSES.error,
                });
                onActionComplete?.();
                return;
              }

              if (allDevicesFailed) {
                updateToast(id, {
                  message: "Failed to download logs",
                  status: TOAST_STATUSES.error,
                });
                onActionComplete?.();
                return;
              }

              getCommandBatchLogBundle({
                request: create(GetCommandBatchLogBundleRequestSchema, { batchIdentifier }),
                onSuccess: ({ chunkData, filename }) => {
                  const mimeType = filename.endsWith(".csv") ? "text/csv" : "application/zip";
                  const blob = new Blob([chunkData as Uint8Array<ArrayBuffer>], { type: mimeType });
                  downloadBlob(blob, filename);
                  updateToast(id, {
                    message: successMessages[deviceActions.downloadLogs],
                    status: TOAST_STATUSES.success,
                  });
                  if (failureCount > 0) {
                    pushToast({
                      message: `Failed to retrieve logs from ${failureCount} ${failureCount === 1 ? "miner" : "miners"}`,
                      status: TOAST_STATUSES.error,
                      longRunning: true,
                    });
                  }
                  onActionComplete?.();
                },
                onError: (err) => {
                  updateToast(id, {
                    message: err || "Failed to download logs",
                    status: TOAST_STATUSES.error,
                  });
                  onActionComplete?.();
                },
              });
            });
          },
          onError: (err) => {
            handleError(id, err);
            onActionComplete?.();
          },
        });
      });
    };

    const handleReboot = async () => {
      onActionStart?.();
      // Check for unsupported miners first - only show confirmation dialog if all supported
      const modalShown = await checkAndShowUnsupportedMinersModal(
        deviceActions.reboot,
        (filteredSelector, filteredDeviceIds) => {
          // This will be called when user clicks Continue on unsupported miners modal
          // The confirmation dialog will not be shown, action executes directly
          handleConfirmation(filteredSelector, filteredDeviceIds, deviceActions.reboot);
        },
      );
      // Only show confirmation dialog if capability modal was not shown
      if (!modalShown) {
        setCurrentAction(deviceActions.reboot);
      }
    };

    const handleShutDown = async () => {
      onActionStart?.();
      const modalShown = await checkAndShowUnsupportedMinersModal(
        deviceActions.shutdown,
        (filteredSelector, filteredDeviceIds) => {
          handleConfirmation(filteredSelector, filteredDeviceIds, deviceActions.shutdown);
        },
      );
      if (!modalShown) {
        setCurrentAction(deviceActions.shutdown);
      }
    };

    const handleWakeUp = async () => {
      onActionStart?.();
      const modalShown = await checkAndShowUnsupportedMinersModal(
        deviceActions.wakeUp,
        (filteredSelector, filteredDeviceIds) => {
          handleConfirmation(filteredSelector, filteredDeviceIds, deviceActions.wakeUp);
        },
      );
      if (!modalShown) {
        setCurrentAction(deviceActions.wakeUp);
      }
    };

    const handleUnpair = () => {
      setCurrentAction(deviceActions.unpair);
      onActionStart?.();
    };

    // Performance actions handlers
    const handleManagePower = async () => {
      onActionStart?.();
      await withCapabilityCheck(performanceActions.managePower, (filteredSelector, filteredDeviceIds) => {
        setFilteredSelectorForPowerModal(filteredSelector);
        setManagePowerFilteredDeviceIds(filteredDeviceIds);
        setCurrentAction(performanceActions.managePower);
        setShowManagePowerModal(true);
      });
    };

    // Settings actions handlers
    const handleMiningPool = async () => {
      onActionStart?.();
      await withCapabilityCheck(settingsActions.miningPool, (_filteredSelector, filteredDeviceIds) => {
        setPoolFilteredDeviceIds(filteredDeviceIds);
        setCurrentAction(settingsActions.miningPool);
        startAuthentication("pool");
      });
    };

    const handleCoolingMode = async () => {
      onActionStart?.();

      // For single miner, fetch current cooling mode for prepopulation
      if (selectedMiners.length === 1) {
        const mode = await fetchCoolingMode(selectedMiners[0].deviceIdentifier);
        setCurrentCoolingMode(mode);
      } else {
        setCurrentCoolingMode(undefined);
      }

      await withCapabilityCheck(settingsActions.coolingMode, (filteredSelector, filteredDeviceIds) => {
        setCoolingModeFilteredSelector(filteredSelector);
        setCoolingModeFilteredDeviceIds(filteredDeviceIds);
        setCurrentAction(settingsActions.coolingMode);
        setShowCoolingModeModal(true);
      });
    };

    const handleManageSecurity = () => {
      onActionStart?.();
      startManageSecurity();
      startAuthentication("security");
    };

    const handleAddToGroup = () => {
      setCurrentAction(groupActions.addToGroup);
      setShowAddToGroupModal(true);
      onActionStart?.();
    };

    const handleFirmwareUpdate = async () => {
      onActionStart?.();

      if (selectionMode === "all") {
        pushToast({
          message: "Firmware update requires selecting specific miners to verify model compatibility.",
          status: TOAST_STATUSES.error,
        });
        onActionComplete?.();
        return;
      }

      await withCapabilityCheck(deviceActions.firmwareUpdate, (filteredSelector, filteredDeviceIds) => {
        const idsToCheck = filteredDeviceIds ?? deviceIdentifiers;
        const { models, hasMissing } =
          idsToCheck.length > 0
            ? getUniqueModels(idsToCheck, miners)
            : { models: new Set<string>(), hasMissing: false };

        if (models.size === 0) {
          pushToast({
            message: "Unable to verify miner model compatibility. Please select specific miners.",
            status: TOAST_STATUSES.error,
          });
          onActionComplete?.();
          return;
        }

        if (hasMissing) {
          pushToast({
            message: "Some selected miners have unknown models. Please deselect them before updating firmware.",
            status: TOAST_STATUSES.error,
          });
          onActionComplete?.();
          return;
        }

        if (models.size > 1) {
          pushToast({
            message: "Firmware update requires miners of the same model. Your selection includes multiple models.",
            status: TOAST_STATUSES.error,
          });
          onActionComplete?.();
          return;
        }

        setFirmwareUpdateFilteredSelector(filteredSelector);
        setFirmwareUpdateFilteredDeviceIds(filteredDeviceIds);
        setCurrentAction(deviceActions.firmwareUpdate);
        setShowFirmwareUpdateModal(true);
      });
    };

    const sleepAction: BulkAction<SupportedAction> = {
      action: deviceActions.shutdown,
      title: "Sleep",
      icon: <Power />,
      actionHandler: handleShutDown,
      requiresConfirmation: true,
      confirmation: {
        title: `Sleep ${displayCount} ${displayCount === 1 ? "miner" : "miners"}?`,
        subtitle: `${displayCount === 1 ? "This miner" : "These miners"} will go to sleep and stop hashing.`,
        confirmAction: {
          title: "Sleep",
          variant: variants.primary,
        },
        testId: "shutdown-confirm-button",
      },
    };

    const wakeUpAction: BulkAction<SupportedAction> = {
      action: deviceActions.wakeUp,
      title: "Wake up",
      icon: <Play />,
      actionHandler: handleWakeUp,
      requiresConfirmation: true,
      confirmation: {
        title: `Wake up ${displayCount} ${displayCount === 1 ? "miner" : "miners"}?`,
        subtitle: `${displayCount === 1 ? "This miner" : "These miners"} will wake up and start hashing.`,
        confirmAction: {
          title: "Wake up",
          variant: variants.primary,
        },
        testId: "wake-up-confirm-button",
      },
    };

    // Determine which power state actions to show based on device status
    const powerStateActions =
      deviceStatus === undefined
        ? [sleepAction, wakeUpAction] // Bulk actions: show both
        : deviceStatus === DeviceStatus.INACTIVE
          ? [wakeUpAction] // Single miner asleep: show wake up only
          : [sleepAction]; // Single miner active: show sleep only

    return [
      // Device actions - ordered per design specifications
      ...powerStateActions, // Sleep/Wake up at top
      {
        action: deviceActions.reboot,
        title: "Reboot",
        icon: <Reboot />,
        actionHandler: handleReboot,
        requiresConfirmation: true,
        confirmation: {
          title: `Reboot ${displayCount} ${displayCount === 1 ? "miner" : "miners"}?`,
          subtitle: `${displayCount === 1 ? "This miner" : "These miners"} will temporarily go offline but will resume hashing automatically after they reboot.`,
          confirmAction: {
            title: "Reboot",
            variant: variants.primary,
          },
          testId: "reboot-confirm-button",
        },
      },
      {
        action: deviceActions.blinkLEDs,
        title: "Blink LEDs",
        icon: <LEDIndicator />,
        actionHandler: handleBlinkLEDs,
        requiresConfirmation: false,
      },
      {
        action: deviceActions.downloadLogs,
        title: "Download logs",
        icon: <Terminal />,
        actionHandler: handleDownloadLogs,
        requiresConfirmation: false,
        showGroupDivider: true,
      },
      // Performance and settings actions
      {
        action: performanceActions.managePower,
        title: "Manage power",
        icon: <Speedometer />,
        actionHandler: handleManagePower,
        requiresConfirmation: false,
      },
      {
        action: deviceActions.firmwareUpdate,
        title: "Update firmware",
        icon: <Settings />,
        actionHandler: handleFirmwareUpdate,
        requiresConfirmation: false,
      },
      {
        action: settingsActions.miningPool,
        title: "Edit pool",
        icon: <MiningPools />,
        actionHandler: handleMiningPool,
        requiresConfirmation: false,
      },
      {
        action: settingsActions.coolingMode,
        title: "Change cooling mode",
        icon: <Fan />,
        actionHandler: handleCoolingMode,
        requiresConfirmation: false,
        showGroupDivider: true, // End of performance/settings group
      },
      {
        action: groupActions.addToGroup,
        title: "Add to group",
        icon: <Groups />,
        actionHandler: handleAddToGroup,
        requiresConfirmation: false,
        showGroupDivider: true,
      },
      // TODO: Implement Add to rack action - when implemented, move showGroupDivider from add-to-group to add-to-rack (last in organization group)
      // Security and dangerous actions (same group)
      {
        action: settingsActions.security,
        title: "Manage security",
        icon: <Lock />,
        actionHandler: handleManageSecurity,
        requiresConfirmation: false,
      },
      {
        action: deviceActions.unpair,
        title: "Unpair",
        icon: <Unpair />,
        actionHandler: handleUnpair,
        requiresConfirmation: true,
        confirmation: {
          title: `Unpair ${displayCount} ${displayCount === 1 ? "miner" : "miners"}?`,
          subtitle: unpairConfirmationSubtitle,
          confirmAction: {
            title: "Unpair",
            variant: variants.secondaryDanger,
          },
          testId: "unpair-confirm-button",
        },
      },
    ] as BulkAction<SupportedAction>[];
  }, [
    blinkLED,
    downloadLogs,
    getCommandBatchLogBundle,
    streamCommandBatchUpdates,
    handleError,
    displayCount,
    onActionStart,
    onActionComplete,
    deviceSelector,
    deviceStatus,
    withCapabilityCheck,
    checkAndShowUnsupportedMinersModal,
    handleConfirmation,
    deviceIdentifiers,
    selectionMode,
    selectedMiners,
    fetchCoolingMode,
    unpairConfirmationSubtitle,
    startManageSecurity,
    startAuthentication,
    miners,
    executeBulkActionWithRetry,
  ]);

  // Extract public UnsupportedMinersInfo (omit internal pendingAction)
  const { pendingAction: _, ...publicUnsupportedMinersInfo } = unsupportedMinersInfo;

  // Count for cooling mode modal - use filtered count if available, otherwise displayCount
  const coolingModeCount = coolingModeFilteredDeviceIds?.length ?? displayCount;

  return {
    currentAction,
    setCurrentAction,
    popoverActions,
    handleConfirmation,
    handleCancel,
    numberOfMiners,
    displayCount,
    handleMiningPoolSuccess,
    handleMiningPoolError,
    handleMiningPoolWarning,
    showPoolSelectionPage,
    poolFilteredDeviceIds,
    fleetCredentials,
    showManagePowerModal,
    handleManagePowerConfirm,
    handleManagePowerDismiss,
    showFirmwareUpdateModal,
    handleFirmwareUpdateConfirm,
    handleFirmwareUpdateDismiss,
    showCoolingModeModal,
    coolingModeCount,
    currentCoolingMode,
    handleCoolingModeConfirm,
    handleCoolingModeDismiss,
    showAuthenticateFleetModal,
    authenticationPurpose,
    showUpdatePasswordModal,
    hasThirdPartyMiners,
    handleFleetAuthenticated,
    handlePasswordConfirm,
    handlePasswordDismiss,
    handleAuthDismiss,
    withCapabilityCheck,
    unsupportedMinersInfo: publicUnsupportedMinersInfo,
    handleUnsupportedMinersContinue,
    handleUnsupportedMinersDismiss,
    showManageSecurityModal,
    minerGroups,
    handleUpdateGroup,
    handleSecurityModalClose,
    showRenameDialog,
    handleRenameOpen,
    handleRenameConfirm,
    handleRenameDismiss,
    showAddToGroupModal,
    handleAddToGroupDismiss,
  };
};
