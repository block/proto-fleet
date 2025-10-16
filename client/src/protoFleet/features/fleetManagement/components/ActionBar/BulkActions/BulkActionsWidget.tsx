import { Key, ReactNode, useCallback, useEffect, useState } from "react";
import { clsx } from "clsx";
import { DeviceAction } from "../DeviceWidget/constants";
import { PerformanceAction } from "../PerformanceWidget/constants";
import { BulkAction } from "../types";
import BulkActionConfirmDialog from "@/protoFleet/features/fleetManagement/components/ActionBar/BulkActions/BulkActionConfirmDialog";
import { SettingsAction } from "@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import { usePopover } from "@/shared/components/Popover";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

interface BulkActionsWidgetProps<ActionType> {
  buttonIcon: ReactNode;
  buttonTitle: string;
  actions: BulkAction<ActionType>[];
  onConfirmation?: () => void;
  onCancel: () => void;
  currentAction: DeviceAction | PerformanceAction | SettingsAction | null;
  renderPopover: (
    onAction: (requiresConfirmation: boolean) => void,
  ) => ReactNode;
  testId: string;
}

const BulkActionsWidget = <ActionType extends Key>({
  buttonIcon,
  buttonTitle,
  actions,
  onConfirmation,
  onCancel,
  currentAction,
  renderPopover,
  testId,
}: BulkActionsWidgetProps<ActionType>) => {
  const { triggerRef, setPopoverRenderMode } = usePopover();
  useEffect(() => {
    setPopoverRenderMode("inline");
  }, [setPopoverRenderMode]);

  const [isOpen, setIsOpen] = useState(false);

  const onClickOutside = useCallback(() => {
    setIsOpen(false);
  }, []);

  useClickOutside({ ref: triggerRef, onClickOutside });

  const [showWarnDialog, setShowWarnDialog] = useState(false);

  const handleAction = (requiresConfirmation: boolean) => {
    setIsOpen(false);
    if (requiresConfirmation) setShowWarnDialog(true);
  };

  const handleConfirmation = () => {
    setShowWarnDialog(false);
    onConfirmation && onConfirmation();
  };

  const handleCancel = () => {
    setShowWarnDialog(false);
    onCancel();
  };

  return (
    <div className="relative" ref={triggerRef}>
      <Button
        className={clsx("text-grayscale-white-90!", {
          "bg-grayscale-white-10!": !isOpen,
          "bg-core-accent-fill!": isOpen,
        })}
        size={sizes.compact}
        variant={variants.secondary}
        prefixIcon={buttonIcon}
        testId={testId + "-button"}
        onClick={() => setIsOpen((prev) => !prev)}
      >
        {buttonTitle}
      </Button>
      {isOpen && renderPopover(handleAction)}
      {actions
        .filter((action) => action.requiresConfirmation)
        .map((action) => {
          if (action.confirmation === undefined) return null;
          return (
            <BulkActionConfirmDialog
              key={action.action}
              actionConfirmation={action.confirmation}
              show={currentAction === action.action && showWarnDialog}
              onConfirmation={handleConfirmation}
              onCancel={handleCancel}
              testId={testId + "-dialog"}
            />
          );
        })}
    </div>
  );
};

export default BulkActionsWidget;
