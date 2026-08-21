import { useCallback, useEffect, useRef } from "react";

import { deviceActions, groupActions, performanceActions, settingsActions } from "./constants";
import CoolingModeModal from "./CoolingModeModal";
import FirmwareUpdateModal from "./FirmwareUpdateModal";
import ManagePowerModal from "./ManagePowerModal";
import { ManageSecurityModal, UpdateMinerPasswordModal } from "./ManageSecurity";
import { type useMinerActions } from "./useMinerActions";
import type { MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import ParentPickerModal from "@/protoFleet/components/ParentPickerModal";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { applyFleetVisiblePairingStatuses } from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";
import { resolveAllModeIds } from "@/protoFleet/features/fleetManagement/utils/resolveAllModeMiners";
import { type SelectionMode } from "@/shared/components/List";
import { pushToast, removeToast, STATUSES, updateToast } from "@/shared/features/toaster";

type MinerActions = ReturnType<typeof useMinerActions>;

// Selector inputs for a group mutation: either an explicit device list or the
// whole-fleet flag. A scoped/filtered "all" resolves to a device list (the
// group selector can't carry a filter); unscoped "all" keeps the flag.
type GroupTarget = { deviceIdentifiers?: string[]; allDevices?: boolean };

interface MinerActionModalStackProps {
  minerActions: MinerActions;
  selectedMinerIds: string[];
  selectionMode: SelectionMode;
  displayCount?: number;
  /** Active scoped filter (URL chips ∩ SitePicker scope); resolves the target
   *  set for a filtered "all" group assignment. */
  currentFilter?: MinerListFilter;
  // Fires before each modal's dismiss/confirm — used by
  // FleetGroupActionsMenu to clear its pendingAction.
  onActionBoundary?: () => void;
}

const MinerActionModalStack = ({
  minerActions,
  selectedMinerIds,
  selectionMode,
  displayCount,
  currentFilter,
  onActionBoundary,
}: MinerActionModalStackProps) => {
  const { addDevicesToGroup, createGroup } = useDeviceSets();
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

  const allMode = selectionMode === "all";
  const minerCount = allMode ? (displayCount ?? selectedMinerIds.length) : selectedMinerIds.length;
  const sourceLabel = `${minerCount} ${minerCount === 1 ? "miner" : "miners"}`;
  const addToGroupOpen =
    minerActions.currentAction === groupActions.addToGroup ? minerActions.showAddToGroupModal : false;

  // Abort in-flight snapshot pagination when the picker is dismissed or the
  // component unmounts, so a filtered all-mode resolution can't dispatch the
  // group mutation after the UI closed (mirrors MinerReparentPicker). This
  // component stays mounted while `show` toggles, so we key a fresh controller
  // to each open and abort in the effect cleanup that fires on close/unmount.
  const abortRef = useRef<AbortController | null>(null);
  useEffect(() => {
    if (!addToGroupOpen) return;
    const controller = new AbortController();
    abortRef.current = controller;
    return () => {
      controller.abort();
    };
  }, [addToGroupOpen]);

  // Resolve the operator's selection to a concrete group-selector target. A
  // scoped/filtered "all" can't ride the group selector (its all-devices is a
  // bare flag), so — like the rack/site/building reparent flow — page the
  // filtered set into an explicit id list. Unscoped "all" keeps the whole-fleet
  // flag. Throws on empty/error (after surfacing a toast) so the confirm/create
  // promise rejects and ParentPickerModal keeps the picker open for retry —
  // rather than resolving normally, which it would treat as success and close.
  const resolveGroupTarget = useCallback(async (): Promise<GroupTarget> => {
    if (!allMode) return { deviceIdentifiers: selectedMinerIds };
    if (!currentFilter) return { allDevices: true };

    const loadingToast = pushToast({
      message: "Loading selected miners…",
      status: STATUSES.loading,
      longRunning: true,
    });
    let ids: string[];
    try {
      ({ ids } = await resolveAllModeIds(applyFleetVisiblePairingStatuses(currentFilter), abortRef.current?.signal));
    } catch (err) {
      const message = err instanceof Error && err.message ? err.message : "Couldn't load selected miners. Try again.";
      updateToast(loadingToast, { message, status: STATUSES.error });
      throw err instanceof Error ? err : new Error(message);
    }
    removeToast(loadingToast);
    // Picker dismissed/unmounted mid-pagination: bail without the empty-selection
    // toast; the confirm/create handlers' aborted gate skips the dispatch.
    if (abortRef.current?.signal.aborted) return { deviceIdentifiers: ids };
    if (ids.length === 0) {
      pushToast({ message: "No miners selected.", status: STATUSES.queued });
      throw new Error("No miners matched the current filter.");
    }
    return { deviceIdentifiers: ids };
  }, [allMode, selectedMinerIds, currentFilter]);

  // Reject on RPC error so ParentPickerModal keeps the picker open.
  const dispatchAddToGroup = useCallback(
    (groupId: bigint, target: GroupTarget) =>
      new Promise<void>((resolve, reject) => {
        void addDevicesToGroup({
          targetGroupId: groupId,
          ...target,
          // Cancels the RPC and suppresses its toast if the picker closes
          // while the add is in flight (the hook gates on signal.aborted).
          signal: abortRef.current?.signal,
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
    [addDevicesToGroup, sourceLabel],
  );

  const handleAddToGroupConfirm = useCallback(
    async (groupIds: bigint[]) => {
      const target = await resolveGroupTarget();
      if (abortRef.current?.signal.aborted) return;
      await Promise.all(groupIds.map((groupId) => dispatchAddToGroup(groupId, target)));
    },
    [dispatchAddToGroup, resolveGroupTarget],
  );

  const handleCreateGroup = useCallback(
    async (name: string) => {
      const target = await resolveGroupTarget();
      if (abortRef.current?.signal.aborted) return;
      await new Promise<void>((resolve, reject) => {
        void createGroup({
          label: name,
          ...target,
          signal: abortRef.current?.signal,
          onSuccess: () => {
            pushToast({ status: STATUSES.success, message: `Added ${sourceLabel} to group` });
            resolve();
          },
          onError: (msg) => {
            pushToast({ status: STATUSES.error, message: msg });
            reject(new Error(msg));
          },
        });
      });
    },
    [createGroup, resolveGroupTarget, sourceLabel],
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
        target={minerActions.firmwareUpdateTarget}
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
