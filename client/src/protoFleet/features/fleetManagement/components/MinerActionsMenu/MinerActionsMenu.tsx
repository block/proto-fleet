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
import { useMinerActions } from "./useMinerActions";
import type { SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  type MinerListFilter,
  type MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useSites } from "@/protoFleet/api/sites";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import ParentPickerModal from "@/protoFleet/components/ParentPickerModal";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { useBatchActions } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";
import { ChevronDown, Edit, MiningPools, Plus } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import { type SelectionMode } from "@/shared/components/List";
import { PopoverProvider } from "@/shared/components/Popover";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface MinerActionsMenuProps {
  selectedMiners: string[];
  selectionMode: SelectionMode;
  /** Total count of all miners in fleet (used for "all" mode confirmation dialogs) */
  totalCount?: number;
  /** Active UI filter — forwarded for "all" mode unpair */
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
  // Re-parent picker target. null = closed; otherwise the picker is
  // open for that kind. Dispatch fires on confirm against the selected
  // miners (or full-set when selectionMode === "all").
  const [reparentKind, setReparentKind] = useState<"rack" | "site" | null>(null);
  const { isPhone, isTablet } = useWindowDimensions();
  const selectedMinersWithStatus = useMemo(
    () => selectedMiners.map((id) => ({ deviceIdentifier: id })),
    [selectedMiners],
  );
  // Subset-mode fallback when the parent omits the prop.
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
  // Modals shared with FleetGroupActionsMenu + SingleMinerActionsMenu are
  // rendered by MinerActionModalStack via the full hook result. Local
  // destructure pulls only the fields this shell still references
  // directly (popover wiring, pool flow, capability check, unsupported
  // miners modal, worker-name auth flow).
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

    // Re-parent openers — opening the picker only; dispatch happens
    // on confirm against the current selection. Inserted before
    // addToGroup so the cluster reads site → rack → group (the
    // canonical order; building is deferred pending backend RPC).
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

    const actions = insertActionAfter(popoverActions, settingsActions.miningPool, updateWorkerNamesAction);
    const actionsWithRenameBeforeGroup = insertActionBefore(actions, groupActions.addToGroup, renameAction);

    const baseActions = actionsWithRenameBeforeGroup !== actions ? actionsWithRenameBeforeGroup : actions;
    // Order is enforced by inserting rack first, then site before
    // rack — addToGroup remains the trailing entry (its existing
    // showGroupDivider closes the cluster).
    const withAddToRack = insertActionBefore(baseActions, groupActions.addToGroup, addToRackAction);
    const withAddToSite = insertActionBefore(withAddToRack, groupActions.addToRack, addToSiteAction);

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

  // Hide actions whose backing RPC the caller can't invoke. The server
  // still enforces every gate; this filter is UX so the menu doesn't
  // surface options that 403 on click.
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
          renderPopover={(beforeEach) => (
            <BulkActionsPopover<SupportedAction>
              actions={visibleActions}
              beforeEach={beforeEach}
              testId="actions-menu-popover"
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
        selectionMode={selectionMode}
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
      <ReparentPicker
        kind={reparentKind}
        selectedMinerIds={selectedMiners}
        onClose={() => setReparentKind(null)}
        onRefetchMiners={onRefetchMiners}
      />
    </PopoverProvider>
  );
};

// Re-parent picker + dispatcher pair. Split out as a child so the
// hooks for sites / device_set APIs only mount when a re-parent action
// fires (otherwise idle in every MinerActionsMenu render).
interface ReparentPickerProps {
  kind: "rack" | "site" | null;
  selectedMinerIds: string[];
  onClose: () => void;
  onRefetchMiners?: () => void;
}

const ReparentPicker = ({ kind, selectedMinerIds, onClose, onRefetchMiners }: ReparentPickerProps) => {
  const { reassignDevicesToSite } = useSites();
  const { addDevicesToDeviceSet } = useDeviceSets();

  const sourceLabel = `${selectedMinerIds.length} ${selectedMinerIds.length === 1 ? "miner" : "miners"}`;

  if (!kind) return null;

  return (
    <ParentPickerModal
      kind={kind}
      show
      selectionMode="single"
      sourceLabel={sourceLabel}
      onDismiss={onClose}
      onConfirm={(targetIds) => {
        const targetId = targetIds[0];
        onClose();
        if (targetId === undefined) return;
        if (selectedMinerIds.length === 0) {
          pushToast({ message: "No miners selected.", status: STATUSES.queued });
          return;
        }
        if (kind === "site") {
          void reassignDevicesToSite({
            targetSiteId: targetId,
            deviceIdentifiers: selectedMinerIds,
            onSuccess: (count) => {
              pushToast({ message: `Moved ${count} miners to selected site.`, status: STATUSES.success });
              onRefetchMiners?.();
            },
            onError: (msg) => pushToast({ message: `Couldn't move miners: ${msg}`, status: STATUSES.error }),
          });
          return;
        }
        // kind === "rack"
        void addDevicesToDeviceSet({
          deviceSetId: targetId,
          deviceIdentifiers: selectedMinerIds,
          onSuccess: (count) => {
            pushToast({ message: `Added ${count} miners to selected rack.`, status: STATUSES.success });
            onRefetchMiners?.();
          },
          onError: (msg) => pushToast({ message: `Couldn't add miners to rack: ${msg}`, status: STATUSES.error }),
        });
      }}
    />
  );
};

export default MinerActionsMenu;
