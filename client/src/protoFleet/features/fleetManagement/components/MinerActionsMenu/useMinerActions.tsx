import { useCallback, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { BulkAction } from "../BulkActions/types";
import {
  deviceActions,
  loadingMessages,
  minersMessage,
  performanceActions,
  settingsActions,
  successMessages,
  SupportedAction,
} from "./constants";
import {
  BlinkLEDRequestSchema,
  BlinkLEDResponse,
  PerformanceMode,
  SetPowerTargetResponse,
  StartMiningRequestSchema,
  StartMiningResponse,
  StopMiningRequestSchema,
  StopMiningResponse,
  StreamCommandBatchUpdatesRequestSchema,
  UnpairRequestSchema,
  UnpairResponse,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import { createDeviceSelector } from "@/protoFleet/features/fleetManagement/utils/deviceSelector";
import { useFleetStore } from "@/protoFleet/store";
import {
  // ArrowLeftCompact, // TODO: Uncomment when Factory Reset is implemented
  // Curtail, // TODO: Uncomment when Curtail is implemented
  // Fan, // TODO: Uncomment when Cooling Mode is implemented
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
import { pushToast, removeToast, STATUSES as TOAST_STATUSES, updateToast } from "@/shared/features/toaster";

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

export const useMinerActions = ({
  selectedMiners,
  selectionMode,
  totalCount,
  onActionStart,
  onActionComplete,
}: UseMinerActionsParams) => {
  const { startMining, stopMining, blinkLED, unpair, streamCommandBatchUpdates, setPowerTarget } = useMinerCommand();

  const [currentAction, setCurrentAction] = useState<SupportedAction | null>(null);
  const [showManagePowerModal, setShowManagePowerModal] = useState(false);
  const miningPoolToastIdRef = useRef<number | null>(null);

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

  const handleSuccess = useCallback(
    (action: SupportedAction, originalToastId: number, batchIdentifier: string) => {
      const streamAbortController = new AbortController();

      let errorToastId: number | null = null;
      let successCount = 0;
      let totalCount = 0;

      streamCommandBatchUpdates({
        streamRequest: create(StreamCommandBatchUpdatesRequestSchema, {
          batchIdentifier,
        }),
        onStreamData: (response) => {
          totalCount = Number(response.status?.commandBatchDeviceCount?.total || 0);
          successCount = Number(response.status?.commandBatchDeviceCount?.success || 0);

          updateToast(originalToastId, {
            message: `${successMessages[action]} ${successCount} out of ${totalCount} ${minersMessage}`,
            status: TOAST_STATUSES.success,
          });

          const failureCount = Number(response.status?.commandBatchDeviceCount?.failure || 0);
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
        },
        streamAbortController: streamAbortController,
      }).finally(() => {
        updateToast(originalToastId, {
          message: `${successMessages[action]} ${successCount} out of ${totalCount} ${minersMessage}`,
          status: TOAST_STATUSES.success,
        });

        if (action === deviceActions.unpair && successCount > 0) {
          useFleetStore.getState().fleet.refetchMiners?.();
        }
      });
    },
    [streamCommandBatchUpdates],
  );

  const handleError = useCallback((originalToastId: number, error: string) => {
    updateToast(originalToastId, {
      message: error,
      status: TOAST_STATUSES.error,
    });
  }, []);

  const handleMiningPoolSuccess = useCallback(
    (batchIdentifier: string) => {
      if (miningPoolToastIdRef.current !== null) {
        handleSuccess(settingsActions.miningPool, miningPoolToastIdRef.current, batchIdentifier);
      }
    },
    [handleSuccess],
  );

  const handleMiningPoolError = useCallback(
    (error: string) => {
      if (miningPoolToastIdRef.current !== null) {
        handleError(miningPoolToastIdRef.current, error);
        // Clear ref so handleCancel doesn't remove the error toast
        miningPoolToastIdRef.current = null;
      }
      // Dismiss the component but keep the error toast visible
      setCurrentAction(null);
      onActionComplete?.();
    },
    [handleError, onActionComplete],
  );

  const handleManagePowerConfirm = useCallback(
    (performanceMode: PerformanceMode) => {
      if (!deviceSelector) return;
      setShowManagePowerModal(false);

      const id = pushToast({
        message: `${loadingMessages[performanceActions.managePower]} ${minersMessage}`,
        status: TOAST_STATUSES.loading,
        longRunning: true,
        onClose: () => onActionComplete?.(),
      });

      setPowerTarget({
        deviceSelector,
        performanceMode,
        onSuccess: (value: SetPowerTargetResponse) =>
          handleSuccess(performanceActions.managePower, id, value.batchIdentifier),
        onError: handleError.bind(null, id),
      });

      setCurrentAction(null);
    },
    [deviceSelector, setPowerTarget, handleSuccess, handleError, onActionComplete],
  );

  const handleManagePowerDismiss = useCallback(() => {
    setShowManagePowerModal(false);
    setCurrentAction(null);
    onActionComplete?.();
  }, [onActionComplete]);

  const handleConfirmation = useCallback(async () => {
    if (currentAction === null || !deviceSelector) return;

    const id = pushToast({
      message: `${loadingMessages[currentAction]} ${minersMessage}`,
      status: TOAST_STATUSES.loading,
      longRunning: true,
      onClose: () => onActionComplete?.(),
    });

    // Handle device action API calls
    switch (currentAction) {
      case deviceActions.shutdown: {
        const stopMiningRequest = create(StopMiningRequestSchema, {
          deviceSelector,
        });
        stopMining({
          stopMiningRequest: stopMiningRequest,
          onSuccess: (value: StopMiningResponse) => handleSuccess(deviceActions.shutdown, id, value.batchIdentifier),
          onError: handleError.bind(null, id),
        });
        break;
      }
      case deviceActions.wakeUp: {
        const startMiningRequest = create(StartMiningRequestSchema, {
          deviceSelector,
        });
        startMining({
          startMiningRequest: startMiningRequest,
          onSuccess: (value: StartMiningResponse) => handleSuccess(deviceActions.wakeUp, id, value.batchIdentifier),
          onError: handleError.bind(null, id),
        });
        break;
      }
      case deviceActions.unpair: {
        const unpairRequest = create(UnpairRequestSchema, {
          deviceSelector,
        });
        unpair({
          unpairRequest: unpairRequest,
          onSuccess: (value: UnpairResponse) => handleSuccess(deviceActions.unpair, id, value.batchIdentifier),
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
  }, [currentAction, onActionComplete, deviceSelector, startMining, stopMining, unpair, handleSuccess, handleError]);

  const handleCancel = useCallback(() => {
    if (miningPoolToastIdRef.current !== null) {
      removeToast(miningPoolToastIdRef.current);
      miningPoolToastIdRef.current = null;
    }

    setCurrentAction(null);
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
        onSuccess: (value: BlinkLEDResponse) => handleSuccess(deviceActions.blinkLEDs, id, value.batchIdentifier),
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

    const handleReboot = () => {
      setCurrentAction(deviceActions.reboot);
      onActionStart?.();
    };

    const handleShutDown = () => {
      setCurrentAction(deviceActions.shutdown);
      onActionStart?.();
    };

    const handleWakeUp = () => {
      setCurrentAction(deviceActions.wakeUp);
      onActionStart?.();
    };

    const handleUnpair = () => {
      setCurrentAction(deviceActions.unpair);
      onActionStart?.();
    };

    // Performance actions handlers
    const handleManagePower = () => {
      setCurrentAction(performanceActions.managePower);
      setShowManagePowerModal(true);
      onActionStart?.();
    };

    // TODO: Implement Curtail action
    // const handleCurtail = () => {
    //   setCurrentAction(performanceActions.curtail);
    //   onActionStart?.();
    // };

    // Settings actions handlers
    const handleMiningPool = () => {
      setCurrentAction(settingsActions.miningPool);
      onActionStart?.();

      miningPoolToastIdRef.current = pushToast({
        message: `${loadingMessages[settingsActions.miningPool]} ${minersMessage}`,
        status: TOAST_STATUSES.loading,
        longRunning: true,
        onClose: () => onActionComplete?.(),
      });
    };

    // TODO: Implement Cooling Mode action
    // const handleCoolingMode = () => {
    //   setCurrentAction(settingsActions.coolingMode);
    //   // TODO show modal
    // };

    const handleSecurity = () => {
      setCurrentAction(settingsActions.security);
      // TODO show modal
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
      // TODO: Implement Cooling Mode action
      // {
      //   action: settingsActions.coolingMode,
      //   title: "Cooling mode",
      //   icon: <Fan />,
      //   actionHandler: handleCoolingMode,
      //   requiresConfirmation: false,
      // },
      {
        action: settingsActions.security,
        title: "Security",
        icon: <Lock />,
        actionHandler: handleSecurity,
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
    onActionComplete,
    deviceSelector,
    deviceStatus,
  ]);

  return {
    currentAction,
    setCurrentAction,
    popoverActions,
    handleConfirmation,
    handleCancel,
    numberOfMiners,
    handleMiningPoolSuccess,
    handleMiningPoolError,
    showManagePowerModal,
    handleManagePowerConfirm,
    handleManagePowerDismiss,
  };
};
