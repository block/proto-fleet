import { Key, ReactNode, useCallback, useEffect, useState } from "react";
import { clsx } from "clsx";
import { BulkAction, UnsupportedMinersInfo } from "./types";
import UnsupportedMinersModal from "./UnsupportedMinersModal";
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
  renderPopover: (onAction: (requiresConfirmation: boolean) => void) => ReactNode;
  testId: string;
  unsupportedMinersInfo?: UnsupportedMinersInfo;
  onUnsupportedMinersContinue?: () => void;
  onUnsupportedMinersDismiss?: () => void;
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
  unsupportedMinersInfo,
  onUnsupportedMinersContinue,
  onUnsupportedMinersDismiss,
}: BulkActionsWidgetProps<ActionType>) => {
  const { triggerRef, setPopoverRenderMode } = usePopover();
  useEffect(() => {
    setPopoverRenderMode("inline");
  }, [setPopoverRenderMode]);

  const [isOpen, setIsOpen] = useState(false);

  const onClickOutside = useCallback(() => {
    setIsOpen(false);
  }, []);

  useClickOutside({
    ref: triggerRef,
    onClickOutside,
    ignoreSelectors: [".popover-content"],
  });

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

  // Prevent confirmation dialog flash when continuing from unsupported miners modal
  const handleUnsupportedMinersContinue = useCallback(() => {
    setShowWarnDialog(false);
    onUnsupportedMinersContinue?.();
  }, [onUnsupportedMinersContinue]);

  const showUnsupportedMinersModal = unsupportedMinersInfo && onUnsupportedMinersContinue && onUnsupportedMinersDismiss;

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
      {/* Unsupported miners modal - shown when some or all miners don't support the action */}
      {showUnsupportedMinersModal && (
        <UnsupportedMinersModal
          {...unsupportedMinersInfo}
          onContinue={handleUnsupportedMinersContinue}
          onDismiss={onUnsupportedMinersDismiss}
        />
      )}
      {/* Confirmation dialog - shown when all miners support the action */}
      {actions
        .filter((action) => action.requiresConfirmation)
        .map((action) => {
          if (action.confirmation === undefined) return null;
          const showDialog = currentAction === action.action && showWarnDialog && !unsupportedMinersInfo?.show;
          return (
            <BulkActionConfirmDialog
              key={action.action}
              actionConfirmation={action.confirmation}
              show={showDialog}
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
