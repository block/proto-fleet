import { useCallback, useEffect, useMemo, useState } from "react";
import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import BulkActionConfirmDialog from "../BulkActions/BulkActionConfirmDialog";
import { BulkAction } from "../BulkActions/types";
import { settingsActions, SupportedAction } from "./constants";
import { useMinerActions } from "./useMinerActions";
import { Ellipsis } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Popover, { popoverSizes } from "@/shared/components/Popover";
import { PopoverProvider, usePopover } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

interface SingleMinerActionsMenuProps {
  deviceIdentifier: string;
  onActionStart?: () => void;
  onActionComplete?: () => void;
}

const SingleMinerActionsMenu = ({
  deviceIdentifier,
  onActionStart,
  onActionComplete,
}: SingleMinerActionsMenuProps) => {
  const selectedMiners = useMemo(() => [deviceIdentifier], [deviceIdentifier]);

  const {
    currentAction,
    setCurrentAction,
    popoverActions,
    handleConfirmation,
    handleCancel,
    numberOfMiners,
    handleMiningPoolSuccess,
    handleMiningPoolError,
  } = useMinerActions({
    selectedMiners,
    onActionStart,
    onActionComplete,
  });

  const [isOpen, setIsOpen] = useState(false);
  const [showWarnDialog, setShowWarnDialog] = useState(false);

  const onClickOutside = useCallback(() => {
    setIsOpen(false);
  }, []);

  const handleAction = (action: BulkAction<SupportedAction>) => {
    setIsOpen(false);
    if (action.requiresConfirmation) {
      setShowWarnDialog(true);
    }
    action.actionHandler();
  };

  const handleConfirmationClick = () => {
    setShowWarnDialog(false);
    handleConfirmation();
  };

  const handleCancelClick = () => {
    setShowWarnDialog(false);
    handleCancel();
  };

  return (
    <PopoverProvider>
      <SingleMinerActionsMenuInner
        isOpen={isOpen}
        setIsOpen={setIsOpen}
        showWarnDialog={showWarnDialog}
        currentAction={currentAction}
        popoverActions={popoverActions}
        numberOfMiners={numberOfMiners}
        onClickOutside={onClickOutside}
        handleAction={handleAction}
        handleConfirmationClick={handleConfirmationClick}
        handleCancelClick={handleCancelClick}
        setCurrentAction={setCurrentAction}
        onActionComplete={onActionComplete}
        selectedMiners={selectedMiners}
        handleMiningPoolSuccess={handleMiningPoolSuccess}
        handleMiningPoolError={handleMiningPoolError}
        handleCancel={handleCancel}
      />
    </PopoverProvider>
  );
};

interface SingleMinerActionsMenuInnerProps {
  isOpen: boolean;
  setIsOpen: (value: boolean | ((prev: boolean) => boolean)) => void;
  showWarnDialog: boolean;
  currentAction: SupportedAction | null;
  popoverActions: BulkAction<SupportedAction>[];
  numberOfMiners: number;
  onClickOutside: () => void;
  handleAction: (action: BulkAction<SupportedAction>) => void;
  handleConfirmationClick: () => void;
  handleCancelClick: () => void;
  setCurrentAction: (action: SupportedAction | null) => void;
  onActionComplete?: () => void;
  selectedMiners: string[];
  handleMiningPoolSuccess: (batchIdentifier: string) => void;
  handleMiningPoolError: (error: string) => void;
  handleCancel: () => void;
}

const SingleMinerActionsMenuInner = ({
  isOpen,
  setIsOpen,
  showWarnDialog,
  currentAction,
  popoverActions,
  numberOfMiners: _numberOfMiners,
  onClickOutside,
  handleAction,
  handleConfirmationClick,
  handleCancelClick,
  setCurrentAction: _setCurrentAction,
  onActionComplete: _onActionComplete,
  selectedMiners,
  handleMiningPoolSuccess,
  handleMiningPoolError,
  handleCancel,
}: SingleMinerActionsMenuInnerProps) => {
  const { triggerRef, setPopoverRenderMode } = usePopover();

  useEffect(() => {
    setPopoverRenderMode("portal-fixed");
  }, [setPopoverRenderMode]);

  useClickOutside({
    ref: triggerRef,
    onClickOutside,
    ignoreSelectors: [".popover-content"],
  });

  return (
    <div className="relative" ref={triggerRef}>
      <Button
        size={sizes.compact}
        variant={variants.textOnly}
        prefixIcon={
          <Ellipsis width={iconSizes.small} className="text-text-primary-70" />
        }
        testId="single-miner-actions-menu-button"
        onClick={(e) => {
          e.stopPropagation();
          setIsOpen((prev) => !prev);
        }}
      />
      {isOpen && (
        <Popover
          className="!space-y-0 px-4 pt-2 pb-1"
          position={positions["bottom right"]}
          size={popoverSizes.medium}
          offset={8}
          testId="single-miner-actions-popover"
        >
          {popoverActions.map((action) => (
            <Row
              key={action.title}
              className="text-emphasis-300"
              prefixIcon={action.icon}
              testId={action.action + "-popover-button"}
              onClick={() => handleAction(action)}
              compact
              divider
            >
              {action.title}
            </Row>
          ))}
        </Popover>
      )}
      {popoverActions
        .filter((action) => action.requiresConfirmation)
        .map((action) => {
          if (action.confirmation === undefined) return null;
          return (
            <BulkActionConfirmDialog
              key={action.action}
              actionConfirmation={action.confirmation}
              show={currentAction === action.action && showWarnDialog}
              onConfirmation={handleConfirmationClick}
              onCancel={handleCancelClick}
              testId="single-miner-actions-dialog"
            />
          );
        })}
      {currentAction === settingsActions.miningPool && (
        <PoolSelectionPageWrapper
          deviceIdentifiers={selectedMiners}
          onSuccess={handleMiningPoolSuccess}
          onError={handleMiningPoolError}
          onDismiss={handleCancel}
        />
      )}
    </div>
  );
};

export default SingleMinerActionsMenu;
