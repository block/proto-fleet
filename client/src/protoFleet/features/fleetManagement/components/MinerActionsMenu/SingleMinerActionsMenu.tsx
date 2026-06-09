import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import BulkActionConfirmDialog from "../BulkActions/BulkActionConfirmDialog";
import { BulkAction, UnsupportedMinersInfo } from "../BulkActions/types";
import UnsupportedMinersModal from "../BulkActions/UnsupportedMinersModal";
import { insertActionAfter, insertActionBefore } from "./actionMenuUtils";
import { usePermittedActions } from "./actionPermissions";
import { deviceActions, groupActions, settingsActions, SupportedAction } from "./constants";
import MinerActionModalStack from "./MinerActionModalStack";
import MinerReparentPicker from "./MinerReparentPicker";
import RenameMinerDialog from "./RenameMinerDialog";
import UpdateWorkerNameDialog from "./UpdateWorkerNameDialog";
import { type MinerSelection, useMinerActions } from "./useMinerActions";
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
import { ArrowRight, Edit, Ellipsis, MiningPools, Plus } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Popover, { popoverSizes } from "@/shared/components/Popover";
import { PopoverProvider, usePopover } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";
import { pushToast, removeToast, STATUSES as TOAST_STATUSES, updateToast } from "@/shared/features/toaster";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

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
  // Re-parent picker target. null = closed.
  const [reparentKind, setReparentKind] = useState<"rack" | "site" | null>(null);

  const minerActionsResult = useMinerActions({
    selectedMiners,
    // Single-miner actions always target a specific device, never "all devices"
    selectionMode: "subset",
    startBatchOperation,
    completeBatchOperation,
    removeDevicesFromBatch,
    miners,
    onRefetchMiners,
    onActionStart,
    onActionComplete,
  });
  // Modals shared with FleetGroupActionsMenu + MinerActionsMenu are
  // rendered via MinerActionModalStack from the full result. Local
  // destructure pulls only the fields this shell still references.
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

    // Re-parent openers — same shape as MinerActionsMenu's bulk
    // entries; click opens the picker for the single miner. Inserted
    // before addToGroup so the cluster reads site → rack → group.
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

  // Hide actions whose backing RPC the caller can't invoke. viewMiner
  // has no RPC and stays visible regardless of permissions; the server
  // still enforces every gate.
  const permittedActions = usePermittedActions(actionsWithSingleNameFlows);

  const visibleActions = useMemo(
    () =>
      needsAuthentication ? permittedActions.filter((a) => unauthenticatedActions.has(a.action)) : permittedActions,
    [permittedActions, needsAuthentication],
  );

  const [isOpen, setIsOpen] = useState(false);
  const [showWarnDialog, setShowWarnDialog] = useState(false);

  const onClickOutside = useCallback(() => {
    setIsOpen(false);
  }, []);

  const handleAction = (action: BulkAction<SingleMinerAction>) => {
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
        popoverActions={visibleActions}
        confirmationActions={actionsWithSingleNameFlows}
        onClickOutside={onClickOutside}
        handleAction={handleAction}
        handleConfirmationClick={handleConfirmationClick}
        handleCancelClick={handleCancelClick}
        selectedMiners={selectedMiners}
        minerActions={minerActionsResult}
        showPoolSelectionPage={showPoolSelectionPage}
        fleetCredentials={fleetCredentials}
        handleMiningPoolSuccess={handleMiningPoolSuccess}
        handleMiningPoolError={handleMiningPoolError}
        handleMiningPoolWarning={handleMiningPoolWarning}
        handleCancel={handleCancel}
        unsupportedMinersInfo={unsupportedMinersInfo}
        handleUnsupportedMinersContinue={handleUnsupportedMinersContinueWithReset}
        handleUnsupportedMinersDismiss={handleUnsupportedMinersDismiss}
        deviceIdentifier={deviceIdentifier}
        minerName={minerName}
        workerName={workerName}
        showRenameDialog={showRenameDialog}
        handleRenameConfirm={handleRenameConfirm}
        handleRenameDismiss={handleRenameDismiss}
        showWorkerNameAuthenticateModal={showWorkerNameAuthenticateModal}
        handleUpdateWorkerNameAuthenticated={handleUpdateWorkerNameAuthenticated}
        showUpdateWorkerNameDialog={showUpdateWorkerNameDialog}
        handleUpdateWorkerNameConfirm={handleUpdateWorkerNameConfirm}
        handleUpdateWorkerNameDismiss={handleUpdateWorkerNameDismiss}
      />
      {reparentKind ? (
        <MinerReparentPicker
          kind={reparentKind}
          deviceIdentifiers={[deviceIdentifier]}
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
    </PopoverProvider>
  );
};

type SingleMinerActionsMenuInnerProps = {
  isOpen: boolean;
  setIsOpen: (value: boolean | ((prev: boolean) => boolean)) => void;
  showWarnDialog: boolean;
  currentAction: SupportedAction | null;
  popoverActions: BulkAction<SingleMinerAction>[];
  confirmationActions: BulkAction<SingleMinerAction>[];
  onClickOutside: () => void;
  handleAction: (action: BulkAction<SingleMinerAction>) => void;
  handleConfirmationClick: () => void;
  handleCancelClick: () => void;
  selectedMiners: MinerSelection[];
  // Full useMinerActions return — fed into the shared MinerActionModalStack
  // so the inner doesn't need to forward every modal field individually.
  minerActions: ReturnType<typeof useMinerActions>;
  showPoolSelectionPage: boolean;
  fleetCredentials: { username: string; password: string } | undefined;
  handleMiningPoolSuccess: (batchIdentifier: string, dispatchedDeviceIdentifiers: string[]) => void;
  handleMiningPoolError: (error: string) => void;
  handleMiningPoolWarning: (warning: string) => void;
  handleCancel: () => void;
  unsupportedMinersInfo: UnsupportedMinersInfo;
  handleUnsupportedMinersContinue: () => void;
  handleUnsupportedMinersDismiss: () => void;
  deviceIdentifier: string;
  minerName?: string;
  workerName?: string;
  showRenameDialog: boolean;
  handleRenameConfirm: (name: string) => void;
  handleRenameDismiss: () => void;
  showWorkerNameAuthenticateModal: boolean;
  handleUpdateWorkerNameAuthenticated: (username: string, password: string) => void;
  showUpdateWorkerNameDialog: boolean;
  handleUpdateWorkerNameConfirm: (name: string) => void;
  handleUpdateWorkerNameDismiss: () => void;
};

const SingleMinerActionsMenuInner = ({
  isOpen,
  setIsOpen,
  showWarnDialog,
  currentAction,
  popoverActions,
  confirmationActions,
  onClickOutside,
  handleAction,
  handleConfirmationClick,
  handleCancelClick,
  selectedMiners,
  minerActions,
  showPoolSelectionPage,
  fleetCredentials,
  handleMiningPoolSuccess,
  handleMiningPoolError,
  handleMiningPoolWarning,
  handleCancel,
  unsupportedMinersInfo,
  handleUnsupportedMinersContinue,
  handleUnsupportedMinersDismiss,
  deviceIdentifier,
  minerName,
  workerName,
  showRenameDialog,
  handleRenameConfirm,
  handleRenameDismiss,
  showWorkerNameAuthenticateModal,
  handleUpdateWorkerNameAuthenticated,
  showUpdateWorkerNameDialog,
  handleUpdateWorkerNameConfirm,
  handleUpdateWorkerNameDismiss,
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
        prefixIcon={<Ellipsis width={iconSizes.small} className="text-text-primary-70" />}
        testId="single-miner-actions-menu-button"
        onClick={() => setIsOpen((prev) => !prev)}
      />
      {isOpen ? (
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
              {action.showGroupDivider ? <Divider dividerStyle="thick" /> : null}
            </Fragment>
          ))}
        </Popover>
      ) : null}
      <UnsupportedMinersModal
        open={unsupportedMinersInfo.visible}
        unsupportedGroups={unsupportedMinersInfo.unsupportedGroups}
        totalUnsupportedCount={unsupportedMinersInfo.totalUnsupportedCount}
        noneSupported={unsupportedMinersInfo.noneSupported}
        onContinue={handleUnsupportedMinersContinue}
        onDismiss={handleUnsupportedMinersDismiss}
      />
      {confirmationActions
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
        minerActions={minerActions}
        selectedMinerIds={[deviceIdentifier]}
        selectionMode="subset"
        displayCount={1}
      />
    </div>
  );
};

export default SingleMinerActionsMenu;
