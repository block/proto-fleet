import { useMemo, useState } from "react";
import BulkActionsWidget, { BulkActionsPopover } from "../BulkActions";
import { BulkAction } from "../types";
import { SettingsAction, settingsActions } from "./constants";
import PoolsModalWrapper from "./PoolsModal";
import { Download, Fan, Lock, Settings } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { PopoverProvider } from "@/shared/components/Popover";

interface SettingsWidgetProps {
  numberOfMiners: number;
  setHidden: (hidden: boolean) => void;
}

const SettingsWidget = ({ numberOfMiners, setHidden }: SettingsWidgetProps) => {
  const [currentAction, setCurrentAction] = useState<SettingsAction | null>(
    null,
  );

  const popoverActions = useMemo(() => {
    const handleMiningPool = () => {
      setCurrentAction(settingsActions.miningPool);
      setHidden(true);
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
      {
        action: settingsActions.miningPool,
        title: "Mining pool",
        icon: <Download />,
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
    ] as BulkAction<SettingsAction>[];
  }, [setHidden]);

  const handleMiningPoolsUpdate = (poolsChanged: boolean) => {
    setCurrentAction(null);
    setHidden(false);
    if (poolsChanged) {
      // TODO handle login
      // TODO call API
    }
  };

  return (
    <>
      <PopoverProvider>
        <BulkActionsWidget<SettingsAction>
          buttonIcon={<Settings width={iconSizes.xSmall} />}
          buttonTitle="Settings"
          actions={popoverActions}
          onCancel={() => setHidden(false)}
          currentAction={currentAction}
          renderPopover={(beforeEach) => (
            <BulkActionsPopover<SettingsAction>
              actions={popoverActions}
              beforeEach={beforeEach}
              testId="settings-widget-popover"
            />
          )}
          testId="settings-widget"
        />
      </PopoverProvider>
      {currentAction === settingsActions.miningPool && (
        <PoolsModalWrapper
          numberOfMiners={numberOfMiners}
          onDismiss={handleMiningPoolsUpdate}
        />
      )}
    </>
  );
};

export default SettingsWidget;
