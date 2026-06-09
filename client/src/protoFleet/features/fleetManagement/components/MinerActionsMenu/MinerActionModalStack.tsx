import { useCallback } from "react";

import { deviceActions, groupActions, performanceActions, settingsActions } from "./constants";
import CoolingModeModal from "./CoolingModeModal";
import FirmwareUpdateModal from "./FirmwareUpdateModal";
import ManagePowerModal from "./ManagePowerModal";
import { ManageSecurityModal, UpdateMinerPasswordModal } from "./ManageSecurity";
import { type useMinerActions } from "./useMinerActions";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import ParentPickerModal from "@/protoFleet/components/ParentPickerModal";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { type SelectionMode } from "@/shared/components/List";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type MinerActions = ReturnType<typeof useMinerActions>;

interface MinerActionModalStackProps {
  minerActions: MinerActions;
  selectedMinerIds: string[];
  selectionMode: SelectionMode;
  // Falls back to selectedMinerIds.length when omitted. MinerActionsMenu
  // passes through to the bulk display count which can differ from the
  // local subset (e.g. "all" mode).
  displayCount?: number;
  // Pre-handler hook fired before each modal's dismiss / confirm
  // delegates to minerActions. FleetGroupActionsMenu uses this to clear
  // its pendingAction state so a re-fired action can run again.
  onActionBoundary?: () => void;
}

// Six action-driven modals shared by MinerActionsMenu, SingleMinerActionsMenu,
// and FleetGroupActionsMenu. State + handlers all come from useMinerActions
// — callers wire the hook into whatever shell they own and forward it in.
//
// Excluded by design: PoolSelectionPageWrapper (selectedMiners + count
// vary per callsite), BulkActionConfirmDialog + UnsupportedMinersModal
// (gating differs across menus), the second AuthenticateFleetModal used
// only for workerNames in MinerActionsMenu, and miner-list-specific
// modals (BulkRename, BulkWorkerName, RenameMinerDialog,
// UpdateWorkerNameDialog).
const MinerActionModalStack = ({
  minerActions,
  selectedMinerIds,
  selectionMode,
  displayCount,
  onActionBoundary,
}: MinerActionModalStackProps) => {
  const { addDevicesToDeviceSet, createGroup } = useDeviceSets();
  const wrap = useCallback(
    <Args extends unknown[]>(handler: (...args: Args) => void) =>
      (...args: Args) => {
        onActionBoundary?.();
        handler(...args);
      },
    [onActionBoundary],
  );

  // "all"-mode means "every miner matching the current server filter"
  // — handed to the device-set RPCs via allDevices=true rather than an
  // explicit identifier list. Subset (incl. single-row) passes the
  // resolved ids straight through.
  const allDevices = selectionMode === "all";
  const deviceIdentifiers = allDevices ? undefined : selectedMinerIds;
  const minerCount = allDevices ? (displayCount ?? selectedMinerIds.length) : selectedMinerIds.length;
  const sourceLabel = `${minerCount} ${minerCount === 1 ? "miner" : "miners"}`;
  const addToGroupOpen =
    minerActions.currentAction === groupActions.addToGroup ? minerActions.showAddToGroupModal : false;

  const dispatchAddToGroup = useCallback(
    async (groupId: bigint) => {
      await new Promise<void>((resolve) => {
        void addDevicesToDeviceSet({
          deviceSetId: groupId,
          deviceIdentifiers,
          allDevices,
          onSuccess: () => {
            pushToast({
              status: STATUSES.success,
              message: `Added ${sourceLabel} to group`,
            });
            resolve();
          },
          onError: (msg) => {
            pushToast({ status: STATUSES.error, message: msg });
            resolve();
          },
        });
      });
    },
    [addDevicesToDeviceSet, deviceIdentifiers, allDevices, sourceLabel],
  );

  const handleAddToGroupConfirm = useCallback(
    async (groupIds: bigint[]) => {
      await Promise.all(groupIds.map(dispatchAddToGroup));
    },
    [dispatchAddToGroup],
  );

  const handleCreateGroup = useCallback(
    async (name: string) => {
      await new Promise<void>((resolve) => {
        void createGroup({
          label: name,
          deviceIdentifiers,
          allDevices,
          onSuccess: () => {
            pushToast({ status: STATUSES.success, message: `Added ${sourceLabel} to group` });
            resolve();
          },
          onError: (msg) => {
            pushToast({ status: STATUSES.error, message: msg });
            resolve();
          },
        });
      });
    },
    [createGroup, deviceIdentifiers, allDevices, sourceLabel],
  );

  return (
    <>
      <ManagePowerModal
        open={minerActions.currentAction === performanceActions.managePower ? minerActions.showManagePowerModal : false}
        onConfirm={wrap(minerActions.handleManagePowerConfirm)}
        onDismiss={wrap(minerActions.handleManagePowerDismiss)}
      />
      <FirmwareUpdateModal
        open={
          minerActions.currentAction === deviceActions.firmwareUpdate ? minerActions.showFirmwareUpdateModal : false
        }
        onConfirm={wrap(minerActions.handleFirmwareUpdateConfirm)}
        onDismiss={wrap(minerActions.handleFirmwareUpdateDismiss)}
      />
      <CoolingModeModal
        open={minerActions.currentAction === settingsActions.coolingMode ? minerActions.showCoolingModeModal : false}
        minerCount={minerActions.coolingModeCount}
        initialCoolingMode={minerActions.currentCoolingMode}
        onConfirm={wrap(minerActions.handleCoolingModeConfirm)}
        onDismiss={wrap(minerActions.handleCoolingModeDismiss)}
      />
      <AuthenticateFleetModal
        open={minerActions.showAuthenticateFleetModal}
        purpose={minerActions.authenticationPurpose ?? undefined}
        onAuthenticated={minerActions.handleFleetAuthenticated}
        onDismiss={wrap(minerActions.handleAuthDismiss)}
      />
      <ManageSecurityModal
        open={minerActions.showManageSecurityModal}
        minerGroups={minerActions.minerGroups}
        onUpdateGroup={minerActions.handleUpdateGroup}
        onDismiss={wrap(minerActions.handleSecurityModalClose)}
        onDone={wrap(minerActions.handleSecurityModalClose)}
      />
      <UpdateMinerPasswordModal
        open={minerActions.showUpdatePasswordModal}
        hasThirdPartyMiners={minerActions.hasThirdPartyMiners}
        onConfirm={minerActions.handlePasswordConfirm}
        onDismiss={wrap(minerActions.handlePasswordDismiss)}
      />
      <ParentPickerModal
        kind="group"
        show={addToGroupOpen}
        selectionMode="multi"
        sourceLabel={sourceLabel}
        createNewLabel="New group name"
        onCreateNew={handleCreateGroup}
        onDismiss={wrap(minerActions.handleAddToGroupDismiss)}
        onConfirm={handleAddToGroupConfirm}
      />
    </>
  );
};

export default MinerActionModalStack;
