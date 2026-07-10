import { useCallback, useMemo, useRef, useState } from "react";
import PoolSelectionPageWrapper from "../ActionBar/SettingsWidget/PoolSelectionPage";
import BulkActionsWidget, { BulkActionsPopover } from "../BulkActions";
import { type BulkAction } from "../BulkActions/types";
import { insertActionAfter, insertActionBefore } from "./actionMenuUtils";
import { usePermittedActions } from "./actionPermissions";
import BulkRenameModal from "./BulkRenameModal";
import BulkWorkerNameModal from "./BulkWorkerNameModal";
import { deviceActions, groupActions, performanceActions, settingsActions, SupportedAction } from "./constants";
import MinerActionModalStack from "./MinerActionModalStack";
import MinerReparentPicker from "./MinerReparentPicker";
import { useMinerActions } from "./useMinerActions";
import type { SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  type MinerListFilter,
  type MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { useBatchActions } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";
import { ChevronDown, Edit, MiningPools, Plus } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import { type SelectionMode } from "@/shared/components/List";
import { PopoverProvider } from "@/shared/components/Popover";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface MinerActionsMenuProps {
  selectedMiners: string[];
  selectionMode: SelectionMode;
  /** Total count of all miners in fleet (used for "all" mode confirmation dialogs) */
  totalCount?: number;
  /**
   * Active scoped filter (URL chips ∩ SitePicker scope). In "all" mode, command
   * dispatch, capability checks, and pool assignment target this set across
   * pages; undefined means whole fleet.
   */
  currentFilter?: MinerListFilter;
  /** Active UI sort — forwarded so bulk actions can match visible table order. */
  currentSort?: SortConfig;
  /** Miner data keyed by device identifier, forwarded to bulk rename modals. */
  miners?: Record<string, MinerStateSnapshot>;
  /** Ordered list of miner device identifiers, forwarded to bulk rename modals. */
  minerIds?: string[];
  /**
   * When true, every action other than Unpair renders disabled. The parent
   * sets this for all-mode (the local miners map only carries the current page);
   * falls back to a subset check from `selectedMiners` + `miners`.
   */
  selectionIncludesUnauthenticatedMiner?: boolean;
  /** Callback to refetch miners after bulk rename or worker-name update. */
  onRefetchMiners?: () => void;
  onWorkerNameUpdated?: (deviceIdentifier: string, workerName: string) => void;
  onActionStart?: () => void;
  onActionComplete?: () => void;
}

type BulkWorkerNameTarget = {
  selectedMinerIds: string[];
  selectionMode: SelectionMode;
  originalSelectionMode: SelectionMode;
  totalCount?: number;
};

const MinerActionsMenu = ({
  selectedMiners,
  selectionMode,
  totalCount,
  currentFilter,
  currentSort,
  miners = {},
  minerIds = [],
  selectionIncludesUnauthenticatedMiner: selectionIncludesUnauthenticatedMinerOverride,
  onRefetchMiners,
  onWorkerNameUpdated,
  onActionStart,
  onActionComplete,
}: MinerActionsMenuProps) => {
  const { startBatchOperation, completeBatchOperation, removeDevicesFromBatch } = useBatchActions();
  const [showBulkRenameModal, setShowBulkRenameModal] = useState(false);
  const [showBulkWorkerNameModal, setShowBulkWorkerNameModal] = useState(false);
  const [showWorkerNameAuthenticateModal, setShowWorkerNameAuthenticateModal] = useState(false);
  const [bulkWorkerNameTarget, setBulkWorkerNameTarget] = useState<BulkWorkerNameTarget | null>(null);
  const workerNameCredentialsRef = useRef<{ username: string; password: string } | undefined>(undefined);
  const [reparentKind, setReparentKind] = useState<"rack" | "site" | "building" | null>(null);
  const { isPhone, isTablet } = useWindowDimensions();
  const selectedMinersWithStatus = useMemo(
    () => selectedMiners.map((id) => ({ deviceIdentifier: id })),
    [selectedMiners],
  );
  const selectedIdsIncludeUnauthenticatedMiner = useMemo(
    () => selectedMiners.some((id) => miners[id]?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED),
    [miners, selectedMiners],
  );
  const selectionIncludesUnauthenticatedMiner =
    selectionIncludesUnauthenticatedMinerOverride ?? selectedIdsIncludeUnauthenticatedMiner;

  const minerActionsResult = useMinerActions({
    selectedMiners: selectedMinersWithStatus,
    selectionMode,
    totalCount,
    currentFilter,
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
    poolFilteredDeviceIds,
    fleetCredentials,
    withCapabilityCheck,
    unsupportedMinersInfo,
    handleUnsupportedMinersContinue,
    handleUnsupportedMinersDismiss,
    displayCount,
  } = minerActionsResult;

  const handleWorkerNameFlowComplete = useCallback(() => {
    setShowBulkWorkerNameModal(false);
    setShowWorkerNameAuthenticateModal(false);
    setBulkWorkerNameTarget(null);
    workerNameCredentialsRef.current = undefined;
    onActionComplete?.();
  }, [onActionComplete]);

  const prepareBulkWorkerNameTarget = useCallback(
    (_filteredSelector?: unknown, filteredDeviceIds?: string[]) => {
      setBulkWorkerNameTarget({
        selectedMinerIds: filteredDeviceIds ?? selectedMiners,
        selectionMode: filteredDeviceIds ? "subset" : selectionMode,
        originalSelectionMode: selectionMode,
        totalCount: filteredDeviceIds ? filteredDeviceIds.length : totalCount,
      });
      setShowWorkerNameAuthenticateModal(true);
    },
    [selectedMiners, selectionMode, totalCount],
  );

  const handleBulkWorkerNamesOpen = useCallback(() => {
    onActionStart?.();
    void withCapabilityCheck(settingsActions.updateWorkerNames, prepareBulkWorkerNameTarget);
  }, [onActionStart, prepareBulkWorkerNameTarget, withCapabilityCheck]);

  const getWorkerNameCredentials = useCallback(() => workerNameCredentialsRef.current, []);

  const actionsWithBulkRename = useMemo(() => {
    const renameAction: BulkAction<SupportedAction> = {
      action: settingsActions.rename,
      title: "Rename",
      icon: <Edit />,
      actionHandler: () => {
        setShowBulkRenameModal(true);
        onActionStart?.();
      },
      requiresConfirmation: false,
    };

    const updateWorkerNamesAction: BulkAction<SupportedAction> = {
      action: settingsActions.updateWorkerNames,
      title: "Update worker names",
      icon: <MiningPools />,
      actionHandler: handleBulkWorkerNamesOpen,
      requiresConfirmation: false,
    };

    // Inserted before addToGroup so the cluster reads site → building → rack → group.
    const addToRackAction: BulkAction<SupportedAction> = {
      action: groupActions.addToRack,
      title: "Add to rack",
      icon: <Plus />,
      actionHandler: () => setReparentKind("rack"),
      requiresConfirmation: false,
    };
    const addToBuildingAction: BulkAction<SupportedAction> = {
      action: groupActions.addToBuilding,
      title: "Add to building",
      icon: <Plus />,
      actionHandler: () => setReparentKind("building"),
      requiresConfirmation: false,
    };
    const addToSiteAction: BulkAction<SupportedAction> = {
      action: groupActions.addToSite,
      title: "Add to site",
      icon: <Plus />,
      actionHandler: () => setReparentKind("site"),
      requiresConfirmation: false,
    };

    const actions = insertActionAfter(popoverActions, settingsActions.miningPool, updateWorkerNamesAction);
    const actionsWithRenameBeforeGroup = insertActionBefore(actions, groupActions.addToGroup, renameAction);

    const baseActions = actionsWithRenameBeforeGroup !== actions ? actionsWithRenameBeforeGroup : actions;
    const withAddToRack = insertActionBefore(baseActions, groupActions.addToGroup, addToRackAction);
    const withAddToBuilding = insertActionBefore(withAddToRack, groupActions.addToRack, addToBuildingAction);
    const withAddToSite = insertActionBefore(withAddToBuilding, groupActions.addToBuilding, addToSiteAction);

    if (actionsWithRenameBeforeGroup !== actions) {
      return withAddToSite;
    }

    const actionsWithRenameBeforeSecurity = insertActionBefore(withAddToSite, settingsActions.security, {
      ...renameAction,
      showGroupDivider: true,
    });

    if (actionsWithRenameBeforeSecurity !== withAddToSite) {
      return actionsWithRenameBeforeSecurity;
    }

    return [...withAddToSite, renameAction];
  }, [handleBulkWorkerNamesOpen, onActionStart, popoverActions]);

  const permittedActions = usePermittedActions(actionsWithBulkRename);

  const visibleActions = useMemo(() => {
    if (!selectionIncludesUnauthenticatedMiner) return permittedActions;
    return permittedActions.map((action) =>
      action.action === deviceActions.unpair
        ? action
        : {
            ...action,
            disabled: true,
            disabledReason: "Selection includes miners that need authentication.",
          },
    );
  }, [permittedActions, selectionIncludesUnauthenticatedMiner]);

  const poolMiners = useMemo(() => {
    if (poolFilteredDeviceIds) {
      return poolFilteredDeviceIds.map((id) => ({ deviceIdentifier: id }));
    }
    return selectedMinersWithStatus;
  }, [poolFilteredDeviceIds, selectedMinersWithStatus]);

  const showQuickActions = !isPhone && !isTablet;
  const quickActions = useMemo(() => {
    const quickActionOrder: SupportedAction[] = [
      deviceActions.blinkLEDs,
      deviceActions.reboot,
      performanceActions.managePower,
    ];
    const actionMap = new Map(visibleActions.map((action) => [action.action, action]));

    return quickActionOrder.flatMap((actionKey) => {
      const action = actionMap.get(actionKey);
      return action ? [action] : [];
    });
  }, [visibleActions]);

  return (
    <PopoverProvider>
      <div className="flex flex-wrap justify-start gap-3">
        <BulkActionsWidget<SupportedAction>
          buttonIconSuffix={<ChevronDown width={iconSizes.xSmall} />}
          buttonTitle={showQuickActions ? "More" : "Actions"}
          actions={visibleActions}
          onConfirmation={handleConfirmation}
          onCancel={handleCancel}
          currentAction={currentAction}
          renderQuickActions={(onAction) =>
            showQuickActions
              ? quickActions.map((action) => {
                  const isDisabled = action.disabled === true;
                  return (
                    <span
                      key={action.action}
                      title={isDisabled ? action.disabledReason : undefined}
                      className="inline-flex"
                    >
                      <Button
                        className="bg-grayscale-white-10! text-grayscale-white-90!"
                        size={sizes.compact}
                        variant={variants.secondary}
                        testId={`actions-menu-quick-action-${action.action}`}
                        disabled={isDisabled}
                        onClick={() => onAction(action)}
                      >
                        {action.title}
                      </Button>
                    </span>
                  );
                })
              : null
          }
          renderPopover={(beforeEach, closePopover) => (
            <BulkActionsPopover<SupportedAction>
              actions={visibleActions}
              beforeEach={beforeEach}
              testId="actions-menu-popover"
              closePopover={closePopover}
            />
          )}
          testId="actions-menu"
          unsupportedMinersInfo={unsupportedMinersInfo}
          onUnsupportedMinersContinue={handleUnsupportedMinersContinue}
          onUnsupportedMinersDismiss={handleUnsupportedMinersDismiss}
        />
      </div>
      <PoolSelectionPageWrapper
        open={showPoolSelectionPage ? !!fleetCredentials : false}
        selectedMiners={poolMiners}
        // Once capability filtering narrows to explicit supported ids, dispatch
        // to exactly those (subset) rather than re-expanding via the selector.
        selectionMode={poolFilteredDeviceIds ? "subset" : selectionMode}
        // Scoped/filtered "all" pool assignment resolves the set server-side
        // (all_matching_filter); only when not already narrowed to explicit ids.
        minerListFilter={selectionMode === "all" && !poolFilteredDeviceIds ? currentFilter : undefined}
        poolNeededCount={poolFilteredDeviceIds ? poolFilteredDeviceIds.length : totalCount}
        userUsername={fleetCredentials?.username}
        userPassword={fleetCredentials?.password}
        onSuccess={handleMiningPoolSuccess}
        onError={handleMiningPoolError}
        onWarning={handleMiningPoolWarning}
        onDismiss={handleCancel}
      />
      <MinerActionModalStack
        minerActions={minerActionsResult}
        selectedMinerIds={selectedMiners}
        selectionMode={selectionMode}
        displayCount={displayCount}
      />
      {/* The second AuthenticateFleetModal is specific to the worker-name
          flow, which only this menu hosts — keep it inline. */}
      <AuthenticateFleetModal
        open={showWorkerNameAuthenticateModal}
        purpose="workerNames"
        onAuthenticated={(username, password) => {
          workerNameCredentialsRef.current = { username, password };
          setShowWorkerNameAuthenticateModal(false);
          setShowBulkWorkerNameModal(true);
        }}
        onDismiss={handleWorkerNameFlowComplete}
      />
      <BulkRenameModal
        open={showBulkRenameModal}
        selectedMinerIds={selectedMiners}
        selectionMode={selectionMode}
        totalCount={totalCount}
        currentFilter={currentFilter}
        currentSort={currentSort}
        miners={miners}
        minerIds={minerIds}
        onRefetchMiners={onRefetchMiners}
        onDismiss={() => {
          setShowBulkRenameModal(false);
          onActionComplete?.();
        }}
      />
      <BulkWorkerNameModal
        open={showBulkWorkerNameModal}
        selectedMinerIds={bulkWorkerNameTarget?.selectedMinerIds ?? selectedMiners}
        selectionMode={bulkWorkerNameTarget?.selectionMode ?? selectionMode}
        originalSelectionMode={bulkWorkerNameTarget?.originalSelectionMode ?? selectionMode}
        totalCount={bulkWorkerNameTarget?.totalCount ?? totalCount}
        currentFilter={currentFilter}
        currentSort={currentSort}
        miners={miners}
        minerIds={minerIds}
        onRefetchMiners={onRefetchMiners}
        onWorkerNameUpdated={onWorkerNameUpdated}
        getWorkerNameCredentials={getWorkerNameCredentials}
        onDismiss={handleWorkerNameFlowComplete}
      />
      {reparentKind ? (
        <MinerReparentPicker
          kind={reparentKind}
          deviceIdentifiers={selectedMiners}
          selectionMode={selectionMode === "all" ? "all" : "subset"}
          currentFilter={currentFilter}
          totalCount={totalCount}
          miners={miners}
          sourceLabel={
            selectionMode === "all" && totalCount !== undefined
              ? `${totalCount} ${totalCount === 1 ? "miner" : "miners"}`
              : `${selectedMiners.length} ${selectedMiners.length === 1 ? "miner" : "miners"}`
          }
          successMessage={(count, target) => {
            if (target === "site") return `Moved ${count} miners to selected site.`;
            if (target === "building") return `Moved ${count} miners to selected building.`;
            return `Added ${count} miners to selected rack.`;
          }}
          onClose={() => setReparentKind(null)}
          onRefetchMiners={onRefetchMiners}
        />
      ) : null}
    </PopoverProvider>
  );
};

export default MinerActionsMenu;
