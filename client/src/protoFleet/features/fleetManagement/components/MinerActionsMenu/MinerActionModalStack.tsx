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
  displayCount?: number;
  // Fires before each modal's dismiss/confirm — used by
  // FleetGroupActionsMenu to clear its pendingAction.
  onActionBoundary?: () => void;
}

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
      onActionBoundary
        ? (...args: Args) => {
            onActionBoundary();
            handler(...args);
          }
        : handler,
    [onActionBoundary],
  );

  const allDevices = selectionMode === "all";
  const deviceIdentifiers = allDevices ? undefined : selectedMinerIds;
  const minerCount = allDevices ? (displayCount ?? selectedMinerIds.length) : selectedMinerIds.length;
  const sourceLabel = `${minerCount} ${minerCount === 1 ? "miner" : "miners"}`;
  const addToGroupOpen =
    minerActions.currentAction === groupActions.addToGroup ? minerActions.showAddToGroupModal : false;

  // Reject on RPC error so ParentPickerModal keeps the picker open.
  const dispatchAddToGroup = useCallback(
    (groupId: bigint) =>
      new Promise<void>((resolve, reject) => {
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
            reject(new Error(msg));
          },
        });
      }),
    [addDevicesToDeviceSet, deviceIdentifiers, allDevices, sourceLabel],
  );

  const handleAddToGroupConfirm = useCallback(
    async (groupIds: bigint[]) => {
      await Promise.all(groupIds.map(dispatchAddToGroup));
    },
    [dispatchAddToGroup],
  );

  const handleCreateGroup = useCallback(
    (name: string) =>
      new Promise<void>((resolve, reject) => {
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
            reject(new Error(msg));
          },
        });
      }),
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
