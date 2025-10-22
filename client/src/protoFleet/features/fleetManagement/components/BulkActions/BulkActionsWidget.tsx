import { Key, ReactNode, useCallback, useEffect, useState } from "react";
import { clsx } from "clsx";
import { BulkAction } from "./types";
import BulkActionConfirmDialog from "@/protoFleet/features/fleetManagement/components/BulkActions/BulkActionConfirmDialog";
import { SupportedAction } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import { usePopover } from "@/shared/components/Popover";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

interface BulkActionsWidgetProps<ActionType> {
  buttonIcon?: ReactNode;
  buttonIconSuffix?: ReactNode;
  buttonTitle: string;
  actions: BulkAction<ActionType>[];
  onConfirmation?: () => void;
  onCancel: () => void;
  currentAction: SupportedAction | null;
  renderPopover: (
    onAction: (requiresConfirmation: boolean) => void,
  ) => ReactNode;
  testId: string;
}

const BulkActionsWidget = <ActionType extends Key>({
  buttonIcon,
  buttonIconSuffix,
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
        className="bg-grayscale-white-10! text-grayscale-white-90!"
        size={sizes.compact}
        variant={variants.secondary}
        prefixIcon={buttonIcon}
        suffixIcon={
          buttonIconSuffix ? (
            <div
              className={clsx("transition-transform duration-200", {
                "rotate-180": isOpen,
              })}
            >
              {buttonIconSuffix}
            </div>
          ) : undefined
        }
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
