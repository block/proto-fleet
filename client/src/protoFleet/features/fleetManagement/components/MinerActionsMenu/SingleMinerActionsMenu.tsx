import { useCallback, useMemo, useRef, useState } from "react";
import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import BulkActionConfirmDialog from "../BulkActions/BulkActionConfirmDialog";
import { BulkAction } from "../BulkActions/types";
import UnsupportedMinersModal from "../BulkActions/UnsupportedMinersModal";
import RowActionsMenu, { type RowAction } from "../RowActionsMenu";
import { insertActionAfter, insertActionBefore } from "./actionMenuUtils";
import { usePermittedActions } from "./actionPermissions";
import { deviceActions, groupActions, settingsActions, SupportedAction } from "./constants";
import MinerActionModalStack from "./MinerActionModalStack";
import MinerReparentPicker from "./MinerReparentPicker";
import RenameMinerDialog from "./RenameMinerDialog";
import UpdateWorkerNameDialog from "./UpdateWorkerNameDialog";
import { useMinerActions } from "./useMinerActions";
import { waitForWorkerNameBatchResult } from "./waitForWorkerNameBatchResult";
import type {
  MinerStateSnapshot,
  UpdateWorkerNamesResponse,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import useUpdateWorkerNames from "@/protoFleet/api/useUpdateWorkerNames";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { useBatchActions } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";
import { ArrowRight, Edit, MiningPools, Plus } from "@/shared/assets/icons";
import { pushToast, removeToast, STATUSES as TOAST_STATUSES, updateToast } from "@/shared/features/toaster";

type SingleMinerAction = SupportedAction | "viewMiner";

const unauthenticatedActions = new Set<SingleMinerAction>([deviceActions.unpair, "viewMiner"]);

interface SingleMinerActionsMenuProps {
  deviceIdentifier: string;
  minerUrl?: string;
  deviceStatus?: DeviceStatus;
  minerName?: string;
  workerName?: string;
  onActionStart?: () => void;
  onActionComplete?: () => void;
  needsAuthentication?: boolean;
  miners?: Record<string, MinerStateSnapshot>;
  onRefetchMiners?: () => void;
  onWorkerNameUpdated?: (deviceIdentifier: string, workerName: string) => void;
}

const SingleMinerActionsMenu = ({
  deviceIdentifier,
  minerUrl,
  deviceStatus,
  minerName,
  workerName,
  onActionStart,
  onActionComplete,
  needsAuthentication = false,
  miners,
  onRefetchMiners,
  onWorkerNameUpdated,
}: SingleMinerActionsMenuProps) => {
  const { startBatchOperation, completeBatchOperation, removeDevicesFromBatch } = useBatchActions();
  const { streamCommandBatchUpdates } = useMinerCommand();
  const { updateSingleWorkerName } = useUpdateWorkerNames();
  const selectedMiners = useMemo(() => [{ deviceIdentifier, deviceStatus }], [deviceIdentifier, deviceStatus]);
  const [showWorkerNameAuthenticateModal, setShowWorkerNameAuthenticateModal] = useState(false);
  const [showUpdateWorkerNameDialog, setShowUpdateWorkerNameDialog] = useState(false);
  const workerNameCredentialsRef = useRef<{ username: string; password: string } | undefined>(undefined);
  const [reparentKind, setReparentKind] = useState<"rack" | "site" | null>(null);
  const [showWarnDialog, setShowWarnDialog] = useState(false);

  const minerActionsResult = useMinerActions({
    selectedMiners,
    selectionMode: "subset",
    startBatchOperation,
    completeBatchOperation,
    removeDevicesFromBatch,
    miners,
    onRefetchMiners,
    onActionStart,
    onActionComplete,
  });
  const {
    currentAction,
    popoverActions,
    handleConfirmation,
    handleCancel,
    handleMiningPoolSuccess,
    handleMiningPoolError,
    handleMiningPoolWarning,
    showPoolSelectionPage,
    fleetCredentials,
    withCapabilityCheck,
    unsupportedMinersInfo,
    handleUnsupportedMinersContinue,
    handleUnsupportedMinersDismiss,
    showRenameDialog,
    handleRenameOpen,
    handleRenameConfirm,
    handleRenameDismiss,
  } = minerActionsResult;

  const handleViewMiner = useCallback(() => {
    if (minerUrl) {
      window.open(minerUrl, "_blank", "noopener,noreferrer");
    }
  }, [minerUrl]);

  const resetWorkerNameFlow = useCallback(() => {
    setShowWorkerNameAuthenticateModal(false);
    setShowUpdateWorkerNameDialog(false);
    workerNameCredentialsRef.current = undefined;
  }, []);

  const handleUpdateWorkerNameDismiss = useCallback(() => {
    resetWorkerNameFlow();
    onActionComplete?.();
  }, [onActionComplete, resetWorkerNameFlow]);

  const handleUpdateWorkerNameOpen = useCallback(() => {
    setShowWorkerNameAuthenticateModal(true);
  }, []);

  const handleUpdateWorkerNameAuthenticated = useCallback((username: string, password: string) => {
    workerNameCredentialsRef.current = { username, password };
    setShowWorkerNameAuthenticateModal(false);
    setShowUpdateWorkerNameDialog(true);
  }, []);

  const handleUpdateWorkerNameAction = useCallback(() => {
    onActionStart?.();
    void withCapabilityCheck(settingsActions.updateWorkerNames, () => {
      handleUpdateWorkerNameOpen();
    });
  }, [handleUpdateWorkerNameOpen, onActionStart, withCapabilityCheck]);

  const showWorkerNameUpdatedToast = useCallback(
    (toastId: number, name: string) => {
      onWorkerNameUpdated?.(deviceIdentifier, name);
      onRefetchMiners?.();
      updateToast(toastId, {
        message: "Worker name updated",
        status: TOAST_STATUSES.success,
      });
    },
    [deviceIdentifier, onRefetchMiners, onWorkerNameUpdated],
  );

  const showWorkerNameErrorToast = useCallback((toastId: number) => {
    updateToast(toastId, {
      message: "Failed to update worker name",
      status: TOAST_STATUSES.error,
    });
  }, []);

  const showWorkerNameUnchangedToast = useCallback(
    (toastId: number) => {
      onRefetchMiners?.();
      updateToast(toastId, {
        message: "Worker name unchanged",
        status: TOAST_STATUSES.success,
      });
    },
    [onRefetchMiners],
  );

  const handleDirectWorkerNameResponse = useCallback(
    (toastId: number, name: string, response: UpdateWorkerNamesResponse) => {
      if (response.failedCount > 0) {
        showWorkerNameErrorToast(toastId);
        return;
      }

      if (response.updatedCount > 0) {
        showWorkerNameUpdatedToast(toastId, name);
        return;
      }

      if (response.unchangedCount > 0) {
        showWorkerNameUnchangedToast(toastId);
        return;
      }

      removeToast(toastId);
    },
    [showWorkerNameErrorToast, showWorkerNameUnchangedToast, showWorkerNameUpdatedToast],
  );

  const handleStreamedWorkerNameResponse = useCallback(
    (
      toastId: number,
      name: string,
      response: UpdateWorkerNamesResponse,
      batchResult: Awaited<ReturnType<typeof waitForWorkerNameBatchResult>>,
    ) => {
      if (batchResult.streamFailed || response.failedCount > 0 || batchResult.failedCount > 0) {
        showWorkerNameErrorToast(toastId);
        return;
      }

      if (batchResult.successCount > 0) {
        showWorkerNameUpdatedToast(toastId, name);
        return;
      }

      if (response.unchangedCount > 0) {
        showWorkerNameUnchangedToast(toastId);
        return;
      }

      removeToast(toastId);
    },
    [showWorkerNameErrorToast, showWorkerNameUnchangedToast, showWorkerNameUpdatedToast],
  );

  const handleUpdateWorkerNameConfirm = useCallback(
    async (name: string) => {
      const workerNameCredentials = workerNameCredentialsRef.current;

      if (!workerNameCredentials) {
        return;
      }

      setShowUpdateWorkerNameDialog(false);

      const toastId = pushToast({
        message: "Updating worker name",
        status: TOAST_STATUSES.loading,
        longRunning: true,
      });

      try {
        const response = await updateSingleWorkerName(
          deviceIdentifier,
          name,
          workerNameCredentials.username,
          workerNameCredentials.password,
        );

        if (response.batchIdentifier) {
          startBatchOperation({
            batchIdentifier: response.batchIdentifier,
            action: settingsActions.updateWorkerNames,
            deviceIdentifiers: [deviceIdentifier],
          });

          try {
            const batchResult = await waitForWorkerNameBatchResult(streamCommandBatchUpdates, response.batchIdentifier);
            handleStreamedWorkerNameResponse(toastId, name, response, batchResult);
          } finally {
            completeBatchOperation(response.batchIdentifier);
          }
        } else {
          handleDirectWorkerNameResponse(toastId, name, response);
        }
      } catch {
        showWorkerNameErrorToast(toastId);
      } finally {
        resetWorkerNameFlow();
        onActionComplete?.();
      }
    },
    [
      completeBatchOperation,
      deviceIdentifier,
      handleDirectWorkerNameResponse,
      handleStreamedWorkerNameResponse,
      onActionComplete,
      resetWorkerNameFlow,
      showWorkerNameErrorToast,
      startBatchOperation,
      streamCommandBatchUpdates,
      updateSingleWorkerName,
    ],
  );

  const actionsWithSingleNameFlows = useMemo(() => {
    const viewMinerAction: BulkAction<SingleMinerAction> | null = minerUrl
      ? {
          action: "viewMiner",
          title: "View miner",
          icon: <ArrowRight className="text-text-primary" />,
          actionHandler: handleViewMiner,
          requiresConfirmation: false,
          showGroupDivider: true,
        }
      : null;

    const renameAction: BulkAction<SupportedAction> = {
      action: settingsActions.rename,
      title: "Rename",
      icon: <Edit />,
      actionHandler: handleRenameOpen,
      requiresConfirmation: false,
    };

    const updateWorkerNameAction: BulkAction<SupportedAction> = {
      action: settingsActions.updateWorkerNames,
      title: "Update worker name",
      icon: <MiningPools />,
      actionHandler: handleUpdateWorkerNameAction,
      requiresConfirmation: false,
    };

    // Inserted before addToGroup so the cluster reads site → rack → group.
    const addToRackAction: BulkAction<SupportedAction> = {
      action: groupActions.addToRack,
      title: "Add to rack",
      icon: <Plus />,
      actionHandler: () => setReparentKind("rack"),
      requiresConfirmation: false,
    };
    const addToSiteAction: BulkAction<SupportedAction> = {
      action: groupActions.addToSite,
      title: "Add to site",
      icon: <Plus />,
      actionHandler: () => setReparentKind("site"),
      requiresConfirmation: false,
    };

    const actions = insertActionAfter(popoverActions, settingsActions.miningPool, updateWorkerNameAction);
    const actionsWithRenameBeforeGroup = insertActionBefore(actions, groupActions.addToGroup, renameAction);
    const baseActions = actionsWithRenameBeforeGroup !== actions ? actionsWithRenameBeforeGroup : actions;
    const withAddToRack = insertActionBefore(baseActions, groupActions.addToGroup, addToRackAction);
    const withAddToSite = insertActionBefore(withAddToRack, groupActions.addToRack, addToSiteAction);

    if (actionsWithRenameBeforeGroup !== actions) {
      return viewMinerAction ? [viewMinerAction, ...withAddToSite] : withAddToSite;
    }

    const actionsWithRenameBeforeSecurity = insertActionBefore(withAddToSite, settingsActions.security, {
      ...renameAction,
      showGroupDivider: true,
    });

    if (actionsWithRenameBeforeSecurity !== withAddToSite) {
      return viewMinerAction ? [viewMinerAction, ...actionsWithRenameBeforeSecurity] : actionsWithRenameBeforeSecurity;
    }

    return viewMinerAction ? [viewMinerAction, ...withAddToSite, renameAction] : [...withAddToSite, renameAction];
  }, [handleRenameOpen, handleUpdateWorkerNameAction, handleViewMiner, minerUrl, popoverActions]);

  // viewMiner has no RPC and passes through unfiltered.
  const permittedActions = usePermittedActions(actionsWithSingleNameFlows);

  const visibleActions = useMemo(
    () =>
      needsAuthentication ? permittedActions.filter((a) => unauthenticatedActions.has(a.action)) : permittedActions,
    [permittedActions, needsAuthentication],
  );

  const handleAction = useCallback((action: BulkAction<SingleMinerAction>) => {
    if (action.requiresConfirmation) {
      setShowWarnDialog(true);
    }
    action.actionHandler();
  }, []);

  const handleConfirmationClick = useCallback(() => {
    setShowWarnDialog(false);
    handleConfirmation();
  }, [handleConfirmation]);

  const handleCancelClick = useCallback(() => {
    setShowWarnDialog(false);
    handleCancel();
  }, [handleCancel]);

  // Prevent confirmation dialog flash when continuing from unsupported miners modal
  const handleUnsupportedMinersContinueWithReset = useCallback(() => {
    setShowWarnDialog(false);
    handleUnsupportedMinersContinue();
  }, [handleUnsupportedMinersContinue]);

  const rowActions: RowAction[] = useMemo(
    () =>
      visibleActions.map((action) => ({
        label: action.title,
        icon: action.icon,
        showGroupDivider: action.showGroupDivider,
        testId: `${action.action}-popover-button`,
        onClick: () => handleAction(action),
      })),
    [visibleActions, handleAction],
  );

  return (
    <>
      <RowActionsMenu
        actions={rowActions}
        ariaLabel="Miner actions"
        testIdPrefix="single-miner-actions-popover"
        triggerTestId="single-miner-actions-menu-button"
      />
      <UnsupportedMinersModal
        open={unsupportedMinersInfo.visible}
        unsupportedGroups={unsupportedMinersInfo.unsupportedGroups}
        totalUnsupportedCount={unsupportedMinersInfo.totalUnsupportedCount}
        noneSupported={unsupportedMinersInfo.noneSupported}
        onContinue={handleUnsupportedMinersContinueWithReset}
        onDismiss={handleUnsupportedMinersDismiss}
      />
      {actionsWithSingleNameFlows
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
        open={showPoolSelectionPage ? !!fleetCredentials : false}
        selectedMiners={selectedMiners}
        selectionMode="subset"
        userUsername={fleetCredentials?.username}
        userPassword={fleetCredentials?.password}
        onSuccess={handleMiningPoolSuccess}
        onError={handleMiningPoolError}
        onWarning={handleMiningPoolWarning}
        onDismiss={handleCancel}
      />
      <RenameMinerDialog
        key={showRenameDialog ? deviceIdentifier : "closed"}
        open={currentAction === settingsActions.rename ? showRenameDialog : false}
        deviceIdentifier={deviceIdentifier}
        currentMinerName={minerName}
        onConfirm={handleRenameConfirm}
        onDismiss={handleRenameDismiss}
      />
      <UpdateWorkerNameDialog
        key={showUpdateWorkerNameDialog ? `${deviceIdentifier}-worker-name` : "closed-worker-name"}
        open={showUpdateWorkerNameDialog}
        currentWorkerName={workerName}
        onConfirm={handleUpdateWorkerNameConfirm}
        onDismiss={handleUpdateWorkerNameDismiss}
      />
      {/* The second AuthenticateFleetModal is specific to the worker-name
          flow which only this menu hosts — keep it inline. */}
      <AuthenticateFleetModal
        open={showWorkerNameAuthenticateModal}
        purpose="workerNames"
        onAuthenticated={handleUpdateWorkerNameAuthenticated}
        onDismiss={handleUpdateWorkerNameDismiss}
      />
      <MinerActionModalStack
        minerActions={minerActionsResult}
        selectedMinerIds={[deviceIdentifier]}
        selectionMode="subset"
        displayCount={1}
      />
      {reparentKind ? (
        <MinerReparentPicker
          kind={reparentKind}
          deviceIdentifiers={[deviceIdentifier]}
          selectionMode="subset"
          miners={miners}
          sourceLabel={minerName || "miner"}
          successMessage={(_count, target) =>
            target === "site"
              ? `Moved "${minerName || "miner"}" to selected site.`
              : `Added "${minerName || "miner"}" to selected rack.`
          }
          onClose={() => setReparentKind(null)}
          onRefetchMiners={onRefetchMiners}
        />
      ) : null}
    </>
  );
};

export default SingleMinerActionsMenu;
