import { useMemo, useState } from "react";
import BulkActionsWidget, { BulkActionsPopover } from "../BulkActions";
import { BulkAction } from "../types";
import { PerformanceAction, performanceActions } from "./constants";
import { Curtail, Lightning, Speedometer } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import { PopoverProvider } from "@/shared/components/Popover";
import {
  pushToast,
  STATUSES as TOAST_STATUSES,
  updateToast,
} from "@/shared/features/toaster";

interface PerformanceWidgetProps {
  numberOfMiners: number;
  setHidden: (hidden: boolean) => void;
}

const PerformanceWidget = ({
  numberOfMiners,
  setHidden,
}: PerformanceWidgetProps) => {
  const [currentAction, setCurrentAction] = useState<PerformanceAction | null>(
    null,
  );

  // TODO remove later
  const simulateAPICall = (callback: () => void) => {
    setTimeout(() => callback && callback(), 2000);
  };

  const popoverActions = useMemo(() => {
    const handlePerformanceMode = () => {
      setCurrentAction(performanceActions.performanceMode);
      // TODO modal
    };

    const handleCurtail = () => {
      setCurrentAction(performanceActions.curtail);
      setHidden(true);
    };

    return [
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
    ] as BulkAction<PerformanceAction>[];
  }, [numberOfMiners, setHidden]);

  const loadingMessages = {
    [performanceActions.curtail]: "Curtailing miners",
  };
  const successMessages = {
    [performanceActions.curtail]: "Miners curtailed",
  };
  const handleConfirmation = () => {
    setHidden(false);
    if (currentAction === null) return;

    const id = pushToast({
      message: loadingMessages[currentAction],
      status: TOAST_STATUSES.loading,
      longRunning: true,
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
      <BulkActionsWidget<PerformanceAction>
        buttonIcon={<Lightning width={iconSizes.xSmall} />}
        buttonTitle="Performance"
        actions={popoverActions}
        onConfirmation={handleConfirmation}
        onCancel={() => setHidden(false)}
        currentAction={currentAction}
        renderPopover={(beforeEach) => (
          <BulkActionsPopover<PerformanceAction>
            actions={popoverActions}
            beforeEach={beforeEach}
            testId="performance-widget-popover"
          />
        )}
        testId="performance-widget"
      />
    </PopoverProvider>
  );
};

export default PerformanceWidget;
