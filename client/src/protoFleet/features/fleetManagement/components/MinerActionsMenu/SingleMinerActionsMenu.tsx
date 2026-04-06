import { Fragment, useCallback, useEffect, useMemo, useState } from "react";
import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import BulkActionConfirmDialog from "../BulkActions/BulkActionConfirmDialog";
import { BulkAction, UnsupportedMinersInfo } from "../BulkActions/types";
import UnsupportedMinersModal from "../BulkActions/UnsupportedMinersModal";
import AddToGroupModal from "./AddToGroupModal";
import { deviceActions, groupActions, performanceActions, settingsActions, SupportedAction } from "./constants";
import CoolingModeModal from "./CoolingModeModal";
import FirmwareUpdateModal from "./FirmwareUpdateModal";
import ManagePowerModal from "./ManagePowerModal";
import { ManageSecurityModal, UpdateMinerPasswordModal } from "./ManageSecurity";
import RenameMinerDialog from "./RenameMinerDialog";
import { type SecurityActionsProps } from "./useManageSecurityFlow";
import { type MinerSelection, useMinerActions } from "./useMinerActions";
import { CoolingMode } from "@/protoFleet/api/generated/common/v1/cooling_pb";
import { PerformanceMode } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { useMinerDeviceStatus } from "@/protoFleet/store/hooks/useFleet";
import { Edit, Ellipsis } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
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
    showFirmwareUpdateModal,
    handleFirmwareUpdateConfirm,
    handleFirmwareUpdateDismiss,
    showCoolingModeModal,
    coolingModeCount,
    currentCoolingMode,
    handleCoolingModeConfirm,
    handleCoolingModeDismiss,
    showAuthenticateFleetModal,
    authenticationPurpose,
    showUpdatePasswordModal,
    hasThirdPartyMiners,
    handleFleetAuthenticated,
    handlePasswordConfirm,
    handlePasswordDismiss,
    handleAuthDismiss,
    unsupportedMinersInfo,
    handleUnsupportedMinersContinue,
    handleUnsupportedMinersDismiss,
    showManageSecurityModal,
    minerGroups,
    handleUpdateGroup,
    handleSecurityModalClose,
    showRenameDialog,
    handleRenameOpen,
    handleRenameConfirm,
    handleRenameDismiss,
    showAddToGroupModal,
    handleAddToGroupDismiss,
  } = useMinerActions({
    selectedMiners,
    // Single-miner actions always target a specific device, never "all devices"
    selectionMode: "subset",
    onActionStart,
    onActionComplete,
  });

  const actionsWithRename = useMemo(() => {
    const renameAction: BulkAction<SupportedAction> = {
      action: settingsActions.rename,
      title: "Rename",
      icon: <Edit />,
      actionHandler: handleRenameOpen,
      requiresConfirmation: false,
    };

    const addToGroupIndex = popoverActions.findIndex((action) => action.action === groupActions.addToGroup);
    if (addToGroupIndex !== -1) {
      return [...popoverActions.slice(0, addToGroupIndex), renameAction, ...popoverActions.slice(addToGroupIndex)];
    }

    const securityIndex = popoverActions.findIndex((action) => action.action === settingsActions.security);
    if (securityIndex !== -1) {
      return [
        ...popoverActions.slice(0, securityIndex),
        {
          ...renameAction,
          showGroupDivider: true,
        },
        ...popoverActions.slice(securityIndex),
      ];
    }

    return [...popoverActions, renameAction];
  }, [handleRenameOpen, popoverActions]);

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
        popoverActions={actionsWithRename}
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
        showFirmwareUpdateModal={showFirmwareUpdateModal}
        handleFirmwareUpdateConfirm={handleFirmwareUpdateConfirm}
        handleFirmwareUpdateDismiss={handleFirmwareUpdateDismiss}
        showCoolingModeModal={showCoolingModeModal}
        coolingModeCount={coolingModeCount}
        currentCoolingMode={currentCoolingMode}
        handleCoolingModeConfirm={handleCoolingModeConfirm}
        handleCoolingModeDismiss={handleCoolingModeDismiss}
        showAuthenticateFleetModal={showAuthenticateFleetModal}
        authenticationPurpose={authenticationPurpose}
        showUpdatePasswordModal={showUpdatePasswordModal}
        hasThirdPartyMiners={hasThirdPartyMiners}
        handleFleetAuthenticated={handleFleetAuthenticated}
        handlePasswordConfirm={handlePasswordConfirm}
        handlePasswordDismiss={handlePasswordDismiss}
        handleAuthDismiss={handleAuthDismiss}
        disabled={disabled}
        unsupportedMinersInfo={unsupportedMinersInfo}
        handleUnsupportedMinersContinue={handleUnsupportedMinersContinueWithReset}
        handleUnsupportedMinersDismiss={handleUnsupportedMinersDismiss}
        showManageSecurityModal={showManageSecurityModal}
        minerGroups={minerGroups}
        handleUpdateGroup={handleUpdateGroup}
        handleSecurityModalClose={handleSecurityModalClose}
        deviceIdentifier={deviceIdentifier}
        showRenameDialog={showRenameDialog}
        handleRenameConfirm={handleRenameConfirm}
        handleRenameDismiss={handleRenameDismiss}
        showAddToGroupModal={showAddToGroupModal}
        handleAddToGroupDismiss={handleAddToGroupDismiss}
      />
    </PopoverProvider>
  );
};

type SingleMinerActionsMenuInnerProps = {
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
  showFirmwareUpdateModal: boolean;
  handleFirmwareUpdateConfirm: (firmwareFileId: string) => void;
  handleFirmwareUpdateDismiss: () => void;
  showCoolingModeModal: boolean;
  coolingModeCount: number;
  currentCoolingMode: CoolingMode | undefined;
  handleCoolingModeConfirm: (coolingMode: CoolingMode) => void;
  handleCoolingModeDismiss: () => void;
  disabled?: boolean;
  unsupportedMinersInfo: UnsupportedMinersInfo;
  handleUnsupportedMinersContinue: () => void;
  handleUnsupportedMinersDismiss: () => void;
  deviceIdentifier: string;
  showRenameDialog: boolean;
  handleRenameConfirm: (name: string) => void;
  handleRenameDismiss: () => void;
  showAddToGroupModal: boolean;
  handleAddToGroupDismiss: () => void;
} & SecurityActionsProps;

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
  showFirmwareUpdateModal,
  handleFirmwareUpdateConfirm,
  handleFirmwareUpdateDismiss,
  showCoolingModeModal,
  coolingModeCount,
  currentCoolingMode,
  handleCoolingModeConfirm,
  handleCoolingModeDismiss,
  showAuthenticateFleetModal,
  authenticationPurpose,
  showUpdatePasswordModal,
  hasThirdPartyMiners,
  handleFleetAuthenticated,
  handlePasswordConfirm,
  handlePasswordDismiss,
  handleAuthDismiss,
  disabled = false,
  unsupportedMinersInfo,
  handleUnsupportedMinersContinue,
  handleUnsupportedMinersDismiss,
  showManageSecurityModal,
  minerGroups,
  handleUpdateGroup,
  handleSecurityModalClose,
  deviceIdentifier,
  showRenameDialog,
  handleRenameConfirm,
  handleRenameDismiss,
  showAddToGroupModal,
  handleAddToGroupDismiss,
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
        className="-my-[10px] !p-[14px]"
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
          className="!space-y-0 !rounded-2xl px-0 pt-2 pb-1"
          position={positions["bottom right"]}
          size={popoverSizes.small}
          offset={8}
          testId="single-miner-actions-popover"
        >
          {popoverActions.map((action) => (
            <Fragment key={action.title}>
              <div className="px-4">
                <Row
                  className="text-emphasis-300"
                  prefixIcon={action.icon}
                  testId={action.action + "-popover-button"}
                  onClick={() => handleAction(action)}
                  compact
                  divider={false}
                >
                  {action.title}
                </Row>
              </div>
              {action.showGroupDivider && <Divider dividerStyle="thick" />}
            </Fragment>
          ))}
        </Popover>
      )}
      <UnsupportedMinersModal
        open={unsupportedMinersInfo.visible}
        unsupportedGroups={unsupportedMinersInfo.unsupportedGroups}
        totalUnsupportedCount={unsupportedMinersInfo.totalUnsupportedCount}
        noneSupported={unsupportedMinersInfo.noneSupported}
        onContinue={handleUnsupportedMinersContinue}
        onDismiss={handleUnsupportedMinersDismiss}
      />
      {popoverActions
        .filter((action) => action.requiresConfirmation)
        .map((action) => {
          if (action.confirmation === undefined) return null;
          const showDialog = currentAction === action.action && showWarnDialog && !unsupportedMinersInfo.visible;
          return (
            <BulkActionConfirmDialog
              key={action.action}
              open={showDialog}
              actionConfirmation={action.confirmation}
              onConfirmation={handleConfirmationClick}
              onCancel={handleCancelClick}
              testId="single-miner-actions-dialog"
            />
          );
        })}
      <PoolSelectionPageWrapper
        open={showPoolSelectionPage && !!fleetCredentials}
        selectedMiners={selectedMiners}
        selectionMode="subset"
        userUsername={fleetCredentials?.username}
        userPassword={fleetCredentials?.password}
        onSuccess={handleMiningPoolSuccess}
        onError={handleMiningPoolError}
        onDismiss={handleCancel}
      />
      <RenameMinerDialog
        key={showRenameDialog ? deviceIdentifier : "closed"}
        open={currentAction === settingsActions.rename && showRenameDialog}
        deviceIdentifier={deviceIdentifier}
        onConfirm={handleRenameConfirm}
        onDismiss={handleRenameDismiss}
      />
      <ManagePowerModal
        open={currentAction === performanceActions.managePower && showManagePowerModal}
        onConfirm={handleManagePowerConfirm}
        onDismiss={handleManagePowerDismiss}
      />
      <FirmwareUpdateModal
        open={currentAction === deviceActions.firmwareUpdate && showFirmwareUpdateModal}
        onConfirm={handleFirmwareUpdateConfirm}
        onDismiss={handleFirmwareUpdateDismiss}
      />
      <CoolingModeModal
        open={currentAction === settingsActions.coolingMode && showCoolingModeModal}
        minerCount={coolingModeCount}
        initialCoolingMode={currentCoolingMode}
        onConfirm={handleCoolingModeConfirm}
        onDismiss={handleCoolingModeDismiss}
      />
      <AuthenticateFleetModal
        open={showAuthenticateFleetModal}
        purpose={authenticationPurpose ?? undefined}
        onAuthenticated={handleFleetAuthenticated}
        onDismiss={handleAuthDismiss}
      />
      <ManageSecurityModal
        open={showManageSecurityModal}
        minerGroups={minerGroups}
        onUpdateGroup={handleUpdateGroup}
        onDismiss={handleSecurityModalClose}
        onDone={handleSecurityModalClose}
      />
      <UpdateMinerPasswordModal
        open={showUpdatePasswordModal}
        hasThirdPartyMiners={hasThirdPartyMiners}
        onConfirm={handlePasswordConfirm}
        onDismiss={handlePasswordDismiss}
      />
      <AddToGroupModal
        open={currentAction === groupActions.addToGroup && showAddToGroupModal}
        onDismiss={handleAddToGroupDismiss}
        selectedMiners={[deviceIdentifier]}
        selectionMode="subset"
        displayCount={1}
      />
    </div>
  );
};

export default SingleMinerActionsMenu;
