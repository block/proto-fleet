import { useMemo } from "react";
import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import BulkActionsWidget, { BulkActionsPopover } from "../BulkActions";
import { performanceActions, settingsActions, SupportedAction } from "./constants";
import CoolingModeModal from "./CoolingModeModal";
import ManagePowerModal from "./ManagePowerModal";
import { ManageSecurityModal, UpdateMinerPasswordModal } from "./ManageSecurity";
import { useMinerActions } from "./useMinerActions";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { ChevronDown } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { type SelectionMode } from "@/shared/components/List";
import { PopoverProvider } from "@/shared/components/Popover";

interface MinerActionsMenuProps {
  selectedMiners: string[];
  selectionMode: SelectionMode;
  /** Total count of all miners in fleet (used for "all" mode confirmation dialogs) */
  totalCount?: number;
  onActionStart?: () => void;
  onActionComplete?: () => void;
}

const MinerActionsMenu = ({
  selectedMiners,
  selectionMode,
  totalCount,
  onActionStart,
  onActionComplete,
}: MinerActionsMenuProps) => {
  const selectedMinersWithStatus = useMemo(
    () => selectedMiners.map((id) => ({ deviceIdentifier: id })),
    [selectedMiners],
  );

  const {
    currentAction,
    popoverActions,
    handleConfirmation,
    handleCancel,
    handleMiningPoolSuccess,
    handleMiningPoolError,
    showPoolSelectionPage,
    poolFilteredDeviceIds,
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
    handleSecurityModalDone,
    handleSecurityModalDismiss,
  } = useMinerActions({
    selectedMiners: selectedMinersWithStatus,
    selectionMode,
    totalCount,
    onActionStart,
    onActionComplete,
  });

  // Use filtered device IDs for pool selection if available
  const poolMiners = useMemo(() => {
    if (poolFilteredDeviceIds) {
      return poolFilteredDeviceIds.map((id) => ({ deviceIdentifier: id }));
    }
    return selectedMinersWithStatus;
  }, [poolFilteredDeviceIds, selectedMinersWithStatus]);

  return (
    <PopoverProvider>
      <BulkActionsWidget<SupportedAction>
        buttonIconSuffix={<ChevronDown width={iconSizes.xSmall} />}
        buttonTitle="All actions"
        actions={popoverActions}
        onConfirmation={handleConfirmation}
        onCancel={handleCancel}
        currentAction={currentAction}
        renderPopover={(beforeEach) => (
          <BulkActionsPopover<SupportedAction>
            actions={popoverActions}
            beforeEach={beforeEach}
            testId="actions-menu-popover"
          />
        )}
        testId="actions-menu"
        unsupportedMinersInfo={unsupportedMinersInfo}
        onUnsupportedMinersContinue={handleUnsupportedMinersContinue}
        onUnsupportedMinersDismiss={handleUnsupportedMinersDismiss}
      />
      {showPoolSelectionPage && fleetCredentials && (
        <PoolSelectionPageWrapper
          selectedMiners={poolMiners}
          selectionMode={selectionMode}
          poolNeededCount={poolFilteredDeviceIds ? poolFilteredDeviceIds.length : totalCount}
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
      {showManageSecurityModal && (
        <ManageSecurityModal
          show={showManageSecurityModal}
          minerGroups={minerGroups}
          onUpdateGroup={handleUpdateGroup}
          onDismiss={handleSecurityModalDismiss}
          onDone={handleSecurityModalDone}
        />
      )}
      {showUpdatePasswordModal && (
        <UpdateMinerPasswordModal
          show={showUpdatePasswordModal}
          hasThirdPartyMiners={hasThirdPartyMiners}
          onConfirm={handlePasswordConfirm}
          onDismiss={handlePasswordDismiss}
        />
      )}
    </PopoverProvider>
  );
};

export default MinerActionsMenu;
