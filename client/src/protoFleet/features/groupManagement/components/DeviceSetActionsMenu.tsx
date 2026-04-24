import { type RefObject, useCallback, useEffect, useMemo, useRef, useState } from "react";

import { fetchAllMinerSnapshots } from "@/protoFleet/api/fetchAllMinerSnapshots";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
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

type DeviceSetActionType = SupportedAction | "edit-group" | "view-group";
import CoolingModeModal from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/CoolingModeModal";
import ManagePowerModal from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManagePowerModal";
import {
  ManageSecurityModal,
  UpdateMinerPasswordModal,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManageSecurity";
import { useMinerActions } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/useMinerActions";
import { useBatchActions } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";
import { ArrowRight, Edit, Ellipsis } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { type ButtonVariant, sizes, variants } from "@/shared/components/Button";
import { type SelectionMode } from "@/shared/components/List";
import { PopoverProvider, usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { positions } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

type DeviceSetType = "group" | "rack";

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
}: DeviceSetActionsMenuProps) => {
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const batchOps = useBatchActions();
  const [isOpen, setIsOpen] = useState(false);

  // Lazy-fetched member IDs for table context (when deviceSetId is provided but memberDeviceIds aren't)
  const [fetchedMemberIds, setFetchedMemberIds] = useState<string[] | null>(null);
  const [fetchingMembers, setFetchingMembers] = useState(false);
  const { listGroupMembers } = useDeviceSets();

  // Lazy-fetched miner snapshots for firmware model checks
  const [fetchedMiners, setFetchedMiners] = useState<Record<string, MinerStateSnapshot>>({});
  const [fetchingMiners, setFetchingMiners] = useState(false);

  const fetchVersionRef = useRef(0);
  const propMemberDeviceIdsRef = useRef(propMemberDeviceIds);
  // Keep the ref in sync with the latest prop without re-running the fetch
  // effect when only this prop changes (parents sometimes pass a new array
  // reference on every render).
  useEffect(() => {
    propMemberDeviceIdsRef.current = propMemberDeviceIds;
  }, [propMemberDeviceIds]);

  const memberDeviceIds = useMemo(
    () => propMemberDeviceIds ?? fetchedMemberIds ?? [],
    [propMemberDeviceIds, fetchedMemberIds],
  );

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
    const opening = !isOpen;

    if (opening) {
      if (deviceSetId) {
        setFetchedMiners({});
        setFetchingMiners(true);

        if (!propMemberDeviceIds) {
          setFetchedMemberIds(null);
          setFetchingMembers(true);
        } else {
          setFetchingMembers(false);
        }
      } else {
        // No deviceSetId: the fetch effect will bail out, so clear any stale
        // data from a prior open so the menu does not show a previous group's
        // members/snapshots.
        setFetchedMemberIds(null);
        setFetchedMiners({});
      }
    }

    setIsOpen(opening);
  }, [isOpen, deviceSetId, propMemberDeviceIds]);

  // Fetch member IDs and miner snapshots when the menu opens.
  // Always refetch on open so membership changes are picked up.
  // A version counter prevents stale callbacks from updating state after
  // the effect re-fires (e.g. close/re-open, deviceSetId change).
  useEffect(() => {
    if (!isOpen || !deviceSetId) return;

    const version = ++fetchVersionRef.current;
    const controller = new AbortController();
    const isCurrent = () => version === fetchVersionRef.current;

    /* eslint-disable react-hooks/set-state-in-effect -- fetch members + miners on open; setState inside async callbacks is the external-sync pattern */
    if (!propMemberDeviceIdsRef.current) {
      setFetchedMemberIds(null);
      setFetchingMembers(true);
      listGroupMembers({
        deviceSetId,
        signal: controller.signal,
        onSuccess: (ids) => {
          if (isCurrent()) setFetchedMemberIds(ids);
        },
        onFinally: () => {
          if (isCurrent()) setFetchingMembers(false);
        },
      });
    } else {
      setFetchingMembers(false);
    }

    const filter = deviceSetType === "rack" ? { rackIds: [deviceSetId] } : { groupIds: [deviceSetId] };
    setFetchedMiners({});
    setFetchingMiners(true);
    /* eslint-enable react-hooks/set-state-in-effect */
    fetchAllMinerSnapshots(filter, controller.signal)
      .then((map) => {
        if (isCurrent()) setFetchedMiners(map);
      })
      .catch(() => {
        // Non-critical — firmware update will show a warning instead
      })
      .finally(() => {
        if (isCurrent()) setFetchingMiners(false);
      });

    return () => {
      // Invalidate version so stale callbacks are rejected.
      // Data state (fetchedMemberIds/fetchedMiners) is deliberately preserved
      // here so that programmatic closes during confirmation/modal flows do
      // not empty the selection that downstream handlers rely on. Stale data
      // is cleared in handleOpen when reopening without a deviceSetId.
      // eslint-disable-next-line react-hooks/exhaustive-deps -- intentional ref mutation in cleanup
      ++fetchVersionRef.current;
      controller.abort();
      setFetchingMembers(false);
      setFetchingMiners(false);
    };
  }, [isOpen, deviceSetId, deviceSetType, listGroupMembers]);

  const selectedMinersWithStatus = useMemo(
    () => memberDeviceIds.map((id) => ({ deviceIdentifier: id })),
    [memberDeviceIds],
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
    miners: fetchedMiners,
    onActionComplete,
  });

  // Keep actionActiveRef in sync so the parent can pause polling during action flows
  useEffect(() => {
    if (actionActiveRef) {
      actionActiveRef.current = currentAction !== null;
    }
  }, [actionActiveRef, currentAction]);

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

  const [showWarnDialog, setShowWarnDialog] = useState(false);

  // Expose the sleep action handler to the parent via ref
  useEffect(() => {
    if (!sleepActionRef) return;
    const sleepAction = popoverActions.find((a) => a.action === deviceActions.shutdown);
    if (sleepAction) {
      sleepActionRef.current = () => {
        setShowWarnDialog(sleepAction.requiresConfirmation);
        sleepAction.actionHandler();
      };
    } else {
      sleepActionRef.current = null;
    }
  }, [sleepActionRef, popoverActions]);

  const handlePopoverAction = useCallback((requiresConfirmation: boolean) => {
    setIsOpen(false);
    if (requiresConfirmation) {
      setShowWarnDialog(true);
    }
  }, []);

  const handleDialogConfirm = useCallback(() => {
    setShowWarnDialog(false);
    handleConfirmation();
  }, [handleConfirmation]);

  const handleDialogCancel = useCallback(() => {
    setShowWarnDialog(false);
    handleCancel();
  }, [handleCancel]);

  // Prevent confirmation dialog flash when continuing from unsupported miners modal
  const handleUnsupportedMinersContinueWithReset = useCallback(() => {
    setShowWarnDialog(false);
    handleUnsupportedMinersContinue();
  }, [handleUnsupportedMinersContinue]);

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
          fetchingMembers || fetchingMiners ? (
            <div
              className={`popover-content absolute right-0 z-10 flex items-center justify-center rounded-2xl bg-surface-overlay p-6 shadow-elevation-200 ${popoverClassName ?? ""}`}
            >
              <ProgressCircular indeterminate />
            </div>
          ) : (
            <BulkActionsPopover<DeviceSetActionType>
              actions={groupPopoverActions}
              beforeEach={handlePopoverAction}
              testId="group-actions-popover"
              position={positions["bottom right"]}
              className={popoverClassName ?? "!space-y-0 !rounded-2xl px-0 pt-2 pb-1"}
            />
          )
        ) : null}
      </div>

      <UnsupportedMinersModal
        open={unsupportedMinersInfo.visible}
        unsupportedGroups={unsupportedMinersInfo.unsupportedGroups}
        totalUnsupportedCount={unsupportedMinersInfo.totalUnsupportedCount}
        noneSupported={unsupportedMinersInfo.noneSupported}
        onContinue={handleUnsupportedMinersContinueWithReset}
        onDismiss={handleUnsupportedMinersDismiss}
      />
      {/* Confirmation dialogs */}
      {groupPopoverActions
        .filter((action) => action.requiresConfirmation && action.confirmation)
        .map((action) => {
          const showDialog = currentAction === action.action && showWarnDialog && !unsupportedMinersInfo.visible;
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
        poolNeededCount={poolFilteredDeviceIds ? poolFilteredDeviceIds.length : memberDeviceIds.length}
        userUsername={fleetCredentials?.username}
        userPassword={fleetCredentials?.password}
        onSuccess={handleMiningPoolSuccess}
        onError={handleMiningPoolError}
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
