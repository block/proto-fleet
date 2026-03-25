import { useCallback, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import {
  deviceActions,
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
import { hasReachedExpectedStatus } from "@/protoFleet/features/fleetManagement/utils/batchStatusCheck";
import { createDeviceSelector } from "@/protoFleet/features/fleetManagement/utils/deviceSelector";
import {
  useCompleteBatchOperation,
  useFleetStore,
  useRemoveDevicesFromBatch,
  useStartBatchOperation,
  useUpdateMinerName,
} from "@/protoFleet/store";
import {
  // ArrowLeftCompact, // TODO: Uncomment when Factory Reset is implemented
  // Curtail, // TODO: Uncomment when Curtail is implemented
  Fan,
  FirmwareUpdate,
  Groups,
  LEDIndicator,
  Lock,
  MiningPools,
  Play,
  Power,
  Reboot,
  Speedometer,
  Terminal,
  Trash,
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
  /** Active UI filter — forwarded as device_filter when deleting in "all" mode */
  currentFilter?: MinerListFilter;
  onActionStart?: () => void;
  onActionComplete?: () => void;
}

/**
 * Metadata for actions that require capability checking.
 * Contains both the description for the unsupported miners modal and the proto CommandType.
 * Actions not in this map don't require capability checking (e.g., delete).
 */
const actionCapabilityMetadata: Partial<Record<SupportedAction, { description: string; commandType: CommandType }>> = {
  [deviceActions.shutdown]: { description: "Sleep mode changes", commandType: CommandType.STOP_MINING },
  [deviceActions.wakeUp]: { description: "Wake-up", commandType: CommandType.START_MINING },
  [deviceActions.reboot]: { description: "Reboot", commandType: CommandType.REBOOT },
  [deviceActions.blinkLEDs]: { description: "LED blinking", commandType: CommandType.BLINK_LED },
  [deviceActions.factoryReset]: { description: "Factory reset", commandType: CommandType.UNSPECIFIED },
  [deviceActions.downloadLogs]: { description: "Log downloads", commandType: CommandType.DOWNLOAD_LOGS },
  [settingsActions.miningPool]: { description: "Pool switching", commandType: CommandType.UPDATE_MINING_POOLS },
  [settingsActions.coolingMode]: { description: "Cooling mode changes", commandType: CommandType.SET_COOLING_MODE },
  [settingsActions.security]: { description: "Password updates", commandType: CommandType.UPDATE_MINER_PASSWORD },
  [performanceActions.managePower]: { description: "Power mode changes", commandType: CommandType.SET_POWER_TARGET },
  [deviceActions.firmwareUpdate]: { description: "Firmware updates", commandType: CommandType.FIRMWARE_UPDATE },
};

function getUniqueModels(deviceIds: string[]): { models: Set<string>; hasMissing: boolean } {
  const miners = useFleetStore.getState().fleet.miners;
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
 * Builds a contextual confirmation subtitle for the delete action based on the
 * miner types and statuses in the selection (per RFC Option C).
 *
 * @param miners - the fleet miners record, passed explicitly for testability
 */
const hasActiveFilter = (filter?: MinerListFilter): boolean =>
  filter !== undefined &&
  (filter.deviceStatus.length > 0 || filter.errorComponentTypes.length > 0 || filter.models.length > 0);

const buildDeleteConfirmationSubtitle = (
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

export const useMinerActions = ({
  selectedMiners,
  selectionMode,
  totalCount,
  currentFilter,
  onActionStart,
  onActionComplete,
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

  const startBatchOperation = useStartBatchOperation();
  const completeBatchOperation = useCompleteBatchOperation();
  const removeDevicesFromBatch = useRemoveDevicesFromBatch();
  const updateMinerName = useUpdateMinerName();
  const { fetchCoolingMode } = useMinerCoolingMode();
  const { getMinerModelGroups } = useMinerModelGroups();
  const { renameSingleMiner } = useRenameMiners();

  const [currentAction, setCurrentAction] = useState<SupportedAction | null>(null);
  const [showRenameDialog, setShowRenameDialog] = useState(false);
  const [showManagePowerModal, setShowManagePowerModal] = useState(false);
  const [filteredSelectorForPowerModal, setFilteredSelectorForPowerModal] = useState<DeviceSelector | undefined>();
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

  // Read miners from store reactively so delete confirmation subtitle updates
  const fleetMiners = useFleetStore((s) => s.fleet.miners);

  const numberOfMiners = useMemo(() => selectedMiners.length, [selectedMiners]);

  // Display count for confirmation dialogs - use totalCount when in "all" mode
  const displayCount = useMemo(
    () => (selectionMode === "all" && totalCount !== undefined ? totalCount : numberOfMiners),
    [selectionMode, totalCount, numberOfMiners],
  );

  // Extract device identifiers for API calls
  const deviceIdentifiers = useMemo(() => selectedMiners.map((m) => m.deviceIdentifier), [selectedMiners]);

  // Contextual subtitle for delete confirmation dialog (per RFC Option C)
  const deleteConfirmationSubtitle = useMemo(
    () => buildDeleteConfirmationSubtitle(selectedMiners, selectionMode, displayCount, fleetMiners, currentFilter),
    [selectedMiners, selectionMode, displayCount, fleetMiners, currentFilter],
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
    ) => {
      const streamAbortController = new AbortController();

      let errorToastId: number | null = null;
      let successCount = 0;
      let totalCount = 0;
      let successDeviceIds: string[] = [];
      let failureDeviceIds: string[] = [];

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
              message: `${successMessages[action]} ${successCount} out of ${totalCount} ${minersMessage}`,
              status: TOAST_STATUSES.success,
            });
          }

          if (failureCount > 0) {
            if (!errorToastId) {
              errorToastId = pushToast({
                message: `Update failed on ${failureCount} out of ${totalCount} ${minersMessage}`,
                status: TOAST_STATUSES.error,
                longRunning: true,
              });
            } else {
              updateToast(errorToastId, {
                message: `Update failed on ${failureCount} out of ${totalCount} ${minersMessage}`,
                status: TOAST_STATUSES.error,
              });
            }
          }

          // Close the stream when we've received results for all devices
          // This triggers .finally() to clear loading states immediately
          if (successCount + failureCount === totalCount && totalCount > 0) {
            streamAbortController.abort();
          }
        },
        streamAbortController: streamAbortController,
      }).finally(() => {
        if (successCount > 0) {
          updateToast(originalToastId, {
            message: `${successMessages[action]} ${successCount} out of ${totalCount} ${minersMessage}`,
            status: TOAST_STATUSES.success,
          });
        } else {
          removeToast(originalToastId);
        }

        onBatchComplete?.(successDeviceIds, failureDeviceIds);

        // Immediately remove failed devices from batch (revert to their original status)
        if (failureDeviceIds.length > 0) {
          removeDevicesFromBatch(batchIdentifier, failureDeviceIds);
        }

        // Keep loading state until we can confirm the action has taken effect
        // For actions that change device state, wait for status confirmation on successful devices only
        const shouldWaitForStatusChange =
          (action === settingsActions.miningPool ||
            action === deviceActions.shutdown ||
            action === deviceActions.wakeUp ||
            action === deviceActions.reboot) &&
          successDeviceIds.length > 0;

        if (shouldWaitForStatusChange) {
          // Wait for device status to change to expected state
          // Polls every 3 seconds for successful devices only. Stale batch cleanup (5 minutes) handles stuck cases.
          const checkInterval = 3000;

          let pollCount = 0;
          const maxPolls = 60; // 3 minutes max (60 * 3000ms)

          const waitForStatusChange = () => {
            const store = useFleetStore.getState();
            pollCount++;

            // Get batch startedAt for time-based checks (e.g., minimum reboot duration)
            const batchOperation = store.fleet.batchOperations.byBatchId[batchIdentifier];
            const batchStartedAt = batchOperation?.startedAt;

            // Check status for all successfully queued devices (from successDeviceIds array)
            // Note: For "select all" operations, only visible/paginated device IDs are stored client-side.
            // This is intentional - polling will update status for all devices, but we only track
            // loading states for devices in the current view. Non-visible devices will show updated
            // status when they scroll into view.
            const allSuccessfulDevicesUpdated = successDeviceIds.every((deviceId) => {
              const miner = store.fleet.miners[deviceId];
              if (!miner) return false;

              return hasReachedExpectedStatus(action, miner.deviceStatus, batchStartedAt);
            });

            if (allSuccessfulDevicesUpdated) {
              completeBatchOperation(batchIdentifier);
            } else if (pollCount >= maxPolls) {
              // After 3 minutes, complete batch to avoid stuck loading states
              // Stale batch cleanup (5 minutes) will handle any truly stuck cases
              completeBatchOperation(batchIdentifier);
            } else {
              setTimeout(waitForStatusChange, checkInterval);
            }
          };

          waitForStatusChange();
        } else {
          // Complete batch immediately for actions that don't change device state (blink, etc.)
          completeBatchOperation(batchIdentifier);
        }
      });
    },
    [streamCommandBatchUpdates, completeBatchOperation, removeDevicesFromBatch],
  );

  const handleError = useCallback((originalToastId: number, error: string) => {
    updateToast(originalToastId, {
      message: error,
      status: TOAST_STATUSES.error,
    });
  }, []);

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

  const handleManagePowerConfirm = useCallback(
    (performanceMode: PerformanceMode) => {
      const selectorToUse = filteredSelectorForPowerModal ?? deviceSelector;
      if (!selectorToUse) return;
      setShowManagePowerModal(false);
      setFilteredSelectorForPowerModal(undefined);

      const id = pushToast({
        message: `${loadingMessages[performanceActions.managePower]} ${minersMessage}`,
        status: TOAST_STATUSES.loading,
        longRunning: true,
        onClose: () => onActionComplete?.(),
      });

      // Note: setPowerTarget does NOT use batch operation tracking because:
      // 1. Power target changes complete instantly (<1s) - no meaningful loading state to show
      // 2. No device status change to wait for (power target is a setting, not a status)
      // 3. Toast notification provides sufficient feedback for this quick operation
      setPowerTarget({
        deviceSelector: selectorToUse,
        performanceMode,
        onSuccess: (value: SetPowerTargetResponse) => {
          handleSuccess(performanceActions.managePower, id, value.batchIdentifier);
        },
        onError: handleError.bind(null, id),
      });

      setCurrentAction(null);
    },
    [filteredSelectorForPowerModal, deviceSelector, setPowerTarget, handleSuccess, handleError, onActionComplete],
  );

  const handleManagePowerDismiss = useCallback(() => {
    setShowManagePowerModal(false);
    setFilteredSelectorForPowerModal(undefined);
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
                    message: `Firmware upload failed on ${failureCount} out of ${totalCount} ${minersMessage}`,
                    status: TOAST_STATUSES.error,
                    longRunning: true,
                  });
                } else {
                  updateToast(errorToastId, {
                    message: `Firmware upload failed on ${failureCount} out of ${totalCount} ${minersMessage}`,
                    status: TOAST_STATUSES.error,
                  });
                }
              }

              if (completed === totalCount && totalCount > 0) {
                streamAbortController.abort();
              }
            },
          }).finally(() => {
            if (successCount > 0) {
              updateToast(toastId, {
                message: `${successMessages[deviceActions.firmwareUpdate]} ${successCount} out of ${totalCount} ${minersMessage}`,
                status: TOAST_STATUSES.success,
                progress: undefined,
              });
            } else {
              removeToast(toastId);
            }

            completeBatchOperation(value.batchIdentifier);
            if (failureIds.length > 0) {
              removeDevicesFromBatch(value.batchIdentifier, failureIds);
            }
            onActionComplete?.();
          });
        },
        onError: (error) => {
          updateToast(toastId, {
            message: `Firmware upload failed: ${error}`,
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
      completeBatchOperation,
      removeDevicesFromBatch,
      streamCommandBatchUpdates,
      deviceIdentifiers,
      onActionComplete,
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

      const id = pushToast({
        message: `${loadingMessages[settingsActions.coolingMode]} ${minersMessage}`,
        status: TOAST_STATUSES.loading,
        longRunning: true,
        onClose: () => onActionComplete?.(),
      });

      setCoolingMode({
        deviceSelector: selectorToUse,
        coolingMode,
        onSuccess: (value: SetCoolingModeResponse) => {
          startBatchOperation({
            batchIdentifier: value.batchIdentifier,
            action: settingsActions.coolingMode,
            deviceIdentifiers: deviceIdsToUse,
          });
          handleSuccess(settingsActions.coolingMode, id, value.batchIdentifier);
        },
        onError: handleError.bind(null, id),
      });

      setCurrentAction(null);
    },
    [
      coolingModeFilteredSelector,
      coolingModeFilteredDeviceIds,
      deviceSelector,
      setCoolingMode,
      handleSuccess,
      handleError,
      onActionComplete,
      startBatchOperation,
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
        updateMinerName(deviceIdentifier, name);
        updateToast(id, { message: successMessages[settingsActions.rename], status: TOAST_STATUSES.success });
      } catch {
        updateToast(id, { message: "Failed to rename miner", status: TOAST_STATUSES.error });
      } finally {
        onActionComplete?.();
      }
    },
    [selectedMiners, renameSingleMiner, updateMinerName, onActionComplete],
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
  });

  handleSecurityAuthRef.current = handleSecurityAuthenticated;

  const handleConfirmation = useCallback(
    async (filteredSelector?: DeviceSelector, filteredDeviceIds?: string[], actionOverride?: SupportedAction) => {
      // Use filtered selector/identifiers if provided (from unsupported miners modal),
      // otherwise use the default selector/identifiers for all selected miners
      const selectorToUse = filteredSelector ?? deviceSelector;
      const deviceIdsToUse = filteredDeviceIds ?? deviceIdentifiers;
      // Use actionOverride when called from unsupported miners modal (where currentAction is null)
      const action = actionOverride ?? currentAction;

      if (action === null || !selectorToUse) return;

      const id = pushToast({
        message: `${loadingMessages[action]} ${minersMessage}`,
        status: TOAST_STATUSES.loading,
        longRunning: true,
        onClose: () => onActionComplete?.(),
      });

      // Handle device action API calls
      switch (action) {
        case deviceActions.shutdown: {
          const stopMiningRequest = create(StopMiningRequestSchema, {
            deviceSelector: selectorToUse,
          });
          stopMining({
            stopMiningRequest: stopMiningRequest,
            onSuccess: (value: StopMiningResponse) => {
              startBatchOperation({
                batchIdentifier: value.batchIdentifier,
                action: deviceActions.shutdown,
                deviceIdentifiers: deviceIdsToUse,
              });
              handleSuccess(deviceActions.shutdown, id, value.batchIdentifier);
            },
            onError: handleError.bind(null, id),
          });
          break;
        }
        case deviceActions.wakeUp: {
          const startMiningRequest = create(StartMiningRequestSchema, {
            deviceSelector: selectorToUse,
          });
          startMining({
            startMiningRequest: startMiningRequest,
            onSuccess: (value: StartMiningResponse) => {
              startBatchOperation({
                batchIdentifier: value.batchIdentifier,
                action: deviceActions.wakeUp,
                deviceIdentifiers: deviceIdsToUse,
              });
              handleSuccess(deviceActions.wakeUp, id, value.batchIdentifier);
            },
            onError: handleError.bind(null, id),
          });
          break;
        }
        case deviceActions.delete: {
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
              updateToast(id, {
                message: `${successMessages[deviceActions.delete]} ${value.deletedCount} ${value.deletedCount === 1 ? "miner" : "miners"}`,
                status: TOAST_STATUSES.success,
              });
              useFleetStore.getState().fleet.refetchMiners?.();
              onActionComplete?.();
            },
            onError: (error) => {
              handleError(id, error);
              onActionComplete?.();
            },
          });
          break;
        }
        case deviceActions.reboot: {
          const rebootRequest = create(RebootRequestSchema, {
            deviceSelector: selectorToUse,
          });
          reboot({
            rebootRequest: rebootRequest,
            onSuccess: (value: RebootResponse) => {
              startBatchOperation({
                batchIdentifier: value.batchIdentifier,
                action: deviceActions.reboot,
                deviceIdentifiers: deviceIdsToUse,
              });
              handleSuccess(deviceActions.reboot, id, value.batchIdentifier);
            },
            onError: handleError.bind(null, id),
          });
          break;
        }
        default:
          // TODO remove this once all actions are implemented
          updateToast(id, {
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
      handleSuccess,
      handleError,
      startBatchOperation,
      deviceIdentifiers,
      currentFilter,
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
      const id = pushToast({
        message: loadingMessages[deviceActions.blinkLEDs],
        status: TOAST_STATUSES.loading,
        longRunning: true,
      });

      const blinkLEDRequest = create(BlinkLEDRequestSchema, {
        deviceSelector,
      });

      blinkLED({
        blinkLEDRequest,
        onSuccess: (value: BlinkLEDResponse) => {
          startBatchOperation({
            batchIdentifier: value.batchIdentifier,
            action: deviceActions.blinkLEDs,
            deviceIdentifiers: deviceIdentifiers,
          });
          handleSuccess(deviceActions.blinkLEDs, id, value.batchIdentifier);
        },
        onError: handleError.bind(null, id),
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

    // TODO: Implement Factory Reset action
    // const handleFactoryReset = () => {
    //   setCurrentAction(deviceActions.factoryReset);
    //   onActionStart?.();
    // };

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

    const handleDelete = () => {
      setCurrentAction(deviceActions.delete);
      onActionStart?.();
    };

    // Performance actions handlers
    const handleManagePower = async () => {
      onActionStart?.();
      await withCapabilityCheck(performanceActions.managePower, (filteredSelector) => {
        setFilteredSelectorForPowerModal(filteredSelector);
        setCurrentAction(performanceActions.managePower);
        setShowManagePowerModal(true);
      });
    };

    // TODO: Implement Curtail action
    // const handleCurtail = () => {
    //   setCurrentAction(performanceActions.curtail);
    //   onActionStart?.();
    // };

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
          idsToCheck.length > 0 ? getUniqueModels(idsToCheck) : { models: new Set<string>(), hasMissing: false };

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
        icon: <FirmwareUpdate />,
        actionHandler: handleFirmwareUpdate,
        requiresConfirmation: false,
      },
      // TODO: Implement Curtail action
      // {
      //   action: performanceActions.curtail,
      //   title: "Curtail",
      //   icon: <Curtail />,
      //   actionHandler: handleCurtail,
      //   requiresConfirmation: true,
      //   confirmation: {
      //     title: `Curtail ${numberOfMiners} miners?`,
      //     subtitle:
      //       "These miners will reduce power to 0.1 kW and stop hashing.",
      //     confirmAction: {
      //       title: "Curtail",
      //       variant: variants.primary,
      //     },
      //     testId: "curtail-confirm-button",
      //   },
      // },
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
        action: deviceActions.delete,
        title: "Delete",
        icon: <Trash />,
        actionHandler: handleDelete,
        requiresConfirmation: true,
        confirmation: {
          title: `Delete ${displayCount} ${displayCount === 1 ? "miner" : "miners"}?`,
          subtitle: deleteConfirmationSubtitle,
          confirmAction: {
            title: "Delete",
            variant: variants.secondaryDanger,
          },
          testId: "delete-confirm-button",
        },
      },
    ] as BulkAction<SupportedAction>[];
  }, [
    blinkLED,
    downloadLogs,
    getCommandBatchLogBundle,
    streamCommandBatchUpdates,
    handleSuccess,
    handleError,
    displayCount,
    onActionStart,
    onActionComplete,
    deviceSelector,
    deviceStatus,
    withCapabilityCheck,
    checkAndShowUnsupportedMinersModal,
    handleConfirmation,
    startBatchOperation,
    deviceIdentifiers,
    selectionMode,
    selectedMiners,
    fetchCoolingMode,
    deleteConfirmationSubtitle,
    startManageSecurity,
    startAuthentication,
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
