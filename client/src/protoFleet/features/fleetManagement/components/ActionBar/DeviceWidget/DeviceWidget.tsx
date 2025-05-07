import { useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import BulkActionsWidget, { BulkActionsPopover } from "../BulkActions";
import { BulkAction } from "../types";
import { DeviceAction, deviceActions } from "./constants";
import {
  StartMiningRequestSchema,
  StopMiningRequestSchema,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import {
  ArrowLeftCompact,
  LEDIndicator,
  Play,
  Power,
  Reboot,
  Rectangle,
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

interface DeviceWidgetProps {
  selectedMiners: string[];
  setHidden: (hidden: boolean) => void;
}

const DeviceWidget = ({ selectedMiners, setHidden }: DeviceWidgetProps) => {
  const { startMining, stopMining } = useMinerCommand();

  const [currentAction, setCurrentAction] = useState<DeviceAction | null>(null);

  const numberOfMiners = useMemo(() => selectedMiners.length, [selectedMiners]);

  // TODO remove later
  const simulateAPICall = (callback: () => void) => {
    setTimeout(() => callback && callback(), 2000);
  };

  const popoverActions = useMemo(() => {
    const handleBlinkLEDs = () => {
      setCurrentAction(deviceActions.blinkLEDs);
      const message = "Blinking LEDs";
      const id = pushToast({
        message: message,
        status: TOAST_STATUSES.loading,
        longRunning: true,
      });
      // TODO call API
      simulateAPICall(() => {
        updateToast(id, {
          message: message,
          status: TOAST_STATUSES.success,
        });
      });
    };

    const handleDownloadLogs = () => {
      setCurrentAction(deviceActions.downloadLogs);
      const id = pushToast({
        message: "Downloading logs",
        status: TOAST_STATUSES.loading,
        longRunning: true,
      });
      // TODO call API
      simulateAPICall(() => {
        updateToast(id, {
          message: "Downloaded logs",
          status: TOAST_STATUSES.success,
        });
      });
    };

    const handleFactoryReset = () => {
      setCurrentAction(deviceActions.factoryReset);
      setHidden(true);
    };

    const handleReboot = () => {
      setCurrentAction(deviceActions.reboot);
      setHidden(true);
    };

    const handleShutDown = () => {
      setCurrentAction(deviceActions.shutdown);
      setHidden(true);
    };

    const handleWakeUp = () => {
      setCurrentAction(deviceActions.wakeUp);
      setHidden(true);
    };

    return [
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
          title: `Reset ${numberOfMiners} miners to factory default?`,
          subtitle:
            "Resetting this miner will remove all settings and mining pool information. You will not lose any mining rewards.",
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
          title: `Reboot ${numberOfMiners} miners?`,
          subtitle:
            "These miners will temporarily go offline but will resume hashing automatically after they reboot.",
          confirmAction: {
            title: "Reboot",
            variant: variants.primary,
          },
          testId: "reboot-confirm-button",
        },
      },
      {
        action: deviceActions.shutdown,
        title: "Shut down",
        icon: <Power className="opacity-30" />,
        actionHandler: handleShutDown,
        requiresConfirmation: true,
        confirmation: {
          title: `Shut down ${numberOfMiners} miners?`,
          subtitle: "These miners will shut down and stop hashing.",
          confirmAction: {
            title: "Shut down",
            variant: variants.primary,
          },
          testId: "shutdown-confirm-button",
        },
      },
      {
        action: deviceActions.wakeUp,
        title: "Wake up",
        icon: <Play className="opacity-30" />,
        actionHandler: handleWakeUp,
        requiresConfirmation: true,
        confirmation: {
          title: `Wake up ${numberOfMiners} miners?`,
          subtitle: "These miners will wake up and start hashing.",
          confirmAction: {
            title: "Wake up",
            variant: variants.primary,
          },
          testId: "wake-up-confirm-button",
        },
      },
    ] as BulkAction<DeviceAction>[];
  }, [numberOfMiners, setHidden]);

  const loadingMessages = {
    [deviceActions.factoryReset]: "Resetting miners",
    [deviceActions.reboot]: "Rebooting miners",
    [deviceActions.shutdown]: "Shutting down miners",
    [deviceActions.wakeUp]: "Waking up miners",
  };
  const successMessages = {
    [deviceActions.factoryReset]: "Reset miners",
    [deviceActions.reboot]: "Rebooted miners",
    [deviceActions.shutdown]: "Shut down miners",
    [deviceActions.wakeUp]: "Woke up miners",
  };
  const handleConfirmation = () => {
    setHidden(false);
    if (
      currentAction === null ||
      currentAction === deviceActions.blinkLEDs ||
      currentAction === deviceActions.downloadLogs
    )
      return;

    const id = pushToast({
      message: loadingMessages[currentAction],
      status: TOAST_STATUSES.loading,
      longRunning: true,
    });

    // TODO call API for rest of the actions
    switch (currentAction) {
      case deviceActions.shutdown: {
        const stopMiningRequest = create(StopMiningRequestSchema, {
          deviceIdentifiers: selectedMiners,
        });
        stopMining({
          stopMiningRequest: stopMiningRequest,
          onSuccess: () => handleSuccess(deviceActions.shutdown, id),
          onError: handleError.bind(this, id),
        });
        break;
      }
      case deviceActions.wakeUp: {
        const startMiningRequest = create(StartMiningRequestSchema, {
          deviceIdentifiers: selectedMiners,
        });
        startMining({
          startMiningRequest: startMiningRequest,
          onSuccess: () => handleSuccess(deviceActions.wakeUp, id),
          onError: handleError.bind(this, id),
        });
        break;
      }
    }
    setCurrentAction(null);
  };

  const handleSuccess = (action: DeviceAction, originalToastId: number) => {
    if (
      action === deviceActions.blinkLEDs ||
      action === deviceActions.downloadLogs
    )
      return;

    updateToast(originalToastId, {
      message: successMessages[action],
      status: TOAST_STATUSES.success,
    });
  };

  const handleError = (originalToastId: number, error: string) => {
    updateToast(originalToastId, {
      message: error,
      status: TOAST_STATUSES.error,
    });
  };

  return (
    <PopoverProvider>
      <BulkActionsWidget<DeviceAction>
        buttonIcon={<Rectangle width={iconSizes.xSmall} />}
        buttonTitle="Device"
        actions={popoverActions}
        onConfirmation={handleConfirmation}
        onCancel={() => setHidden(false)}
        currentAction={currentAction}
        renderPopover={(beforeEach) => (
          <BulkActionsPopover<DeviceAction>
            actions={popoverActions}
            beforeEach={beforeEach}
            testId="device-widget-popover"
          />
        )}
        testId="device-widget"
      />
    </PopoverProvider>
  );
};

export default DeviceWidget;
