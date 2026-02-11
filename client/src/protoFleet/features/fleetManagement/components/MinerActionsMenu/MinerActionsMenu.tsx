import { useMemo } from "react";
import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import BulkActionsWidget, { BulkActionsPopover } from "../BulkActions";
import { performanceActions, settingsActions, SupportedAction } from "./constants";
import CoolingModeModal from "./CoolingModeModal";
import ManagePowerModal from "./ManagePowerModal";
import { useMinerActions } from "./useMinerActions";
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
    showManagePowerModal,
    handleManagePowerConfirm,
    handleManagePowerDismiss,
    showCoolingModeModal,
    coolingModeCount,
    currentCoolingMode,
    handleCoolingModeConfirm,
    handleCoolingModeDismiss,
    unsupportedMinersInfo,
    handleUnsupportedMinersContinue,
    handleUnsupportedMinersDismiss,
  } = useMinerActions({
    selectedMiners: selectedMinersWithStatus,
    selectionMode,
    totalCount,
    onActionStart,
    onActionComplete,
  });

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
      {currentAction === settingsActions.miningPool && (
        <PoolSelectionPageWrapper
          selectedMiners={selectedMinersWithStatus}
          selectionMode={selectionMode}
          poolNeededCount={totalCount}
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
    </PopoverProvider>
  );
};

export default MinerActionsMenu;
