import { useCallback, useEffect, useMemo, useState } from "react";

import { useCollections } from "@/protoFleet/api/useCollections";
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

type GroupActionType = SupportedAction | "edit-group";
import CoolingModeModal from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/CoolingModeModal";
import ManagePowerModal from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManagePowerModal";
import {
  ManageSecurityModal,
  UpdateMinerPasswordModal,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManageSecurity";
import { useMinerActions } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/useMinerActions";
import { Edit, Ellipsis } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { type ButtonVariant, sizes, variants } from "@/shared/components/Button";
import { type SelectionMode } from "@/shared/components/List";
import { PopoverProvider, usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { positions } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

interface GroupActionsMenuProps {
  memberDeviceIds?: string[];
  collectionId?: bigint;
  onEditGroup: () => void;
  onActionComplete?: () => void;
  popoverClassName?: string;
  buttonVariant?: ButtonVariant;
}

const GroupActionsMenu = (props: GroupActionsMenuProps) => {
  return (
    <PopoverProvider>
      <GroupActionsMenuInner {...props} />
    </PopoverProvider>
  );
};

const GroupActionsMenuInner = ({
  memberDeviceIds: propMemberDeviceIds,
  collectionId,
  onEditGroup,
  onActionComplete,
  popoverClassName,
  buttonVariant = variants.secondary,
}: GroupActionsMenuProps) => {
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const [isOpen, setIsOpen] = useState(false);

  // Lazy-fetched member IDs for table context (when collectionId is provided but memberDeviceIds aren't)
  const [fetchedMemberIds, setFetchedMemberIds] = useState<string[] | null>(null);
  const [fetchingMembers, setFetchingMembers] = useState(false);
  const { listGroupMembers } = useCollections();

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

  // Lazy fetch member IDs when opening the menu in table context
  // Always refetch on open so membership changes are picked up
  const handleOpen = useCallback(() => {
    setIsOpen((prev) => {
      const opening = !prev;
      if (opening && !propMemberDeviceIds && collectionId && !fetchingMembers) {
        setFetchedMemberIds(null);
        setFetchingMembers(true);
        listGroupMembers({
          collectionId,
          onSuccess: (ids) => {
            setFetchedMemberIds(ids);
            setFetchingMembers(false);
          },
          onError: () => {
            setFetchingMembers(false);
          },
        });
      }
      return opening;
    });
  }, [propMemberDeviceIds, collectionId, fetchingMembers, listGroupMembers]);

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
    onActionComplete,
  });

  // Customize actions for group context:
  // 1. Filter out "Add to group" (already in a group)
  // 2. Insert "Edit group" after the cooling mode divider
  // 3. Rename "Delete" to "Unpair"
  const groupPopoverActions = useMemo(() => {
    const filtered = popoverActions.filter((a) => a.action !== groupActions.addToGroup);

    const editGroupAction: BulkAction<GroupActionType> = {
      action: "edit-group",
      title: "Edit group",
      icon: <Edit />,
      actionHandler: () => {
        setIsOpen(false);
        onEditGroup();
      },
      requiresConfirmation: false,
      showGroupDivider: true,
    };

    // Insert "Edit group" where the organization section was (after cooling mode's divider)
    const coolingModeIndex = filtered.findIndex((a) => a.action === settingsActions.coolingMode);
    const result =
      coolingModeIndex !== -1
        ? [
            ...filtered.slice(0, coolingModeIndex),
            filtered[coolingModeIndex],
            editGroupAction,
            ...filtered.slice(coolingModeIndex + 1),
          ]
        : [editGroupAction, ...filtered];

    // Rename "Delete" to "Unpair" and update confirmation copy
    return result.map((a) =>
      a.action === deviceActions.delete
        ? {
            ...a,
            title: "Unpair",
            ...(a.confirmation && {
              confirmation: {
                ...a.confirmation,
                title: a.confirmation.title.replace("Delete", "Unpair"),
                subtitle: a.confirmation.subtitle?.replace(/delet/gi, (m) => (m[0] === "D" ? "Unpair" : "unpair")),
                confirmAction: { ...a.confirmation.confirmAction, title: "Unpair" },
              },
            }),
          }
        : a,
    );
  }, [popoverActions, onEditGroup]);

  const poolMiners = useMemo(() => {
    if (poolFilteredDeviceIds) {
      return poolFilteredDeviceIds.map((id) => ({ deviceIdentifier: id }));
    }
    return selectedMinersWithStatus;
  }, [poolFilteredDeviceIds, selectedMinersWithStatus]);

  const [showWarnDialog, setShowWarnDialog] = useState(false);

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
          ariaLabel="Group actions"
          prefixIcon={<Ellipsis width={iconSizes.small} className="text-text-primary-70" />}
          onClick={(e) => {
            e.stopPropagation();
            handleOpen();
          }}
        />
        {isOpen &&
          (fetchingMembers ? (
            <div
              className={`popover-content absolute right-0 z-10 flex items-center justify-center rounded-2xl bg-surface-overlay p-6 shadow-elevation-200 ${popoverClassName ?? ""}`}
            >
              <ProgressCircular indeterminate />
            </div>
          ) : (
            <BulkActionsPopover<GroupActionType>
              actions={groupPopoverActions}
              beforeEach={handlePopoverAction}
              testId="group-actions-popover"
              position={positions["bottom right"]}
              className={popoverClassName ?? "!space-y-0 !rounded-2xl px-0 pt-2 pb-1"}
            />
          ))}
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
        open={showPoolSelectionPage && !!fleetCredentials}
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
        open={currentAction === performanceActions.managePower && showManagePowerModal}
        onConfirm={handleManagePowerConfirm}
        onDismiss={handleManagePowerDismiss}
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
    </>
  );
};

export default GroupActionsMenu;
