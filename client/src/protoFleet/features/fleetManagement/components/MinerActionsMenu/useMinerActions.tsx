import { useCallback, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import {
  deviceActions,
  loadingMessages,
  minersMessage,
  performanceActions,
  settingsActions,
  successMessages,
  SupportedAction,
} from "./constants";
import { CoolingMode } from "@/protoFleet/api/generated/common/v1/cooling_pb";
import {
  BlinkLEDRequestSchema,
  BlinkLEDResponse,
  CommandType,
  DeviceSelector,
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
  UnpairRequestSchema,
  UnpairResponse,
  UpdateMinerPasswordResponse,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import useBatchTelemetry from "@/protoFleet/api/useBatchTelemetry";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import useMinerCoolingMode from "@/protoFleet/api/useMinerCoolingMode";
import {
  BulkAction,
  type UnsupportedMinersInfo,
} from "@/protoFleet/features/fleetManagement/components/BulkActions/types";
import { minerTypes } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import { hasReachedExpectedStatus } from "@/protoFleet/features/fleetManagement/utils/batchStatusCheck";
import { createDeviceSelector } from "@/protoFleet/features/fleetManagement/utils/deviceSelector";
import {
  useCompleteBatchOperation,
  useFleetStore,
  useRemoveDevicesFromBatch,
  useStartBatchOperation,
} from "@/protoFleet/store";
import {
  // ArrowLeftCompact, // TODO: Uncomment when Factory Reset is implemented
  // Curtail, // TODO: Uncomment when Curtail is implemented
  Fan,
  LEDIndicator,
  Lock,
  MiningPools,
  Play,
  Power,
  Reboot,
  Speedometer,
  // Terminal, // TODO: Uncomment when Download Logs is implemented
  Unpair,
} from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import { type SelectionMode } from "@/shared/components/List";
import { pushToast, STATUSES as TOAST_STATUSES, updateToast } from "@/shared/features/toaster";

export interface MinerSelection {
  deviceIdentifier: string;
  deviceStatus?: DeviceStatus;
}

interface UseMinerActionsParams {
  selectedMiners: MinerSelection[];
  selectionMode: SelectionMode;
  /** Total count of all miners in fleet (used for "all" mode confirmation dialogs) */
  totalCount?: number;
  onActionStart?: () => void;
  onActionComplete?: () => void;
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
  [settingsActions.coolingMode]: { description: "Cooling mode changes", commandType: CommandType.SET_COOLING_MODE },
  [settingsActions.security]: { description: "Password updates", commandType: CommandType.UPDATE_MINER_PASSWORD },
  [performanceActions.managePower]: { description: "Power mode changes", commandType: CommandType.SET_POWER_TARGET },
};

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
  show: false,
  actionDescription: "",
  unsupportedGroups: [],
  totalUnsupportedCount: 0,
  noneSupported: false,
  pendingAction: null,
  supportedDeviceIdentifiers: [],
};

export const useMinerActions = ({
  selectedMiners,
  selectionMode,
  totalCount,
  onActionStart,
  onActionComplete,
}: UseMinerActionsParams) => {
  const {
    startMining,
    stopMining,
    blinkLED,
    unpair,
    reboot,
    streamCommandBatchUpdates,
    setPowerTarget,
    setCoolingMode,
    checkCommandCapabilities,
    updateMinerPassword,
  } = useMinerCommand();

  const startBatchOperation = useStartBatchOperation();
  const completeBatchOperation = useCompleteBatchOperation();
  const removeDevicesFromBatch = useRemoveDevicesFromBatch();
  const { resetFetchedIds } = useBatchTelemetry();
  const { fetchCoolingMode } = useMinerCoolingMode();

  const [currentAction, setCurrentAction] = useState<SupportedAction | null>(null);
  const [showManagePowerModal, setShowManagePowerModal] = useState(false);
  const [filteredSelectorForPowerModal, setFilteredSelectorForPowerModal] = useState<DeviceSelector | undefined>();
  const [showCoolingModeModal, setShowCoolingModeModal] = useState(false);
  const [coolingModeFilteredSelector, setCoolingModeFilteredSelector] = useState<DeviceSelector | undefined>(undefined);
  const [coolingModeFilteredDeviceIds, setCoolingModeFilteredDeviceIds] = useState<string[] | undefined>(undefined);
  const [currentCoolingMode, setCurrentCoolingMode] = useState<CoolingMode | undefined>(undefined);
  const [showAuthenticateFleetModal, setShowAuthenticateFleetModal] = useState(false);
  const [authenticationPurpose, setAuthenticationPurpose] = useState<"security" | "pool" | null>(null);
  const [showUpdatePasswordModal, setShowUpdatePasswordModal] = useState(false);
  const [showPoolSelectionPage, setShowPoolSelectionPage] = useState(false);
  const [securityFilteredSelector, setSecurityFilteredSelector] = useState<DeviceSelector | undefined>(undefined);
  const [securityFilteredDeviceIds, setSecurityFilteredDeviceIds] = useState<string[] | undefined>(undefined);
  const [poolFilteredSelector, setPoolFilteredSelector] = useState<DeviceSelector | undefined>(undefined);
  const [poolFilteredDeviceIds, setPoolFilteredDeviceIds] = useState<string[] | undefined>(undefined);
  const [fleetCredentials, setFleetCredentials] = useState<{ username: string; password: string } | undefined>(
    undefined,
  );
  const [hasThirdPartyMiners, setHasThirdPartyMiners] = useState(false);
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
              show: true,
              actionDescription: metadata.description,
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
    (action: SupportedAction, originalToastId: number, batchIdentifier: string) => {
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

          updateToast(originalToastId, {
            message: `${successMessages[action]} ${successCount} out of ${totalCount} ${minersMessage}`,
            status: TOAST_STATUSES.success,
          });

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
        updateToast(originalToastId, {
          message: `${successMessages[action]} ${successCount} out of ${totalCount} ${minersMessage}`,
          status: TOAST_STATUSES.success,
        });

        // Reset telemetry cache and immediately fetch fresh status for status polling
        resetFetchedIds();

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

            // Rely entirely on telemetry stream for device status updates
            // No need to fetch batch telemetry during polling - stream provides real-time updates

            // Get batch startedAt for time-based checks (e.g., minimum reboot duration)
            const batchOperation = store.fleet.batchOperations.byBatchId[batchIdentifier];
            const batchStartedAt = batchOperation?.startedAt;

            // Check status for all successfully queued devices (from successDeviceIds array)
            // Note: For "select all" operations, only visible/paginated device IDs are stored client-side.
            // This is intentional - telemetry stream updates status for all devices, but we only track
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
          // Complete batch immediately for actions that don't change device state (blink, unpair, etc.)
          completeBatchOperation(batchIdentifier);
        }

        if (action === deviceActions.unpair && successCount > 0) {
          useFleetStore.getState().fleet.refetchMiners?.();
        }
      });
    },
    [streamCommandBatchUpdates, completeBatchOperation, removeDevicesFromBatch, resetFetchedIds],
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

  const handleCoolingModeConfirm = useCallback(
    (coolingMode: CoolingMode) => {
      // Use filtered selector/identifiers if available (from unsupported miners flow),
      // otherwise use the default selector/identifiers for all selected miners.
      // Note: When selectorToUse is "all", deviceIdsToUse only contains visible/paginated device IDs.
      // The server applies the action to all devices, but client-side batch tracking only covers
      // visible devices (see note at line 325 for rationale).
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

  // Fleet authentication handler (shared for security and pool)
  const handleFleetAuthenticated = useCallback(
    (username: string, password: string) => {
      // Store Fleet credentials
      setFleetCredentials({ username, password });
      setShowAuthenticateFleetModal(false);

      // Show the appropriate next modal based on purpose
      if (authenticationPurpose === "security") {
        setShowUpdatePasswordModal(true);
      } else if (authenticationPurpose === "pool") {
        setShowPoolSelectionPage(true);
      }
    },
    [authenticationPurpose],
  );

  const handlePasswordConfirm = useCallback(
    (currentPassword: string, newPassword: string) => {
      const selectorToUse = securityFilteredSelector ?? deviceSelector;
      const deviceIdsToUse = securityFilteredDeviceIds ?? deviceIdentifiers;

      if (!selectorToUse || !fleetCredentials) return;

      setShowUpdatePasswordModal(false);
      setSecurityFilteredSelector(undefined);
      setSecurityFilteredDeviceIds(undefined);
      setFleetCredentials(undefined);

      const id = pushToast({
        message: `${loadingMessages[settingsActions.security]} ${minersMessage}`,
        status: TOAST_STATUSES.loading,
        longRunning: true,
        onClose: () => onActionComplete?.(),
      });

      updateMinerPassword({
        deviceSelector: selectorToUse,
        newPassword,
        currentPassword,
        userUsername: fleetCredentials.username,
        userPassword: fleetCredentials.password,
        onSuccess: (value: UpdateMinerPasswordResponse) => {
          startBatchOperation({
            batchIdentifier: value.batchIdentifier,
            action: settingsActions.security,
            deviceIdentifiers: deviceIdsToUse,
          });
          handleSuccess(settingsActions.security, id, value.batchIdentifier);
        },
        onError: handleError.bind(null, id),
      });

      setCurrentAction(null);
    },
    [
      securityFilteredSelector,
      securityFilteredDeviceIds,
      deviceSelector,
      deviceIdentifiers,
      fleetCredentials,
      updateMinerPassword,
      handleSuccess,
      handleError,
      onActionComplete,
      startBatchOperation,
    ],
  );

  const handlePasswordDismiss = useCallback(() => {
    setShowUpdatePasswordModal(false);
    setSecurityFilteredSelector(undefined);
    setSecurityFilteredDeviceIds(undefined);
    setFleetCredentials(undefined);
    setCurrentAction(null);
    onActionComplete?.();
  }, [onActionComplete]);

  const handleAuthDismiss = useCallback(() => {
    setShowAuthenticateFleetModal(false);
    setAuthenticationPurpose(null);
    setSecurityFilteredSelector(undefined);
    setSecurityFilteredDeviceIds(undefined);
    setPoolFilteredSelector(undefined);
    setPoolFilteredDeviceIds(undefined);
    setFleetCredentials(undefined);
    setCurrentAction(null);
    onActionComplete?.();
  }, [onActionComplete]);

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
        case deviceActions.unpair: {
          const unpairRequest = create(UnpairRequestSchema, {
            deviceSelector: selectorToUse,
          });
          unpair({
            unpairRequest: unpairRequest,
            onSuccess: (value: UnpairResponse) => {
              startBatchOperation({
                batchIdentifier: value.batchIdentifier,
                action: deviceActions.unpair,
                deviceIdentifiers: deviceIdsToUse,
              });
              handleSuccess(deviceActions.unpair, id, value.batchIdentifier);
            },
            onError: handleError.bind(null, id),
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
      startMining,
      stopMining,
      unpair,
      reboot,
      handleSuccess,
      handleError,
      startBatchOperation,
      deviceIdentifiers,
    ],
  );

  const handleCancel = useCallback(() => {
    setCurrentAction(null);
    setShowPoolSelectionPage(false);
    setFleetCredentials(undefined);
    setAuthenticationPurpose(null);
    onActionComplete?.();
  }, [onActionComplete]);

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

    // TODO: Implement Download Logs action
    // const handleDownloadLogs = () => {
    //   setCurrentAction(deviceActions.downloadLogs);
    //   const id = pushToast({
    //     message: "Downloading logs",
    //     status: TOAST_STATUSES.loading,
    //     longRunning: true,
    //   });
    //   simulateAPICall(() => {
    //     updateToast(id, {
    //       message: "Downloaded logs",
    //       status: TOAST_STATUSES.success,
    //     });
    //   });
    // };

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

    const handleUnpair = () => {
      setCurrentAction(deviceActions.unpair);
      onActionStart?.();
    };

    // Performance actions handlers
    const handleManagePower = async () => {
      onActionStart?.();
      const modalShown = await checkAndShowUnsupportedMinersModal(
        performanceActions.managePower,
        (filteredSelector) => {
          setFilteredSelectorForPowerModal(filteredSelector);
          setCurrentAction(performanceActions.managePower);
          setShowManagePowerModal(true);
        },
      );
      if (!modalShown) {
        setFilteredSelectorForPowerModal(undefined);
        setCurrentAction(performanceActions.managePower);
        setShowManagePowerModal(true);
      }
    };

    // TODO: Implement Curtail action
    // const handleCurtail = () => {
    //   setCurrentAction(performanceActions.curtail);
    //   onActionStart?.();
    // };

    // Settings actions handlers
    const handleMiningPool = async () => {
      onActionStart?.();

      const modalShown = await checkAndShowUnsupportedMinersModal(
        settingsActions.miningPool,
        (filteredSelector, filteredDeviceIds) => {
          // Store filtered values for use after authentication
          setPoolFilteredSelector(filteredSelector);
          setPoolFilteredDeviceIds(filteredDeviceIds);
          setCurrentAction(settingsActions.miningPool);
          setAuthenticationPurpose("pool");
          setShowAuthenticateFleetModal(true);
        },
      );
      if (!modalShown) {
        // No filtering needed - clear any stale filtered values
        setPoolFilteredSelector(undefined);
        setPoolFilteredDeviceIds(undefined);
        setCurrentAction(settingsActions.miningPool);
        setAuthenticationPurpose("pool");
        setShowAuthenticateFleetModal(true);
      }
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

      const modalShown = await checkAndShowUnsupportedMinersModal(
        settingsActions.coolingMode,
        (filteredSelector, filteredDeviceIds) => {
          // Store filtered values for use in handleCoolingModeConfirm
          setCoolingModeFilteredSelector(filteredSelector);
          setCoolingModeFilteredDeviceIds(filteredDeviceIds);
          setCurrentAction(settingsActions.coolingMode);
          setShowCoolingModeModal(true);
        },
      );
      if (!modalShown) {
        // No filtering needed - clear any stale filtered values
        setCoolingModeFilteredSelector(undefined);
        setCoolingModeFilteredDeviceIds(undefined);
        setCurrentAction(settingsActions.coolingMode);
        setShowCoolingModeModal(true);
      }
    };

    const handleManageSecurity = async () => {
      onActionStart?.();

      // Check if third-party miners are in the selection
      const miners = useFleetStore.getState().fleet.miners;
      const hasThirdParty = selectedMiners.some((m) => {
        const miner = miners[m.deviceIdentifier];
        return miner?.manufacturer?.toLowerCase() === minerTypes.bitmain;
      });
      setHasThirdPartyMiners(hasThirdParty);

      const modalShown = await checkAndShowUnsupportedMinersModal(
        settingsActions.security,
        (filteredSelector, filteredDeviceIds) => {
          // Store filtered values for use in handlePasswordConfirm
          setSecurityFilteredSelector(filteredSelector);
          setSecurityFilteredDeviceIds(filteredDeviceIds);
          setCurrentAction(settingsActions.security);
          setAuthenticationPurpose("security");
          setShowAuthenticateFleetModal(true);
        },
      );
      if (!modalShown) {
        // No filtering needed - clear any stale filtered values
        setSecurityFilteredSelector(undefined);
        setSecurityFilteredDeviceIds(undefined);
        setCurrentAction(settingsActions.security);
        setAuthenticationPurpose("security");
        setShowAuthenticateFleetModal(true);
      }
    };

    // TODO: Firmware update action - when implemented, add Fleet user authentication requirement
    // similar to handleMiningPool and handleManageSecurity patterns above.
    // Example implementation:
    // const handleFirmwareUpdate = async () => {
    //   onActionStart?.();
    //   const modalShown = await checkAndShowUnsupportedMinersModal(
    //     deviceActions.firmwareUpdate, // Add to constants when implemented
    //     (filteredSelector, filteredDeviceIds) => {
    //       setFirmwareFilteredSelector(filteredSelector);
    //       setFirmwareFilteredDeviceIds(filteredDeviceIds);
    //       setCurrentAction(deviceActions.firmwareUpdate);
    //       setAuthenticationPurpose("firmware"); // Add new purpose type
    //       setShowAuthenticateFleetModal(true);
    //     },
    //   );
    //   if (!modalShown) {
    //     setFirmwareFilteredSelector(undefined);
    //     setFirmwareFilteredDeviceIds(undefined);
    //     setCurrentAction(deviceActions.firmwareUpdate);
    //     setAuthenticationPurpose("firmware");
    //     setShowAuthenticateFleetModal(true);
    //   }
    // };

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
      // Device actions
      {
        action: deviceActions.blinkLEDs,
        title: "Blink LEDs",
        icon: <LEDIndicator />,
        actionHandler: handleBlinkLEDs,
        requiresConfirmation: false,
      },
      // TODO: Implement Download Logs action
      // {
      //   action: deviceActions.downloadLogs,
      //   title: "Download logs",
      //   icon: <Terminal />,
      //   actionHandler: handleDownloadLogs,
      //   requiresConfirmation: false,
      // },
      // TODO: Implement Factory Reset action
      // {
      //   action: deviceActions.factoryReset,
      //   title: "Factory reset",
      //   icon: <ArrowLeftCompact />,
      //   actionHandler: handleFactoryReset,
      //   requiresConfirmation: true,
      //   confirmation: {
      //     title: `Reset ${numberOfMiners} ${numberOfMiners === 1 ? "miner" : "miners"} to factory default?`,
      //     subtitle: `Resetting ${numberOfMiners === 1 ? "this miner" : "these miners"} will remove all settings and mining pool information. You will not lose any mining rewards.`,
      //     confirmAction: {
      //       title: "Reset",
      //       variant: variants.secondaryDanger,
      //     },
      //     testId: "factory-reset-confirm-button",
      //   },
      // },
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
      ...powerStateActions,
      // Performance actions
      {
        action: performanceActions.managePower,
        title: "Manage power",
        icon: <Speedometer />,
        actionHandler: handleManagePower,
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
      // Settings actions
      {
        action: settingsActions.miningPool,
        title: "Edit mining pool",
        icon: <MiningPools />,
        actionHandler: handleMiningPool,
        requiresConfirmation: false,
      },
      {
        action: settingsActions.coolingMode,
        title: "Cooling mode",
        icon: <Fan />,
        actionHandler: handleCoolingMode,
        requiresConfirmation: false,
      },
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
          subtitle: `${displayCount === 1 ? "This miner" : "These miners"} will be removed from your fleet and will stop sending telemetry data. You can re-pair ${displayCount === 1 ? "it" : "them"} later.`,
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
    handleSuccess,
    handleError,
    displayCount,
    onActionStart,
    deviceSelector,
    deviceStatus,
    checkAndShowUnsupportedMinersModal,
    handleConfirmation,
    startBatchOperation,
    deviceIdentifiers,
    selectedMiners,
    fetchCoolingMode,
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
    poolFilteredSelector,
    poolFilteredDeviceIds,
    fleetCredentials,
    showManagePowerModal,
    handleManagePowerConfirm,
    handleManagePowerDismiss,
    showCoolingModeModal,
    coolingModeCount,
    currentCoolingMode,
    handleCoolingModeConfirm,
    handleCoolingModeDismiss,
    showAuthenticateFleetModal,
    showUpdatePasswordModal,
    hasThirdPartyMiners,
    handleFleetAuthenticated,
    handlePasswordConfirm,
    handlePasswordDismiss,
    handleAuthDismiss,
    unsupportedMinersInfo: publicUnsupportedMinersInfo,
    handleUnsupportedMinersContinue,
    handleUnsupportedMinersDismiss,
  };
};
