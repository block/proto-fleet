import { useMemo, useState } from "react";
import BulkActionsWidget, { BulkActionsPopover } from "../BulkActions";
import { BulkAction } from "../types";
import { DeviceAction, deviceActions } from "./constants";
import {
  ArrowLeftCompact,
  LEDIndicator,
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
  numberOfMiners: number;
  setHidden: (hidden: boolean) => void;
}

const DeviceWidget = ({ numberOfMiners, setHidden }: DeviceWidgetProps) => {
  const [currentAction, setCurrentAction] = useState<DeviceAction | null>(null);

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
    ] as BulkAction<DeviceAction>[];
  }, [numberOfMiners, setHidden]);

  const loadingMessages = {
    [deviceActions.factoryReset]: "Resetting miners",
    [deviceActions.reboot]: "Rebooting miners",
    [deviceActions.shutdown]: "Shutting down miners",
  };
  const successMessages = {
    [deviceActions.factoryReset]: "Reset miners",
    [deviceActions.reboot]: "Rebooted miners",
    [deviceActions.shutdown]: "Shut down miners",
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
    });
    // TODO call API according to currentAction
    simulateAPICall(() => {
      updateToast(id, {
        message: successMessages[currentAction],
        status: TOAST_STATUSES.success,
      });
    });
    setCurrentAction(null);
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
