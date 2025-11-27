import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import BulkActionsWidget, { BulkActionsPopover } from "../BulkActions";
import { settingsActions, SupportedAction } from "./constants";
import { useMinerActions } from "./useMinerActions";
import { ChevronDown } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { PopoverProvider } from "@/shared/components/Popover";

interface MinerActionsMenuProps {
  selectedMiners: string[];
  onActionStart?: () => void;
  onActionComplete?: () => void;
}

const MinerActionsMenu = ({ selectedMiners, onActionStart, onActionComplete }: MinerActionsMenuProps) => {
  const {
    currentAction,
    popoverActions,
    handleConfirmation,
    handleCancel,
    handleMiningPoolSuccess,
    handleMiningPoolError,
  } = useMinerActions({
    selectedMiners,
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
      />
      {currentAction === settingsActions.miningPool && (
        <PoolSelectionPageWrapper
          deviceIdentifiers={selectedMiners}
          onSuccess={handleMiningPoolSuccess}
          onError={handleMiningPoolError}
          onDismiss={handleCancel}
        />
      )}
    </PopoverProvider>
  );
};

export default MinerActionsMenu;
