import { Key, ReactNode, useCallback, useEffect, useState } from "react";
import { clsx } from "clsx";
import { DeviceAction } from "../DeviceWidget/constants";
import { PerformanceAction } from "../PerformanceWidget/constants";
import { BulkAction } from "../types";
import { SettingsAction } from "@/protoFleet/components/ActionBar/SettingsWidget/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import ButtonGroup, { groupVariants } from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";
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
  const { triggerRef, setIsTriggerFixed } = usePopover();
  useEffect(() => {
    setIsTriggerFixed(true);
  }, [setIsTriggerFixed]);

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
        className={clsx(
          "text-grayscale-white-90!",
          { "bg-grayscale-white-10!": !isOpen },
          { "bg-core-accent-fill!": isOpen },
        )}
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
          const confirmation = action.confirmation!;
          return (
            <Dialog
              key={action.action}
              className="visible"
              title={confirmation.title}
              preventScroll
              titleSize="text-heading-200"
              subtitle={confirmation.subtitle}
              subtitleSize="text-300"
              show={currentAction === action.action && showWarnDialog}
              testId={testId + "-dialog"}
            >
              <ButtonGroup
                className="mt-4"
                variant={groupVariants.stack}
                size={sizes.base}
                buttons={[
                  {
                    text: confirmation.confirmAction.title,
                    onClick: handleConfirmation,
                    variant: confirmation.confirmAction.variant,
                    testId: confirmation.testId,
                  },
                  {
                    text: "Cancel",
                    onClick: handleCancel,
                    variant: variants.secondary,
                    testId: "cancel-button",
                  },
                ]}
              />
            </Dialog>
          );
        })}
    </div>
  );
};

export default BulkActionsWidget;
