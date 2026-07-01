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
import { useClickOutside } from "@/shared/hooks/useClickOutside";

type DeviceSetActionType = SupportedAction | "edit-group" | "view-group";
type DeviceSetType = "group" | "rack";
type TargetCachedMemberIds = {
  targetKey: string;
  ids: string[];
};
type TargetCachedMiners = {
  targetKey: string;
  miners: Record<string, MinerStateSnapshot>;
};
type PreparedActionTarget = {
  memberDeviceIds: string[];
  targetKey: string;
  version: number;
  tick: number;
};

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
  return (
    <PopoverProvider>
      <DeviceSetActionsMenuInner {...props} />
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
  const actionTargetKey = useMemo(
    () =>
      [
        deviceSetType,
        deviceSetId?.toString() ?? "",
        isScopedGroupAction ? "scoped" : "unscoped",
        siteScopeFilter.includeUnassigned ? "unassigned" : "assigned",
        siteScopeFilter.siteIds.join(","),
      ].join(":"),
    [deviceSetId, deviceSetType, isScopedGroupAction, siteScopeFilter.includeUnassigned, siteScopeFilter.siteIds],
  );
  const actionTargetKeyRef = useRef(actionTargetKey);
  const previousActionTargetKeyRef = useRef(actionTargetKey);
  actionTargetKeyRef.current = actionTargetKey;

  // Lazy-fetched member IDs for table context (when deviceSetId is provided but memberDeviceIds aren't)
  const [fetchedMemberIds, setFetchedMemberIds] = useState<TargetCachedMemberIds | null>(null);
  const { listGroupMembers } = useDeviceSets();

  // Lazy-fetched miner snapshots for firmware model checks
  const [fetchedMiners, setFetchedMiners] = useState<TargetCachedMiners | null>(null);

  const fetchVersionRef = useRef(0);
  const fetchAbortRef = useRef<AbortController | null>(null);
  const propMemberDeviceIdsRef = useRef(propMemberDeviceIds);
  const memberDeviceIdsRef = useRef<string[]>([]);
  // Keep the ref in sync with the latest prop without re-running the fetch
  // effect when only this prop changes (parents sometimes pass a new array
  // reference on every render).
  useEffect(() => {
    propMemberDeviceIdsRef.current = propMemberDeviceIds;
  }, [propMemberDeviceIds]);

  const cachedMemberIdsForTarget = useMemo(
    () => (fetchedMemberIds?.targetKey === actionTargetKey ? fetchedMemberIds.ids : null),
    [actionTargetKey, fetchedMemberIds],
  );
  const cachedMinersForTarget = useMemo(
    () => (fetchedMiners?.targetKey === actionTargetKey ? fetchedMiners.miners : {}),
    [actionTargetKey, fetchedMiners],
  );
  const memberDeviceIds = useMemo(
    () => propMemberDeviceIds ?? cachedMemberIdsForTarget ?? [],
    [propMemberDeviceIds, cachedMemberIdsForTarget],
  );
  memberDeviceIdsRef.current = memberDeviceIds;
  const memberDeviceIdsLoaded = propMemberDeviceIds !== undefined || cachedMemberIdsForTarget !== null;

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

  const refreshEmptyMemberCache = useCallback(
    (targetKey: string, id: bigint, version: number) => {
      const controller = new AbortController();
      let resolved = false;
      const resolveOnce = (ids: string[]) => {
        if (
          resolved ||
          controller.signal.aborted ||
          targetKey !== actionTargetKeyRef.current ||
          version !== fetchVersionRef.current
        ) {
          return;
        }
        resolved = true;
        setFetchedMemberIds({ targetKey, ids });
        if (ids.length === 0) {
          setFetchedMiners({ targetKey, miners: {} });
        }
      };

      listGroupMembers({
        deviceSetId: id,
        siteIds: siteScopeFilter.siteIds,
        includeUnassigned: siteScopeFilter.includeUnassigned,
        signal: controller.signal,
        onSuccess: resolveOnce,
        onError: () => resolveOnce([]),
        onFinally: () => resolveOnce([]),
      });

      return controller;
    },
    [listGroupMembers, siteScopeFilter.includeUnassigned, siteScopeFilter.siteIds],
  );

  const handleOpen = useCallback(() => {
    const opening = !isOpen;

    if (opening && !deviceSetId) {
      setFetchedMemberIds(null);
      setFetchedMiners(null);
    } else if (opening && propMemberDeviceIds === undefined && deviceSetId && cachedMemberIdsForTarget?.length === 0) {
      const targetKey = actionTargetKeyRef.current;
      const version = fetchVersionRef.current;
      setFetchedMemberIds(null);
      setFetchedMiners(null);
      refreshEmptyMemberCache(targetKey, deviceSetId, version);
    }

    setIsOpen(opening);
  }, [cachedMemberIdsForTarget, deviceSetId, isOpen, propMemberDeviceIds, refreshEmptyMemberCache]);

  const scopedActionsRef = useRef<BulkAction<DeviceSetActionType>[]>([]);
  const [showWarnDialog, setShowWarnDialog] = useState(false);
  const [pendingScopedAction, setPendingScopedAction] = useState<BulkAction<DeviceSetActionType> | null>(null);
  const [pendingPreparedAction, setPendingPreparedAction] = useState<
    | (PreparedActionTarget & {
        action: DeviceSetActionType;
        tick: number;
      })
    | null
  >(null);
  const [activePreparedAction, setActivePreparedAction] = useState<PreparedActionTarget | null>(null);
  const [isPreparingAction, setIsPreparingAction] = useState(false);
  const pendingPreparedActionForTarget =
    pendingPreparedAction?.targetKey === actionTargetKey && pendingPreparedAction.version === fetchVersionRef.current
      ? pendingPreparedAction
      : null;
  const activePreparedActionForTarget =
    activePreparedAction?.targetKey === actionTargetKey && activePreparedAction.version === fetchVersionRef.current
      ? activePreparedAction
      : null;
  const actionMemberDeviceIds =
    activePreparedActionForTarget?.memberDeviceIds ??
    pendingPreparedActionForTarget?.memberDeviceIds ??
    memberDeviceIds;
  const actionMemberDeviceIdsLoaded =
    activePreparedActionForTarget !== null || pendingPreparedActionForTarget !== null || memberDeviceIdsLoaded;
  const selectedMinersWithStatus = useMemo(
    () => actionMemberDeviceIds.map((id) => ({ deviceIdentifier: id })),
    [actionMemberDeviceIds],
  );
  const preparedActionTickRef = useRef(0);
  const [pendingUnsupportedContinuation, setPendingUnsupportedContinuation] = useState<{
    continueAction: () => void;
  } | null>(null);
  const preparedActionFlowActive =
    isPreparingAction || pendingPreparedActionForTarget !== null || activePreparedActionForTarget !== null;
  const preparedActionLifecycleTarget = activePreparedActionForTarget ?? pendingPreparedActionForTarget;
  const actionLifecycleKey = preparedActionLifecycleTarget
    ? `${preparedActionLifecycleTarget.targetKey}:${preparedActionLifecycleTarget.version}:${preparedActionLifecycleTarget.tick}`
    : actionTargetKey;

  const clearPreparedActionTarget = useCallback(() => {
    setPendingPreparedAction(null);
    setActivePreparedAction(null);
    setIsPreparingAction(false);
  }, []);

  const clearMatchingPreparedActionTarget = useCallback(
    (target: PreparedActionTarget | null) => {
      if (!target) {
        clearPreparedActionTarget();
        return;
      }

      const matchesTarget = (candidate: PreparedActionTarget | null) =>
        candidate?.targetKey === target.targetKey &&
        candidate.version === target.version &&
        candidate.tick === target.tick;

      setPendingPreparedAction((current) => (matchesTarget(current) ? null : current));
      setActivePreparedAction((current) => (matchesTarget(current) ? null : current));
      if (target.version === fetchVersionRef.current) {
        setIsPreparingAction(false);
      }
    },
    [clearPreparedActionTarget],
  );

  const resetLocalPreparedActionState = useCallback(() => {
    setPendingScopedAction(null);
    setPendingUnsupportedContinuation(null);
    setShowWarnDialog(false);
    clearPreparedActionTarget();
  }, [clearPreparedActionTarget]);

  useEffect(() => {
    return () => {
      // eslint-disable-next-line react-hooks/exhaustive-deps -- intentional ref mutation in cleanup
      ++fetchVersionRef.current;
      fetchAbortRef.current?.abort();
      fetchAbortRef.current = null;
      resetLocalPreparedActionState();
    };
  }, [actionTargetKey, resetLocalPreparedActionState]);

  const handlePreparedActionComplete = useCallback(() => {
    clearMatchingPreparedActionTarget(preparedActionLifecycleTarget);
    onActionComplete?.();
  }, [clearMatchingPreparedActionTarget, onActionComplete, preparedActionLifecycleTarget]);

  const handlePreparedActionCancel = useCallback(() => {
    clearPreparedActionTarget();
  }, [clearPreparedActionTarget]);

  const setPendingPreparedTarget = useCallback(
    (target: Omit<PreparedActionTarget, "tick"> & { action: DeviceSetActionType }) => {
      preparedActionTickRef.current += 1;
      setPendingPreparedAction({
        ...target,
        tick: preparedActionTickRef.current,
      });
    },
    [],
  );

  const setActivePreparedTarget = useCallback((target: PreparedActionTarget) => {
    setActivePreparedAction(target);
  }, []);

  const clearPendingPreparedAction = useCallback(() => {
    setPendingPreparedAction(null);
  }, []);

  const markPreparingAction = useCallback(() => {
    setIsPreparingAction(true);
  }, []);

  const clearPreparingActionForVersion = useCallback((version: number) => {
    if (version === fetchVersionRef.current) {
      setIsPreparingAction(false);
    }
  }, []);

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
    miners: cachedMinersForTarget,
    actionLifecycleKey,
    onActionComplete: handlePreparedActionComplete,
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

  useEffect(() => {
    if (previousActionTargetKeyRef.current === actionTargetKey) return;
    previousActionTargetKeyRef.current = actionTargetKey;
    queueMicrotask(() => {
      handleCancel({ notifyComplete: false });
      resetLocalPreparedActionState();
    });
  }, [actionTargetKey, handleCancel, resetLocalPreparedActionState]);

  // Keep actionActiveRef in sync so the parent can pause polling during action flows
  useEffect(() => {
    if (actionActiveRef) {
      actionActiveRef.current =
        preparedActionFlowActive ||
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
    preparedActionFlowActive,
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

      const filter = getSnapshotFilter();
      if (!deviceSetId || !filter) {
        setPendingPreparedTarget({
          action,
          memberDeviceIds: memberDeviceIdsRef.current,
          targetKey: actionTargetKeyRef.current,
          version: fetchVersionRef.current,
        });
        return;
      }

      const version = ++fetchVersionRef.current;
      const targetKey = actionTargetKeyRef.current;
      markPreparingAction();
      fetchAbortRef.current?.abort();
      const controller = new AbortController();
      fetchAbortRef.current = controller;
      const isCurrent = () =>
        version === fetchVersionRef.current && !controller.signal.aborted && targetKey === actionTargetKeyRef.current;

      if (!propMemberDeviceIdsRef.current) {
        setFetchedMemberIds(null);
      }
      setFetchedMiners(null);

      const [ids, miners] = await Promise.all([
        fetchMemberIdsForAction(deviceSetId, controller.signal),
        fetchAllMinerSnapshots(filter, controller.signal).catch(() => ({})),
      ]);

      if (!isCurrent()) {
        clearPreparingActionForVersion(version);
        return;
      }

      if (!propMemberDeviceIdsRef.current) {
        setFetchedMemberIds({ targetKey, ids });
      }
      setFetchedMiners({ targetKey, miners });
      clearPreparingActionForVersion(version);
      if (isScopedGroupAction && ids.length === 0) return;
      setPendingPreparedTarget({
        action,
        memberDeviceIds: ids,
        targetKey,
        version,
      });
    },
    [
      clearPreparingActionForVersion,
      deviceSetId,
      fetchMemberIdsForAction,
      getSnapshotFilter,
      isScopedGroupAction,
      markPreparingAction,
      setPendingPreparedTarget,
    ],
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
    handlePreparedActionCancel();
  }, [handleCancel, handlePreparedActionCancel]);

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

  useEffect(() => {
    if (!pendingPreparedAction) return;
    if (
      pendingPreparedAction.targetKey !== actionTargetKey ||
      pendingPreparedAction.version !== fetchVersionRef.current
    ) {
      queueMicrotask(() => {
        clearPendingPreparedAction();
      });
      return;
    }
    const action = scopedGroupPopoverActions.find((candidate) => candidate.action === pendingPreparedAction.action);
    if (action?.disabled) {
      queueMicrotask(() => {
        clearPendingPreparedAction();
      });
      return;
    }
    if (!action) {
      queueMicrotask(() => {
        clearPendingPreparedAction();
      });
      return;
    }

    queueMicrotask(() => {
      if (
        pendingPreparedAction.targetKey !== actionTargetKeyRef.current ||
        pendingPreparedAction.version !== fetchVersionRef.current
      ) {
        return;
      }
      setActivePreparedTarget({
        memberDeviceIds: pendingPreparedAction.memberDeviceIds,
        targetKey: pendingPreparedAction.targetKey,
        version: pendingPreparedAction.version,
        tick: pendingPreparedAction.tick,
      });
      clearPendingPreparedAction();
      action.actionHandler();
    });
  }, [
    actionTargetKey,
    clearPendingPreparedAction,
    pendingPreparedAction,
    scopedGroupPopoverActions,
    setActivePreparedTarget,
  ]);

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
    resetLocalPreparedActionState();
    handleUnsupportedMinersDismiss();
  }, [handleUnsupportedMinersDismiss, resetLocalPreparedActionState]);

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
