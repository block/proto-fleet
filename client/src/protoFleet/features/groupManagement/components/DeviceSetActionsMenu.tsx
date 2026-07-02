import { type RefObject, useCallback, useEffect, useMemo, useRef, useState } from "react";

import { fetchAllMinerSnapshots } from "@/protoFleet/api/fetchAllMinerSnapshots";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { siteFilterFromActive } from "@/protoFleet/components/PageHeader/SitePicker";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import PoolSelectionPageWrapper from "@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage";
import { BulkActionsPopover } from "@/protoFleet/features/fleetManagement/components/BulkActions";
import BulkActionConfirmDialog from "@/protoFleet/features/fleetManagement/components/BulkActions/BulkActionConfirmDialog";
import { type BulkAction } from "@/protoFleet/features/fleetManagement/components/BulkActions/types";
import UnsupportedMinersModal from "@/protoFleet/features/fleetManagement/components/BulkActions/UnsupportedMinersModal";
import {
  deviceActions,
  groupActions,
  performanceActions,
  settingsActions,
  type SupportedAction,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";
import CoolingModeModal from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/CoolingModeModal";
import ManagePowerModal from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManagePowerModal";
import {
  ManageSecurityModal,
  UpdateMinerPasswordModal,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManageSecurity";
import { useMinerActions } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/useMinerActions";
import { useBatchActions } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";
import type { ActiveSite } from "@/protoFleet/store/types/activeSite";
import { ArrowRight, Edit, Ellipsis } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { type ButtonVariant, sizes, variants } from "@/shared/components/Button";
import { type SelectionMode } from "@/shared/components/List";
import { PopoverProvider, usePopover } from "@/shared/components/Popover";
import { positions } from "@/shared/constants";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

type DeviceSetActionType = SupportedAction | "edit-group" | "view-group";
type DeviceSetType = "group" | "rack";

/**
 * Member IDs and miner snapshots fetched when an action was chosen, frozen so
 * later prop or membership changes cannot retarget an in-progress flow. The id
 * identifies one prepare→run flow; completions carrying an older id are ignored.
 */
type PreparedAction = {
  id: number;
  action: DeviceSetActionType;
  memberDeviceIds: string[];
  miners: Record<string, MinerStateSnapshot>;
};

const noMiners: Record<string, MinerStateSnapshot> = {};

interface DeviceSetActionsMenuProps {
  memberDeviceIds?: string[];
  deviceSetId?: bigint;
  /** Whether this menu is for a group or a rack. Affects the filter used for miner snapshot fetches. */
  deviceSetType?: DeviceSetType;
  onEdit: () => void;
  /** Label for the edit action in the popover menu (e.g., "Edit group", "Edit rack"). */
  editLabel?: string;
  /** Optional callback to navigate to the detail view. When provided, a "View" action is shown. */
  onView?: () => void;
  /** Label for the view action in the popover menu (e.g., "View group", "View rack"). */
  viewLabel?: string;
  onActionComplete?: () => void;
  popoverClassName?: string;
  buttonVariant?: ButtonVariant;
  /** Ref that exposes the sleep action handler so a parent can trigger it from an external button. */
  sleepActionRef?: RefObject<(() => void) | null>;
  /** Ref that reflects whether a bulk-action dialog is currently open. */
  actionActiveRef?: RefObject<boolean>;
  /** Optional route scope for list-row actions. Omitted on canonical detail pages. */
  activeSite?: ActiveSite;
  /** Human-readable label for the active site scope. */
  activeSiteLabel?: string;
  /** Human-readable group/rack label used in scoped confirmation copy. */
  deviceSetLabel?: string;
  /** Org-wide member count used for scoped X/Y confirmation copy. */
  totalMemberCount?: number;
}

const DeviceSetActionsMenu = (props: DeviceSetActionsMenuProps) => {
  const { deviceSetId, deviceSetType = "group", activeSite } = props;
  const isScopedGroupAction = deviceSetType === "group" && activeSite !== undefined && activeSite.kind !== "all";
  const siteScopeFilter =
    isScopedGroupAction && activeSite ? siteFilterFromActive(activeSite) : { siteIds: [], includeUnassigned: false };
  // Remount on target change: every dialog, fetch, and in-flight continuation
  // belongs to one group/rack + site scope, so switching targets resets them
  // wholesale instead of guarding each async path individually.
  const targetKey = [
    deviceSetType,
    deviceSetId?.toString() ?? "",
    isScopedGroupAction ? "scoped" : "unscoped",
    siteScopeFilter.includeUnassigned ? "unassigned" : "assigned",
    siteScopeFilter.siteIds.join(","),
  ].join(":");

  return (
    <PopoverProvider>
      <DeviceSetActionsMenuInner key={targetKey} {...props} />
    </PopoverProvider>
  );
};

const DeviceSetActionsMenuInner = ({
  memberDeviceIds: propMemberDeviceIds,
  deviceSetId,
  deviceSetType = "group",
  onEdit,
  editLabel = "Edit group",
  onView,
  viewLabel = "View group",
  onActionComplete,
  popoverClassName,
  buttonVariant = variants.secondary,
  sleepActionRef,
  actionActiveRef,
  activeSite,
  activeSiteLabel,
  deviceSetLabel,
  totalMemberCount,
}: DeviceSetActionsMenuProps) => {
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const batchOps = useBatchActions();
  const [isOpen, setIsOpen] = useState(false);
  const isScopedGroupAction = deviceSetType === "group" && activeSite !== undefined && activeSite.kind !== "all";
  const siteScopeFilter = useMemo(
    () =>
      isScopedGroupAction && activeSite ? siteFilterFromActive(activeSite) : { siteIds: [], includeUnassigned: false },
    [activeSite, isScopedGroupAction],
  );
  const siteScopeLabel = useMemo(() => {
    if (!isScopedGroupAction || !activeSite) return "";
    return activeSite.kind === "unassigned" ? "unassigned miners" : (activeSiteLabel ?? `site ${activeSite.id}`);
  }, [activeSite, activeSiteLabel, isScopedGroupAction]);

  const { listGroupMembers } = useDeviceSets();

  const propMemberDeviceIdsRef = useRef(propMemberDeviceIds);
  // Keep the ref in sync with the latest prop without re-creating the fetch
  // callbacks when only this prop changes (parents sometimes pass a new array
  // reference on every render).
  useEffect(() => {
    propMemberDeviceIdsRef.current = propMemberDeviceIds;
  }, [propMemberDeviceIds]);

  useEffect(() => {
    setPopoverRenderMode("portal-fixed");
  }, [setPopoverRenderMode]);

  const onClickOutside = useCallback(() => {
    setIsOpen(false);
  }, []);

  useClickOutside({
    ref: triggerRef,
    onClickOutside,
    ignoreSelectors: [".popover-content"],
  });

  const handleOpen = useCallback(() => {
    setIsOpen((open) => !open);
  }, []);

  const scopedActionsRef = useRef<BulkAction<DeviceSetActionType>[]>([]);
  const [showWarnDialog, setShowWarnDialog] = useState(false);
  const [pendingScopedAction, setPendingScopedAction] = useState<BulkAction<DeviceSetActionType> | null>(null);
  const [pendingUnsupportedContinuation, setPendingUnsupportedContinuation] = useState<{
    continueAction: () => void;
  } | null>(null);

  // Member data is fetched when an action is chosen, not when the menu opens,
  // so the popover renders instantly and the data is fresh at the moment it
  // matters.
  const [preparedAction, setPreparedAction] = useState<PreparedAction | null>(null);
  const [isPreparing, setIsPreparing] = useState(false);
  const prepareIdRef = useRef(0);
  const prepareAbortRef = useRef<AbortController | null>(null);

  useEffect(() => () => prepareAbortRef.current?.abort(), []);

  const actionMemberDeviceIds = useMemo(
    () => preparedAction?.memberDeviceIds ?? propMemberDeviceIds ?? [],
    [preparedAction, propMemberDeviceIds],
  );
  const actionMemberDeviceIdsLoaded = preparedAction !== null || propMemberDeviceIds !== undefined;
  const selectedMinersWithStatus = useMemo(
    () => actionMemberDeviceIds.map((id) => ({ deviceIdentifier: id })),
    [actionMemberDeviceIds],
  );

  const clearPreparedActionState = useCallback(() => {
    ++prepareIdRef.current;
    prepareAbortRef.current?.abort();
    prepareAbortRef.current = null;
    setIsPreparing(false);
    setPreparedAction(null);
  }, []);

  const resetActionFlowState = useCallback(() => {
    setPendingScopedAction(null);
    setPendingUnsupportedContinuation(null);
    setShowWarnDialog(false);
    clearPreparedActionState();
  }, [clearPreparedActionState]);

  // Toast onClose callbacks capture this at registration, so a completion from
  // an earlier flow clears only its own prepared action, never a newer one.
  const preparedActionId = preparedAction?.id;
  const handleActionComplete = useCallback(() => {
    if (preparedActionId !== undefined) {
      setPreparedAction((current) => (current?.id === preparedActionId ? null : current));
    }
    onActionComplete?.();
  }, [onActionComplete, preparedActionId]);

  const {
    currentAction,
    popoverActions,
    handleConfirmation,
    handleCancel,
    handleMiningPoolSuccess,
    handleMiningPoolError,
    handleMiningPoolWarning,
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
    selectionMode: "subset" as SelectionMode,
    startBatchOperation: batchOps.startBatchOperation,
    completeBatchOperation: batchOps.completeBatchOperation,
    removeDevicesFromBatch: batchOps.removeDevicesFromBatch,
    miners: preparedAction?.miners ?? noMiners,
    onActionComplete: handleActionComplete,
    onUnsupportedMinersContinue: ({ action, continueAction }) => {
      if (!isScopedGroupAction) return false;
      const scopedAction = scopedActionsRef.current.find((candidate) => candidate.action === action);
      if (!scopedAction?.requiresConfirmation || !scopedAction.confirmation) return false;

      setPendingScopedAction(scopedAction);
      setPendingUnsupportedContinuation({ continueAction });
      setShowWarnDialog(true);
      return true;
    },
  });

  // Keep actionActiveRef in sync so the parent can pause polling during action flows
  useEffect(() => {
    if (actionActiveRef) {
      actionActiveRef.current =
        isPreparing ||
        preparedAction !== null ||
        currentAction !== null ||
        showWarnDialog ||
        unsupportedMinersInfo.visible ||
        showPoolSelectionPage ||
        showManagePowerModal ||
        showCoolingModeModal ||
        showAuthenticateFleetModal ||
        showUpdatePasswordModal ||
        showManageSecurityModal;
    }
  }, [
    actionActiveRef,
    currentAction,
    isPreparing,
    preparedAction,
    showAuthenticateFleetModal,
    showCoolingModeModal,
    showManagePowerModal,
    showManageSecurityModal,
    showPoolSelectionPage,
    showUpdatePasswordModal,
    showWarnDialog,
    unsupportedMinersInfo.visible,
  ]);

  // Customize actions for group context:
  // 1. Filter out "Add to group" (already in a group)
  // 2. Insert "Edit group" after the cooling mode divider
  const groupPopoverActions = useMemo(() => {
    const filtered = popoverActions.filter((a) => a.action !== groupActions.addToGroup);

    const editGroupAction: BulkAction<DeviceSetActionType> = {
      action: "edit-group",
      title: editLabel,
      icon: <Edit />,
      actionHandler: () => {
        setIsOpen(false);
        onEdit();
      },
      requiresConfirmation: false,
      showGroupDivider: true,
    };

    const viewGroupAction: BulkAction<DeviceSetActionType> | null = onView
      ? {
          action: "view-group",
          title: viewLabel,
          icon: <ArrowRight className="text-text-primary" />,
          actionHandler: () => {
            setIsOpen(false);
            onView();
          },
          requiresConfirmation: false,
          showGroupDivider: false,
        }
      : null;

    // Insert "Edit group" where the organization section was (after cooling mode's divider)
    const coolingModeIndex = filtered.findIndex((a) => a.action === settingsActions.coolingMode);
    const withEdit =
      coolingModeIndex !== -1
        ? [
            ...filtered.slice(0, coolingModeIndex),
            filtered[coolingModeIndex],
            editGroupAction,
            ...filtered.slice(coolingModeIndex + 1),
          ]
        : [editGroupAction, ...filtered];

    return viewGroupAction ? [viewGroupAction, ...withEdit] : withEdit;
  }, [popoverActions, onEdit, editLabel, onView, viewLabel]);

  const poolMiners = useMemo(() => {
    if (poolFilteredDeviceIds) {
      return poolFilteredDeviceIds.map((id) => ({ deviceIdentifier: id }));
    }
    return selectedMinersWithStatus;
  }, [poolFilteredDeviceIds, selectedMinersWithStatus]);

  const scopedActionSummary = useMemo(() => {
    if (!isScopedGroupAction) return "";
    const scopedCount = actionMemberDeviceIds.length;
    const totalCount = totalMemberCount ?? scopedCount;
    const groupLabel = deviceSetLabel ?? "this group";
    const scopeLabel = activeSite?.kind === "unassigned" ? "unassigned miners" : `miners in ${siteScopeLabel}`;
    const countSummary =
      scopedCount === totalCount
        ? `all ${scopedCount} ${scopedCount === 1 ? "miner" : "miners"} in ${groupLabel}`
        : `${scopedCount} of the ${totalCount} miners in ${groupLabel}`;
    return `This action only applies to ${scopeLabel}, ${countSummary}`;
  }, [
    actionMemberDeviceIds.length,
    activeSite?.kind,
    deviceSetLabel,
    isScopedGroupAction,
    siteScopeLabel,
    totalMemberCount,
  ]);

  const scopedActionSubtitle = useCallback(
    (subtitle?: string) => {
      if (!scopedActionSummary) return subtitle ?? "";
      if (!subtitle) return `${scopedActionSummary}.`;
      const actionEffect = subtitle.replace(/^These miners\s+/, "").replace(/^This miner\s+/, "");
      return `${scopedActionSummary} ${actionEffect}`;
    },
    [scopedActionSummary],
  );

  const getSnapshotFilter = useCallback(() => {
    if (!deviceSetId) return undefined;
    if (deviceSetType === "rack") {
      return { rackIds: [deviceSetId] };
    }
    if (!isScopedGroupAction) {
      return { groupIds: [deviceSetId] };
    }
    return {
      groupIds: [deviceSetId],
      siteIds: siteScopeFilter.siteIds,
      includeUnassigned: siteScopeFilter.includeUnassigned,
    };
  }, [deviceSetId, deviceSetType, isScopedGroupAction, siteScopeFilter.includeUnassigned, siteScopeFilter.siteIds]);

  const fetchMemberIdsForAction = useCallback(
    (deviceSetId: bigint, signal: AbortSignal) => {
      const propIds = propMemberDeviceIdsRef.current;
      if (propIds) return Promise.resolve(propIds);

      return new Promise<string[]>((resolve) => {
        let resolved = false;
        const resolveOnce = (ids: string[]) => {
          if (resolved) return;
          resolved = true;
          resolve(ids);
        };

        listGroupMembers({
          deviceSetId,
          siteIds: siteScopeFilter.siteIds,
          includeUnassigned: siteScopeFilter.includeUnassigned,
          signal,
          onSuccess: resolveOnce,
          onError: () => resolveOnce([]),
          onFinally: () => resolveOnce([]),
        });
      });
    },
    [listGroupMembers, siteScopeFilter.includeUnassigned, siteScopeFilter.siteIds],
  );

  const prepareAndRunAction = useCallback(
    async (action: DeviceSetActionType) => {
      if (action === "edit-group" || action === "view-group") return;

      const id = ++prepareIdRef.current;

      const filter = getSnapshotFilter();
      if (!deviceSetId || !filter) {
        setPreparedAction({ id, action, memberDeviceIds: propMemberDeviceIdsRef.current ?? [], miners: noMiners });
        return;
      }

      setIsPreparing(true);
      prepareAbortRef.current?.abort();
      const controller = new AbortController();
      prepareAbortRef.current = controller;

      const [ids, miners] = await Promise.all([
        fetchMemberIdsForAction(deviceSetId, controller.signal),
        fetchAllMinerSnapshots(filter, controller.signal).catch(() => ({})),
      ]);

      // A newer prepare or a cancel superseded this fetch; whoever did now owns the state.
      if (id !== prepareIdRef.current || controller.signal.aborted) return;

      setIsPreparing(false);
      if (isScopedGroupAction && ids.length === 0) {
        setShowWarnDialog(false);
        pushToast({ message: `No miners in ${siteScopeLabel}.`, status: TOAST_STATUSES.error });
        return;
      }
      setPreparedAction({ id, action, memberDeviceIds: ids, miners });
    },
    [deviceSetId, fetchMemberIdsForAction, getSnapshotFilter, isScopedGroupAction, siteScopeLabel],
  );

  // Expose the sleep action handler to the parent via ref
  useEffect(() => {
    if (!sleepActionRef) return;
    const sleepAction = popoverActions.find((a) => a.action === deviceActions.shutdown);
    if (sleepAction) {
      sleepActionRef.current = () => {
        setShowWarnDialog(sleepAction.requiresConfirmation);
        void prepareAndRunAction(deviceActions.shutdown);
      };
    } else {
      sleepActionRef.current = null;
    }
  }, [sleepActionRef, popoverActions, prepareAndRunAction]);

  const handlePopoverAction = useCallback((requiresConfirmation: boolean) => {
    setIsOpen(false);
    setShowWarnDialog(requiresConfirmation);
    if (!requiresConfirmation) {
      setPendingScopedAction(null);
      setPendingUnsupportedContinuation(null);
    }
  }, []);

  const handleDialogConfirm = useCallback(() => {
    if (pendingUnsupportedContinuation) {
      const { continueAction } = pendingUnsupportedContinuation;
      setPendingUnsupportedContinuation(null);
      setPendingScopedAction(null);
      setShowWarnDialog(false);
      continueAction();
      return;
    }
    if (pendingScopedAction) {
      const action = pendingScopedAction;
      setPendingScopedAction(null);
      setShowWarnDialog(false);
      action.actionHandler();
      return;
    }
    setShowWarnDialog(false);
    handleConfirmation();
  }, [handleConfirmation, pendingScopedAction, pendingUnsupportedContinuation]);

  const handleDialogCancel = useCallback(() => {
    setPendingUnsupportedContinuation(null);
    setPendingScopedAction(null);
    setShowWarnDialog(false);
    handleCancel();
    clearPreparedActionState();
  }, [handleCancel, clearPreparedActionState]);

  const scopedGroupPopoverActions = useMemo(() => {
    if (!isScopedGroupAction) return groupPopoverActions;

    return groupPopoverActions.map((action) => {
      if (action.action === "edit-group" || action.action === "view-group") {
        return action;
      }

      if (actionMemberDeviceIdsLoaded && actionMemberDeviceIds.length === 0) {
        return {
          ...action,
          disabled: true,
          disabledReason: `No miners in ${siteScopeLabel}.`,
        };
      }

      if (action.requiresConfirmation && action.confirmation) {
        return {
          ...action,
          confirmation: {
            ...action.confirmation,
            subtitle: scopedActionSubtitle(action.confirmation.subtitle),
          },
        };
      }

      return {
        ...action,
        requiresConfirmation: true,
        confirmation: {
          title: `${action.title} ${actionMemberDeviceIds.length} ${actionMemberDeviceIds.length === 1 ? "miner" : "miners"}?`,
          subtitle: scopedActionSubtitle(),
          confirmAction: {
            title: action.title,
            variant: variants.primary,
          },
          testId: `${action.action}-scoped-confirm-button`,
        },
        actionHandler: () => {
          setPendingScopedAction(action);
          setShowWarnDialog(true);
        },
      };
    });
  }, [
    groupPopoverActions,
    actionMemberDeviceIds.length,
    actionMemberDeviceIdsLoaded,
    isScopedGroupAction,
    scopedActionSubtitle,
    siteScopeLabel,
  ]);

  useEffect(() => {
    scopedActionsRef.current = scopedGroupPopoverActions;
  }, [scopedGroupPopoverActions]);

  // Run the chosen action once its frozen member IDs/snapshots have rendered
  // into useMinerActions. Runs at most once per prepared action.
  const replayedIdRef = useRef(0);
  useEffect(() => {
    if (!preparedAction || replayedIdRef.current === preparedAction.id) return;
    replayedIdRef.current = preparedAction.id;
    const action = scopedGroupPopoverActions.find((candidate) => candidate.action === preparedAction.action);
    if (!action || action.disabled) {
      queueMicrotask(() => {
        setPreparedAction((current) => (current?.id === preparedAction.id ? null : current));
      });
      return;
    }
    action.actionHandler();
  }, [preparedAction, scopedGroupPopoverActions]);

  const displayedGroupPopoverActions = useMemo(
    () =>
      scopedGroupPopoverActions.map((action) => {
        if (action.action === "edit-group" || action.action === "view-group") return action;
        if (action.disabled) return action;
        return {
          ...action,
          actionHandler: () => {
            void prepareAndRunAction(action.action);
          },
        };
      }),
    [prepareAndRunAction, scopedGroupPopoverActions],
  );

  // Keep the base confirmation hidden while the unsupported-miners modal is active.
  // Scoped unsupported continuations can re-open the scoped confirmation after Continue.
  const handleUnsupportedMinersContinueWithReset = useCallback(() => {
    setShowWarnDialog(false);
    handleUnsupportedMinersContinue();
  }, [handleUnsupportedMinersContinue]);

  const handleUnsupportedMinersDismissWithReset = useCallback(() => {
    resetActionFlowState();
    handleUnsupportedMinersDismiss();
  }, [handleUnsupportedMinersDismiss, resetActionFlowState]);

  return (
    <>
      <div ref={triggerRef} className="relative">
        <Button
          size={sizes.compact}
          variant={buttonVariant}
          ariaLabel="Device set actions"
          prefixIcon={<Ellipsis width={iconSizes.small} className="text-text-primary-70" />}
          onClick={handleOpen}
        />
        {isOpen ? (
          <BulkActionsPopover<DeviceSetActionType>
            actions={displayedGroupPopoverActions}
            beforeEach={handlePopoverAction}
            testId="group-actions-popover"
            position={positions["bottom right"]}
            className={popoverClassName ?? "!space-y-0 !rounded-2xl px-0 pt-2 pb-1"}
          />
        ) : null}
      </div>

      <UnsupportedMinersModal
        open={unsupportedMinersInfo.visible}
        unsupportedGroups={unsupportedMinersInfo.unsupportedGroups}
        totalUnsupportedCount={unsupportedMinersInfo.totalUnsupportedCount}
        noneSupported={unsupportedMinersInfo.noneSupported}
        onContinue={handleUnsupportedMinersContinueWithReset}
        onDismiss={handleUnsupportedMinersDismissWithReset}
      />
      {/* Confirmation dialogs */}
      {scopedGroupPopoverActions
        .filter((action) => action.requiresConfirmation && action.confirmation)
        .map((action) => {
          const showDialog =
            (currentAction === action.action || pendingScopedAction?.action === action.action) &&
            showWarnDialog &&
            !unsupportedMinersInfo.visible;
          return (
            <BulkActionConfirmDialog
              key={action.action}
              open={showDialog}
              actionConfirmation={action.confirmation!}
              onConfirmation={handleDialogConfirm}
              onCancel={handleDialogCancel}
              testId="group-actions-dialog"
            />
          );
        })}

      {/* Modal dialogs */}
      <PoolSelectionPageWrapper
        open={showPoolSelectionPage ? !!fleetCredentials : false}
        selectedMiners={poolMiners}
        selectionMode={"subset" as SelectionMode}
        poolNeededCount={poolFilteredDeviceIds ? poolFilteredDeviceIds.length : actionMemberDeviceIds.length}
        userUsername={fleetCredentials?.username}
        userPassword={fleetCredentials?.password}
        onSuccess={handleMiningPoolSuccess}
        onError={handleMiningPoolError}
        onWarning={handleMiningPoolWarning}
        onDismiss={handleCancel}
      />
      <ManagePowerModal
        open={currentAction === performanceActions.managePower ? showManagePowerModal : false}
        onConfirm={handleManagePowerConfirm}
        onDismiss={handleManagePowerDismiss}
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
    </>
  );
};

export default DeviceSetActionsMenu;
