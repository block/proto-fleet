import { useMemo, useState } from "react";
import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import BulkActionsWidget, { BulkActionsPopover } from "../BulkActions";
import { type BulkAction } from "../BulkActions/types";
import AddToGroupModal from "./AddToGroupModal";
import BulkRenameModal from "./BulkRenameModal";
import { deviceActions, groupActions, performanceActions, settingsActions, SupportedAction } from "./constants";
import CoolingModeModal from "./CoolingModeModal";
import FirmwareUpdateModal from "./FirmwareUpdateModal";
import ManagePowerModal from "./ManagePowerModal";
import { ManageSecurityModal, UpdateMinerPasswordModal } from "./ManageSecurity";
import { useMinerActions } from "./useMinerActions";
import type { SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import type { MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { ChevronDown, Edit } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import { type SelectionMode } from "@/shared/components/List";
import { PopoverProvider } from "@/shared/components/Popover";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface MinerActionsMenuProps {
  selectedMiners: string[];
  selectionMode: SelectionMode;
  /** Total count of all miners in fleet (used for "all" mode confirmation dialogs) */
  totalCount?: number;
  /** Active UI filter — forwarded for "all" mode delete */
  currentFilter?: MinerListFilter;
  /** Active UI sort — forwarded so bulk actions can match visible table order. */
  currentSort?: SortConfig;
  onActionStart?: () => void;
  onActionComplete?: () => void;
}

const MinerActionsMenu = ({
  selectedMiners,
  selectionMode,
  totalCount,
  currentFilter,
  currentSort,
  onActionStart,
  onActionComplete,
}: MinerActionsMenuProps) => {
  const [showBulkRenameModal, setShowBulkRenameModal] = useState(false);
  const { isPhone, isTablet } = useWindowDimensions();
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
    showAddToGroupModal,
    handleAddToGroupDismiss,
    displayCount,
  } = useMinerActions({
    selectedMiners: selectedMinersWithStatus,
    selectionMode,
    totalCount,
    currentFilter,
    onActionStart,
    onActionComplete,
  });

  const actionsWithBulkRename = useMemo(() => {
    const renameAction: BulkAction<SupportedAction> = {
      action: settingsActions.rename,
      title: "Rename",
      icon: <Edit />,
      actionHandler: () => {
        setShowBulkRenameModal(true);
        onActionStart?.();
      },
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
  }, [onActionStart, popoverActions]);

  const poolMiners = useMemo(() => {
    if (poolFilteredDeviceIds) {
      return poolFilteredDeviceIds.map((id) => ({ deviceIdentifier: id }));
    }
    return selectedMinersWithStatus;
  }, [poolFilteredDeviceIds, selectedMinersWithStatus]);

  const showQuickActions = !isPhone && !isTablet;
  const quickActions = useMemo(() => {
    const quickActionOrder: SupportedAction[] = [
      deviceActions.blinkLEDs,
      deviceActions.reboot,
      performanceActions.managePower,
    ];
    const actionMap = new Map(actionsWithBulkRename.map((action) => [action.action, action]));

    return quickActionOrder.flatMap((actionKey) => {
      const action = actionMap.get(actionKey);
      return action ? [action] : [];
    });
  }, [actionsWithBulkRename]);

  return (
    <PopoverProvider>
      <div className="flex flex-wrap justify-start gap-3">
        <BulkActionsWidget<SupportedAction>
          buttonIconSuffix={<ChevronDown width={iconSizes.xSmall} />}
          buttonTitle={showQuickActions ? "More" : "Actions"}
          actions={actionsWithBulkRename}
          onConfirmation={handleConfirmation}
          onCancel={handleCancel}
          currentAction={currentAction}
          renderQuickActions={(onAction) =>
            showQuickActions
              ? quickActions.map((action) => (
                  <Button
                    key={action.action}
                    className="bg-grayscale-white-10! text-grayscale-white-90!"
                    size={sizes.compact}
                    variant={variants.secondary}
                    testId={`actions-menu-quick-action-${action.action}`}
                    onClick={() => onAction(action)}
                  >
                    {action.title}
                  </Button>
                ))
              : null
          }
          renderPopover={(beforeEach) => (
            <BulkActionsPopover<SupportedAction>
              actions={actionsWithBulkRename}
              beforeEach={beforeEach}
              testId="actions-menu-popover"
            />
          )}
          testId="actions-menu"
          unsupportedMinersInfo={unsupportedMinersInfo}
          onUnsupportedMinersContinue={handleUnsupportedMinersContinue}
          onUnsupportedMinersDismiss={handleUnsupportedMinersDismiss}
        />
      </div>
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
        selectedMiners={selectedMiners}
        selectionMode={selectionMode}
        displayCount={displayCount ?? selectedMiners.length}
      />
      <BulkRenameModal
        open={showBulkRenameModal}
        selectedMinerIds={selectedMiners}
        selectionMode={selectionMode}
        totalCount={totalCount}
        currentFilter={currentFilter}
        currentSort={currentSort}
        onDismiss={() => {
          setShowBulkRenameModal(false);
          onActionComplete?.();
        }}
      />
    </PopoverProvider>
  );
};

export default MinerActionsMenu;
