import { useMemo, useState } from "react";
import { action } from "storybook/actions";
import { DeviceAction, deviceActions, PerformanceAction, performanceActions } from "../MinerActionsMenu/constants";
import { BulkAction } from "./types";
import { BulkActionsPopover } from ".";
import BulkActionsWidgetComponent from ".";
import { ArrowLeftCompact, Curtail, LEDIndicator, Rectangle } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import { PopoverProvider } from "@/shared/components/Popover";

interface BulkActionsWidgetArgs {
  numberOfActions: number;
  numberOfMiners: number;
}

export const BulkActionsWidget = ({ numberOfActions, numberOfMiners }: BulkActionsWidgetArgs) => {
  const [currentAction, setCurrentAction] = useState<DeviceAction | PerformanceAction | null>(null);

  const handleBlinkLEDs = () => {
    setCurrentAction(deviceActions.blinkLEDs);
    action("Blink LEDs")();
  };

  const handleFactoryReset = () => {
    setCurrentAction(deviceActions.factoryReset);
  };

  const handleCurtail = () => {
    setCurrentAction(performanceActions.curtail);
  };

  const handleConfirmation = () => {
    if (currentAction === deviceActions.factoryReset) {
      action("Factory reset")();
    } else {
      action("Curtail")();
    }
    setCurrentAction(null);
  };

  const popoverActions = useMemo(() => {
    const availableActions = [
      {
        action: deviceActions.blinkLEDs,
        title: "Blink LEDs",
        icon: <LEDIndicator />,
        actionHandler: handleBlinkLEDs,
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
          testId: "factory-reset-button",
        },
      },
      {
        action: performanceActions.curtail,
        title: "Curtail",
        icon: <Curtail />,
        actionHandler: handleCurtail,
        requiresConfirmation: true,
        confirmation: {
          title: `Curtail ${numberOfMiners} miners?`,
          subtitle: "These miners will reduce power to 0.1 kW and stop hashing.",
          confirmAction: {
            title: "Curtail",
            variant: variants.primary,
          },
          testId: "curtail-button",
        },
      },
    ] as BulkAction<DeviceAction | PerformanceAction>[];
    return availableActions.slice(0, numberOfActions);
  }, [numberOfActions, numberOfMiners]);

  return (
    <div className="fixed top-40 left-40 rounded-3xl bg-grayscale-gray-87">
      <PopoverProvider>
        <BulkActionsWidgetComponent<DeviceAction | PerformanceAction>
          buttonIcon={<Rectangle width={iconSizes.xSmall} />}
          buttonTitle="Bulk actions"
          actions={popoverActions}
          onConfirmation={handleConfirmation}
          onCancel={action("Action cancelled")}
          currentAction={currentAction}
          renderPopover={(beforeEach) => (
            <BulkActionsPopover<DeviceAction | PerformanceAction>
              actions={popoverActions}
              beforeEach={beforeEach}
              testId="widget-popover"
            />
          )}
          testId="widget"
        />
      </PopoverProvider>
    </div>
  );
};

export default {
  title: "Proto Fleet/Action Bar/Bulk Actions Widget",
  parameters: {
    docs: {
      source: {
        // Tell storybook to not infer the code from the rendered component because that would cause infinite loop.
        // It is caused by the fact that popover actions are a dynamic array.
        type: "code",
      },
    },
  },
  args: {
    numberOfActions: 1,
    numberOfMiners: 1,
  },
  argTypes: {
    numberOfActions: {
      control: { type: "range", min: 1, max: 3, step: 1 },
    },
    numberOfMiners: {
      control: { type: "range", min: 1, max: 25, step: 1 },
    },
  },
};
