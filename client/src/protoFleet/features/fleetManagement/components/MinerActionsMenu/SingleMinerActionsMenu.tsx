import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import BulkActionConfirmDialog from "../BulkActions/BulkActionConfirmDialog";
import { BulkAction, UnsupportedMinersInfo } from "../BulkActions/types";
import UnsupportedMinersModal from "../BulkActions/UnsupportedMinersModal";
import { insertActionAfter, insertActionBefore } from "./actionMenuUtils";
import AddToGroupModal from "./AddToGroupModal";
import { deviceActions, groupActions, performanceActions, settingsActions, SupportedAction } from "./constants";
import CoolingModeModal from "./CoolingModeModal";
import FirmwareUpdateModal from "./FirmwareUpdateModal";
import ManagePowerModal from "./ManagePowerModal";
import { ManageSecurityModal, UpdateMinerPasswordModal } from "./ManageSecurity";
import RenameMinerDialog from "./RenameMinerDialog";
import UpdateWorkerNameDialog from "./UpdateWorkerNameDialog";
import { type SecurityActionsProps } from "./useManageSecurityFlow";
import { type MinerSelection, useMinerActions } from "./useMinerActions";
import { waitForWorkerNameBatchResult } from "./waitForWorkerNameBatchResult";
import { CoolingMode } from "@/protoFleet/api/generated/common/v1/cooling_pb";
import type {
  MinerStateSnapshot,
  UpdateWorkerNamesResponse,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { PerformanceMode } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import type { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import useUpdateWorkerNames from "@/protoFleet/api/useUpdateWorkerNames";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { useBatchActions } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";
import { ArrowRight, Edit, Ellipsis, MiningPools } from "@/shared/assets/icons";
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
    withCapabilityCheck,
    unsupportedMinersInfo,
    handleUnsupportedMinersContinue,
    handleUnsupportedMinersDismiss,
    showManageSecurityModal,
    minerGroups,
    handleUpdateGroup,
    handleSecurityModalClose,
    showRenameDialog,
    handleRenameOpen,
    handleRenameConfirm,
    handleRenameDismiss,
    showAddToGroupModal,
    handleAddToGroupDismiss,
  } = useMinerActions({
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

    const actions = insertActionAfter(popoverActions, settingsActions.miningPool, updateWorkerNameAction);
    const actionsWithRenameBeforeGroup = insertActionBefore(actions, groupActions.addToGroup, renameAction);

    if (actionsWithRenameBeforeGroup !== actions) {
      return viewMinerAction ? [viewMinerAction, ...actionsWithRenameBeforeGroup] : actionsWithRenameBeforeGroup;
    }

    const actionsWithRenameBeforeSecurity = insertActionBefore(actions, settingsActions.security, {
      ...renameAction,
      showGroupDivider: true,
    });

    if (actionsWithRenameBeforeSecurity !== actions) {
      return viewMinerAction ? [viewMinerAction, ...actionsWithRenameBeforeSecurity] : actionsWithRenameBeforeSecurity;
    }

    return viewMinerAction ? [viewMinerAction, ...actions, renameAction] : [...actions, renameAction];
  }, [handleRenameOpen, handleUpdateWorkerNameAction, handleViewMiner, minerUrl, popoverActions]);

  const visibleActions = useMemo(
    () =>
      needsAuthentication
        ? actionsWithSingleNameFlows.filter((a) => unauthenticatedActions.has(a.action))
        : actionsWithSingleNameFlows,
    [actionsWithSingleNameFlows, needsAuthentication],
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
        showPoolSelectionPage={showPoolSelectionPage}
        fleetCredentials={fleetCredentials}
        handleMiningPoolSuccess={handleMiningPoolSuccess}
        handleMiningPoolError={handleMiningPoolError}
        handleMiningPoolWarning={handleMiningPoolWarning}
        handleCancel={handleCancel}
        showManagePowerModal={showManagePowerModal}
        handleManagePowerConfirm={handleManagePowerConfirm}
        handleManagePowerDismiss={handleManagePowerDismiss}
        showFirmwareUpdateModal={showFirmwareUpdateModal}
        handleFirmwareUpdateConfirm={handleFirmwareUpdateConfirm}
        handleFirmwareUpdateDismiss={handleFirmwareUpdateDismiss}
        showCoolingModeModal={showCoolingModeModal}
        coolingModeCount={coolingModeCount}
        currentCoolingMode={currentCoolingMode}
        handleCoolingModeConfirm={handleCoolingModeConfirm}
        handleCoolingModeDismiss={handleCoolingModeDismiss}
        showAuthenticateFleetModal={showAuthenticateFleetModal}
        authenticationPurpose={authenticationPurpose}
        showUpdatePasswordModal={showUpdatePasswordModal}
        hasThirdPartyMiners={hasThirdPartyMiners}
        handleFleetAuthenticated={handleFleetAuthenticated}
        handlePasswordConfirm={handlePasswordConfirm}
        handlePasswordDismiss={handlePasswordDismiss}
        handleAuthDismiss={handleAuthDismiss}
        unsupportedMinersInfo={unsupportedMinersInfo}
        handleUnsupportedMinersContinue={handleUnsupportedMinersContinueWithReset}
        handleUnsupportedMinersDismiss={handleUnsupportedMinersDismiss}
        showManageSecurityModal={showManageSecurityModal}
        minerGroups={minerGroups}
        handleUpdateGroup={handleUpdateGroup}
        handleSecurityModalClose={handleSecurityModalClose}
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
        showAddToGroupModal={showAddToGroupModal}
        handleAddToGroupDismiss={handleAddToGroupDismiss}
      />
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
  showPoolSelectionPage: boolean;
  fleetCredentials: { username: string; password: string } | undefined;
  handleMiningPoolSuccess: (batchIdentifier: string) => void;
  handleMiningPoolError: (error: string) => void;
  handleMiningPoolWarning: (warning: string) => void;
  handleCancel: () => void;
  showManagePowerModal: boolean;
  handleManagePowerConfirm: (performanceMode: PerformanceMode) => void;
  handleManagePowerDismiss: () => void;
  showFirmwareUpdateModal: boolean;
  handleFirmwareUpdateConfirm: (firmwareFileId: string) => void;
  handleFirmwareUpdateDismiss: () => void;
  showCoolingModeModal: boolean;
  coolingModeCount: number;
  currentCoolingMode: CoolingMode | undefined;
  handleCoolingModeConfirm: (coolingMode: CoolingMode) => void;
  handleCoolingModeDismiss: () => void;
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
  showAddToGroupModal: boolean;
  handleAddToGroupDismiss: () => void;
} & SecurityActionsProps;

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
  showPoolSelectionPage,
  fleetCredentials,
  handleMiningPoolSuccess,
  handleMiningPoolError,
  handleMiningPoolWarning,
  handleCancel,
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
  showAddToGroupModal,
  handleAddToGroupDismiss,
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
      <ManagePowerModal
        open={currentAction === performanceActions.managePower ? showManagePowerModal : false}
        onConfirm={handleManagePowerConfirm}
        onDismiss={handleManagePowerDismiss}
      />
      <FirmwareUpdateModal
        open={currentAction === deviceActions.firmwareUpdate ? showFirmwareUpdateModal : false}
        onConfirm={handleFirmwareUpdateConfirm}
        onDismiss={handleFirmwareUpdateDismiss}
      />
      <CoolingModeModal
        open={currentAction === settingsActions.coolingMode ? showCoolingModeModal : false}
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
      <AuthenticateFleetModal
        open={showWorkerNameAuthenticateModal}
        purpose="workerNames"
        onAuthenticated={handleUpdateWorkerNameAuthenticated}
        onDismiss={handleUpdateWorkerNameDismiss}
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
        open={currentAction === groupActions.addToGroup ? showAddToGroupModal : false}
        onDismiss={handleAddToGroupDismiss}
        selectedMiners={[deviceIdentifier]}
        selectionMode="subset"
        displayCount={1}
      />
    </div>
  );
};

export default SingleMinerActionsMenu;
