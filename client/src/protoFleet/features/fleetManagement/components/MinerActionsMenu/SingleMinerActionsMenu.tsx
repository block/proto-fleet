import { useCallback, useEffect, useMemo, useState } from "react";
import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import BulkActionConfirmDialog from "../BulkActions/BulkActionConfirmDialog";
import { BulkAction, UnsupportedMinersInfo } from "../BulkActions/types";
import UnsupportedMinersModal from "../BulkActions/UnsupportedMinersModal";
import { performanceActions, settingsActions, SupportedAction } from "./constants";
import CoolingModeModal from "./CoolingModeModal";
import ManagePowerModal from "./ManagePowerModal";
import { type MinerSelection, useMinerActions } from "./useMinerActions";
import { CoolingMode } from "@/protoFleet/api/generated/common/v1/cooling_pb";
import { PerformanceMode } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { useMinerDeviceStatus } from "@/protoFleet/store/hooks/useFleet";
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
  disabled?: boolean;
}

const SingleMinerActionsMenu = ({
  deviceIdentifier,
  onActionStart,
  onActionComplete,
  disabled = false,
}: SingleMinerActionsMenuProps) => {
  const deviceStatus = useMinerDeviceStatus(deviceIdentifier);

  const selectedMiners = useMemo(() => [{ deviceIdentifier, deviceStatus }], [deviceIdentifier, deviceStatus]);

  const {
    currentAction,
    popoverActions,
    handleConfirmation,
    handleCancel,
    handleMiningPoolSuccess,
    handleMiningPoolError,
    showPoolSelectionPage,
    fleetCredentials,
    showManagePowerModal,
    handleManagePowerConfirm,
    handleManagePowerDismiss,
    showCoolingModeModal,
    coolingModeCount,
    currentCoolingMode,
    handleCoolingModeConfirm,
    handleCoolingModeDismiss,
    showAuthenticateFleetModal,
    authenticationPurpose,
    handleFleetAuthenticated,
    handleAuthDismiss,
    unsupportedMinersInfo,
    handleUnsupportedMinersContinue,
    handleUnsupportedMinersDismiss,
  } = useMinerActions({
    selectedMiners,
    // Single-miner actions always target a specific device, never "all devices"
    selectionMode: "subset",
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

  // Prevent confirmation dialog flash when continuing from unsupported miners modal
  const handleUnsupportedMinersContinueWithReset = useCallback(() => {
    setShowWarnDialog(false);
    handleUnsupportedMinersContinue();
  }, [handleUnsupportedMinersContinue]);

  return (
    <PopoverProvider>
      <SingleMinerActionsMenuInner
        isOpen={isOpen}
        setIsOpen={setIsOpen}
        showWarnDialog={showWarnDialog}
        currentAction={currentAction}
        popoverActions={popoverActions}
        onClickOutside={onClickOutside}
        handleAction={handleAction}
        handleConfirmationClick={handleConfirmationClick}
        handleCancelClick={handleCancelClick}
        selectedMiners={selectedMiners}
        showPoolSelectionPage={showPoolSelectionPage}
        fleetCredentials={fleetCredentials}
        handleMiningPoolSuccess={handleMiningPoolSuccess}
        handleMiningPoolError={handleMiningPoolError}
        handleCancel={handleCancel}
        showManagePowerModal={showManagePowerModal}
        handleManagePowerConfirm={handleManagePowerConfirm}
        handleManagePowerDismiss={handleManagePowerDismiss}
        showCoolingModeModal={showCoolingModeModal}
        coolingModeCount={coolingModeCount}
        currentCoolingMode={currentCoolingMode}
        handleCoolingModeConfirm={handleCoolingModeConfirm}
        handleCoolingModeDismiss={handleCoolingModeDismiss}
        showAuthenticateFleetModal={showAuthenticateFleetModal}
        authenticationPurpose={authenticationPurpose}
        handleFleetAuthenticated={handleFleetAuthenticated}
        handleAuthDismiss={handleAuthDismiss}
        disabled={disabled}
        unsupportedMinersInfo={unsupportedMinersInfo}
        handleUnsupportedMinersContinue={handleUnsupportedMinersContinueWithReset}
        handleUnsupportedMinersDismiss={handleUnsupportedMinersDismiss}
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
  onClickOutside: () => void;
  handleAction: (action: BulkAction<SupportedAction>) => void;
  handleConfirmationClick: () => void;
  handleCancelClick: () => void;
  selectedMiners: MinerSelection[];
  showPoolSelectionPage: boolean;
  fleetCredentials: { username: string; password: string } | undefined;
  handleMiningPoolSuccess: (batchIdentifier: string) => void;
  handleMiningPoolError: (error: string) => void;
  handleCancel: () => void;
  showManagePowerModal: boolean;
  handleManagePowerConfirm: (performanceMode: PerformanceMode) => void;
  handleManagePowerDismiss: () => void;
  showCoolingModeModal: boolean;
  coolingModeCount: number;
  currentCoolingMode: CoolingMode | undefined;
  handleCoolingModeConfirm: (coolingMode: CoolingMode) => void;
  handleCoolingModeDismiss: () => void;
  showAuthenticateFleetModal: boolean;
  authenticationPurpose: "security" | "pool" | null;
  handleFleetAuthenticated: (username: string, password: string) => void;
  handleAuthDismiss: () => void;
  disabled?: boolean;
  unsupportedMinersInfo: UnsupportedMinersInfo;
  handleUnsupportedMinersContinue: () => void;
  handleUnsupportedMinersDismiss: () => void;
}

const SingleMinerActionsMenuInner = ({
  isOpen,
  setIsOpen,
  showWarnDialog,
  currentAction,
  popoverActions,
  onClickOutside,
  handleAction,
  handleConfirmationClick,
  handleCancelClick,
  selectedMiners,
  showPoolSelectionPage,
  fleetCredentials,
  handleMiningPoolSuccess,
  handleMiningPoolError,
  handleCancel,
  showManagePowerModal,
  handleManagePowerConfirm,
  handleManagePowerDismiss,
  showCoolingModeModal,
  coolingModeCount,
  currentCoolingMode,
  handleCoolingModeConfirm,
  handleCoolingModeDismiss,
  showAuthenticateFleetModal,
  authenticationPurpose,
  handleFleetAuthenticated,
  handleAuthDismiss,
  disabled = false,
  unsupportedMinersInfo,
  handleUnsupportedMinersContinue,
  handleUnsupportedMinersDismiss,
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
          <Ellipsis width={iconSizes.small} className={disabled ? "text-text-primary-30" : "text-text-primary-70"} />
        }
        testId="single-miner-actions-menu-button"
        disabled={disabled}
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
      {/* Unsupported miners modal - shown when the action is not supported */}
      <UnsupportedMinersModal
        {...unsupportedMinersInfo}
        onContinue={handleUnsupportedMinersContinue}
        onDismiss={handleUnsupportedMinersDismiss}
      />
      {/* Confirmation dialog - shown when the action is supported */}
      {popoverActions
        .filter((action) => action.requiresConfirmation)
        .map((action) => {
          if (action.confirmation === undefined) return null;
          const showDialog = currentAction === action.action && showWarnDialog && !unsupportedMinersInfo.show;
          return (
            <BulkActionConfirmDialog
              key={action.action}
              actionConfirmation={action.confirmation}
              show={showDialog}
              onConfirmation={handleConfirmationClick}
              onCancel={handleCancelClick}
              testId="single-miner-actions-dialog"
            />
          );
        })}
      {showPoolSelectionPage && fleetCredentials && (
        <PoolSelectionPageWrapper
          selectedMiners={selectedMiners}
          selectionMode="subset"
          userUsername={fleetCredentials.username}
          userPassword={fleetCredentials.password}
          onSuccess={handleMiningPoolSuccess}
          onError={handleMiningPoolError}
          onDismiss={handleCancel}
        />
      )}
      {currentAction === performanceActions.managePower && (
        <ManagePowerModal
          show={showManagePowerModal}
          onConfirm={handleManagePowerConfirm}
          onDismiss={handleManagePowerDismiss}
        />
      )}
      {currentAction === settingsActions.coolingMode && (
        <CoolingModeModal
          show={showCoolingModeModal}
          minerCount={coolingModeCount}
          initialCoolingMode={currentCoolingMode}
          onConfirm={handleCoolingModeConfirm}
          onDismiss={handleCoolingModeDismiss}
        />
      )}
      {showAuthenticateFleetModal && (
        <AuthenticateFleetModal
          show={showAuthenticateFleetModal}
          purpose={authenticationPurpose ?? undefined}
          onAuthenticated={handleFleetAuthenticated}
          onDismiss={handleAuthDismiss}
        />
      )}
    </div>
  );
};

export default SingleMinerActionsMenu;
