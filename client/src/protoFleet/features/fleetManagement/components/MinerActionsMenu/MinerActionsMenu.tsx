import { useMemo } from "react";
import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import BulkActionsWidget, { BulkActionsPopover } from "../BulkActions";
import { performanceActions, settingsActions, SupportedAction } from "./constants";
import CoolingModeModal from "./CoolingModeModal";
import ManagePowerModal from "./ManagePowerModal";
import { ManageSecurityModal, UpdateMinerPasswordModal } from "./ManageSecurity";
import { useMinerActions } from "./useMinerActions";
import type { MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
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
  /** Active UI filter — forwarded for "all" mode delete */
  currentFilter?: MinerListFilter;
  onActionStart?: () => void;
  onActionComplete?: () => void;
}

const MinerActionsMenu = ({
  selectedMiners,
  selectionMode,
  totalCount,
  currentFilter,
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
    handleSecurityModalClose,
  } = useMinerActions({
    selectedMiners: selectedMinersWithStatus,
    selectionMode,
    totalCount,
    currentFilter,
    onActionStart,
    onActionComplete,
  });

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
      <PoolSelectionPageWrapper
        open={showPoolSelectionPage && !!fleetCredentials}
        selectedMiners={poolMiners}
        selectionMode={selectionMode}
        poolNeededCount={poolFilteredDeviceIds ? poolFilteredDeviceIds.length : totalCount}
        userUsername={fleetCredentials?.username}
        userPassword={fleetCredentials?.password}
        onSuccess={handleMiningPoolSuccess}
        onError={handleMiningPoolError}
        onDismiss={handleCancel}
      />
      <ManagePowerModal
        open={currentAction === performanceActions.managePower && showManagePowerModal}
        onConfirm={handleManagePowerConfirm}
        onDismiss={handleManagePowerDismiss}
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
    </PopoverProvider>
  );
};

export default MinerActionsMenu;
