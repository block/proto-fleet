import { useCallback, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import PoolsModalWrapper from "../ActionBar/SettingsWidget/PoolsModal";
import BulkActionsWidget, { BulkActionsPopover } from "../BulkActions";
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
  DeviceListSchema,
  DeviceSelectorSchema,
  StartMiningRequestSchema,
  StartMiningResponse,
  StopMiningRequestSchema,
  StopMiningResponse,
  StreamCommandBatchUpdatesRequestSchema,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import {
  ArrowLeftCompact,
  ChevronDown,
  Curtail,
  Fan,
  LEDIndicator,
  Lock,
  MiningPools,
  Play,
  Power,
  Reboot,
  Speedometer,
  Terminal,
} from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import { PopoverProvider } from "@/shared/components/Popover";
import {
  pushToast,
  STATUSES as TOAST_STATUSES,
  updateToast,
} from "@/shared/features/toaster";

interface MinerActionsMenuProps {
  selectedMiners: string[];
  onActionStart?: () => void;
  onActionComplete?: () => void;
}

const MinerActionsMenu = ({
  selectedMiners,
  onActionStart,
  onActionComplete,
}: MinerActionsMenuProps) => {
  const { startMining, stopMining, blinkLED, streamCommandBatchUpdates } =
    useMinerCommand();

  const [currentAction, setCurrentAction] = useState<SupportedAction | null>(
    null,
  );

  const numberOfMiners = useMemo(() => selectedMiners.length, [selectedMiners]);

  // TODO remove later - only used for downloadLogs
  const simulateAPICall = (callback: () => void) => {
    setTimeout(() => callback && callback(), 2000);
  };

  const handleConfirmation = async () => {
    if (currentAction === null || currentAction === deviceActions.downloadLogs)
      return;

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
          deviceSelector: create(DeviceSelectorSchema, {
            selectionType: {
              case: "includeDevices",
              value: create(DeviceListSchema, {
                deviceIdentifiers: selectedMiners,
              }),
            },
          }),
        });
        stopMining({
          stopMiningRequest: stopMiningRequest,
          onSuccess: (value: StopMiningResponse) =>
            handleSuccess(deviceActions.shutdown, id, value.batchIdentifier),
          onError: handleError.bind(this, id),
        });
        break;
      }
      case deviceActions.wakeUp: {
        const startMiningRequest = create(StartMiningRequestSchema, {
          deviceSelector: create(DeviceSelectorSchema, {
            selectionType: {
              case: "includeDevices",
              value: create(DeviceListSchema, {
                deviceIdentifiers: selectedMiners,
              }),
            },
          }),
        });
        startMining({
          startMiningRequest: startMiningRequest,
          onSuccess: (value: StartMiningResponse) =>
            handleSuccess(deviceActions.wakeUp, id, value.batchIdentifier),
          onError: handleError.bind(this, id),
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
  };

  const handleSuccess = useCallback(
    (
      action: SupportedAction,
      originalToastId: number,
      batchIdentifier: string,
    ) => {
      if (action === deviceActions.downloadLogs) return;

      const streamAbortController = new AbortController();

      let errorToastId: number | null = null;
      let successCount: number;
      let totalCount: number;

      streamCommandBatchUpdates({
        streamRequest: create(StreamCommandBatchUpdatesRequestSchema, {
          batchIdentifier,
        }),
        onStreamData: (response) => {
          totalCount = Number(
            response.status?.commandBatchDeviceCount?.total || 0,
          );
          successCount = Number(
            response.status?.commandBatchDeviceCount?.success || 0,
          );

          updateToast(originalToastId, {
            message: `${successMessages[action]} ${successCount} out of ${totalCount} ${minersMessage}`,
            status: TOAST_STATUSES.success,
          });

          const failureCount = Number(
            response.status?.commandBatchDeviceCount?.failure || 0,
          );
          if (failureCount > 0) {
            if (!errorToastId) {
              errorToastId = pushToast({
                message: `Update failed on ${failureCount} out of ${totalCount} ${minersMessage}`,
                status: TOAST_STATUSES.error,
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
      });
    },
    [streamCommandBatchUpdates],
  );

  const handleError = (originalToastId: number, error: string) => {
    updateToast(originalToastId, {
      message: error,
      status: TOAST_STATUSES.error,
    });
  };

  const handleCancel = () => {
    setCurrentAction(null);
    onActionComplete?.();
  };

  const popoverActions = useMemo(() => {
    // Device actions handlers
    const handleBlinkLEDs = () => {
      setCurrentAction(deviceActions.blinkLEDs);
      const id = pushToast({
        message: loadingMessages[deviceActions.blinkLEDs],
        status: TOAST_STATUSES.loading,
        longRunning: true,
      });

      const blinkLEDRequest = create(BlinkLEDRequestSchema, {
        deviceSelector: create(DeviceSelectorSchema, {
          selectionType: {
            case: "includeDevices",
            value: create(DeviceListSchema, {
              deviceIdentifiers: selectedMiners,
            }),
          },
        }),
      });

      blinkLED({
        blinkLEDRequest,
        onSuccess: (value: BlinkLEDResponse) =>
          handleSuccess(deviceActions.blinkLEDs, id, value.batchIdentifier),
        onError: handleError.bind(this, id),
      });
    };

    const handleDownloadLogs = () => {
      setCurrentAction(deviceActions.downloadLogs);
      const id = pushToast({
        message: "Downloading logs",
        status: TOAST_STATUSES.loading,
        longRunning: true,
      });
      simulateAPICall(() => {
        updateToast(id, {
          message: "Downloaded logs",
          status: TOAST_STATUSES.success,
        });
      });
    };

    const handleFactoryReset = () => {
      setCurrentAction(deviceActions.factoryReset);
      onActionStart?.();
    };

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

    // Performance actions handlers
    const handlePerformanceMode = () => {
      setCurrentAction(performanceActions.performanceMode);
      // TODO modal
    };

    const handleCurtail = () => {
      setCurrentAction(performanceActions.curtail);
      onActionStart?.();
    };

    // Settings actions handlers
    const handleMiningPool = () => {
      setCurrentAction(settingsActions.miningPool);
      onActionStart?.();
    };

    const handleCoolingMode = () => {
      setCurrentAction(settingsActions.coolingMode);
      // TODO show modal
    };

    const handleSecurity = () => {
      setCurrentAction(settingsActions.security);
      // TODO show modal
    };

    return [
      // Device actions
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
      },
      {
        action: deviceActions.factoryReset,
        title: "Factory reset",
        icon: <ArrowLeftCompact />,
        actionHandler: handleFactoryReset,
        requiresConfirmation: true,
        confirmation: {
          title: `Reset ${numberOfMiners} ${numberOfMiners === 1 ? "miner" : "miners"} to factory default?`,
          subtitle: `Resetting ${numberOfMiners === 1 ? "this miner" : "these miners"} will remove all settings and mining pool information. You will not lose any mining rewards.`,
          confirmAction: {
            title: "Reset",
            variant: variants.secondaryDanger,
          },
          testId: "factory-reset-confirm-button",
        },
      },
      {
        action: deviceActions.reboot,
        title: "Reboot",
        icon: <Reboot />,
        actionHandler: handleReboot,
        requiresConfirmation: true,
        confirmation: {
          title: `Reboot ${numberOfMiners} ${numberOfMiners === 1 ? "miner" : "miners"}?`,
          subtitle: `${numberOfMiners === 1 ? "This miner" : "These miners"} will temporarily go offline but will resume hashing automatically after they reboot.`,
          confirmAction: {
            title: "Reboot",
            variant: variants.primary,
          },
          testId: "reboot-confirm-button",
        },
      },
      {
        action: deviceActions.shutdown,
        title: "Sleep",
        icon: <Power />,
        actionHandler: handleShutDown,
        requiresConfirmation: true,
        confirmation: {
          title: `Sleep ${numberOfMiners} ${numberOfMiners === 1 ? "miner" : "miners"}?`,
          subtitle: `${numberOfMiners === 1 ? "This miner" : "These miners"} will go to sleep and stop hashing.`,
          confirmAction: {
            title: "Sleep",
            variant: variants.primary,
          },
          testId: "shutdown-confirm-button",
        },
      },
      {
        action: deviceActions.wakeUp,
        title: "Wake up",
        icon: <Play />,
        actionHandler: handleWakeUp,
        requiresConfirmation: true,
        confirmation: {
          title: `Wake up ${numberOfMiners} ${numberOfMiners === 1 ? "miner" : "miners"}?`,
          subtitle: `${numberOfMiners === 1 ? "This miner" : "These miners"} will wake up and start hashing.`,
          confirmAction: {
            title: "Wake up",
            variant: variants.primary,
          },
          testId: "wake-up-confirm-button",
        },
      },
      // Performance actions
      {
        action: performanceActions.performanceMode,
        title: "Performance mode",
        icon: <Speedometer />,
        actionHandler: handlePerformanceMode,
        requiresConfirmation: false,
      },
      {
        action: performanceActions.curtail,
        title: "Curtail",
        icon: <Curtail />,
        actionHandler: handleCurtail,
        requiresConfirmation: true,
        confirmation: {
          title: `Curtail ${numberOfMiners} miners?`,
          subtitle:
            "These miners will reduce power to 0.1 kW and stop hashing.",
          confirmAction: {
            title: "Curtail",
            variant: variants.primary,
          },
          testId: "curtail-confirm-button",
        },
      },
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
        title: "Security",
        icon: <Lock />,
        actionHandler: handleSecurity,
        requiresConfirmation: false,
      },
    ] as BulkAction<SupportedAction>[];
  }, [blinkLED, handleSuccess, numberOfMiners, onActionStart, selectedMiners]);

  return (
    <PopoverProvider>
      <BulkActionsWidget<SupportedAction>
        buttonIconSuffix={<ChevronDown width={iconSizes.xSmall} />}
        buttonTitle="All actions"
        actions={popoverActions}
        onConfirmation={handleConfirmation}
        onCancel={handleCancel}
        currentAction={currentAction}
        renderPopover={(beforeEach) => (
          <BulkActionsPopover<SupportedAction>
            actions={popoverActions}
            beforeEach={beforeEach}
            testId="actions-menu-popover"
          />
        )}
        testId="actions-menu"
      />
      {currentAction === settingsActions.miningPool && (
        <PoolsModalWrapper
          numberOfMiners={numberOfMiners}
          onDismiss={(_poolsChanged) => {
            setCurrentAction(null);
            onActionComplete?.();
          }}
        />
      )}
    </PopoverProvider>
  );
};

export default MinerActionsMenu;
