import { type ReactElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import BulkActionConfirmDialog from "../BulkActions/BulkActionConfirmDialog";
import UnsupportedMinersModal from "../BulkActions/UnsupportedMinersModal";
import RowActionsMenu, { type RowAction } from "../RowActionsMenu";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import PoolSelectionPageWrapper from "@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage";
import {
  deviceActions,
  groupActions,
  performanceActions,
  settingsActions,
  type SupportedAction,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";
import MinerActionModalStack from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/MinerActionModalStack";
import { useMinerActions } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/useMinerActions";
import { useBatchActions } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";
import {
  Lock,
  MiningPools,
  Play,
  Plus,
  Power,
  Reboot,
  Settings,
  Speedometer,
  Terminal,
  Unpair,
} from "@/shared/assets/icons";
import { pushToast, removeToast, STATUSES, updateToast } from "@/shared/features/toaster";

export type GroupScope = {
  kind: "site" | "building" | "rack";
  id: bigint;
  name: string;
};

interface FleetGroupActionsMenuProps {
  scope: GroupScope;
  ariaLabel: string;
  testIdPrefix?: string;
  // Host-composed row actions (view / edit / add) rendered between the
  // wired top + bottom bulk clusters.
  extraActions?: RowAction[];
}

const TOP_WIRED_KEYS = [
  deviceActions.shutdown,
  deviceActions.wakeUp,
  deviceActions.reboot,
  deviceActions.downloadLogs,
  performanceActions.managePower,
  deviceActions.firmwareUpdate,
  settingsActions.miningPool,
] as const;

const BOTTOM_WIRED_KEYS = [groupActions.addToGroup, settingsActions.security, deviceActions.unpair] as const;

type WiredActionKey = (typeof TOP_WIRED_KEYS)[number] | (typeof BOTTOM_WIRED_KEYS)[number];

// Keys that historically rendered a thick divider IMMEDIATELY BEFORE
// their row. The merger below converts each such entry into a
// `showGroupDivider: true` on the PREVIOUS row (since RowActionsMenu
// renders dividers after, not before).
const DIVIDER_BEFORE_KEY: ReadonlySet<WiredActionKey> = new Set<WiredActionKey>([
  performanceActions.managePower,
  settingsActions.security,
]);

const ACTION_LABEL: Record<WiredActionKey, string> = {
  [deviceActions.shutdown]: "Sleep miners",
  [deviceActions.wakeUp]: "Wake miners",
  [deviceActions.reboot]: "Reboot miners",
  [deviceActions.downloadLogs]: "Download logs",
  [performanceActions.managePower]: "Manage power",
  [deviceActions.firmwareUpdate]: "Update firmware",
  [settingsActions.miningPool]: "Edit pool",
  [groupActions.addToGroup]: "Add to group",
  [settingsActions.security]: "Manage security",
  [deviceActions.unpair]: "Unpair miners",
};

const ACTION_ICON: Record<WiredActionKey, ReactElement> = {
  [deviceActions.shutdown]: <Power />,
  [deviceActions.wakeUp]: <Play />,
  [deviceActions.reboot]: <Reboot />,
  [deviceActions.downloadLogs]: <Terminal />,
  [performanceActions.managePower]: <Speedometer />,
  [deviceActions.firmwareUpdate]: <Settings />,
  [settingsActions.miningPool]: <MiningPools />,
  [groupActions.addToGroup]: <Plus />,
  [settingsActions.security]: <Lock />,
  [deviceActions.unpair]: <Unpair />,
};

const FleetGroupActionsMenu = ({ scope, ariaLabel, testIdPrefix, extraActions = [] }: FleetGroupActionsMenuProps) => {
  // Lazy-fetched on first action click; ref tracks "fetched yet" so
  // repeat clicks skip the network.
  const [ids, setIds] = useState<string[]>([]);
  const idsLoadedRef = useRef(false);
  const [isBusy, setIsBusy] = useState(false);

  // Action is deferred via state because useMinerActions rebuilds
  // popoverActions asynchronously after `ids` lands — the effect below
  // resolves a fresh handler once both have caught up. Also gates
  // BulkActionConfirmDialog so the dialog stays open until the operator
  // confirms or cancels.
  const [pendingAction, setPendingAction] = useState<WiredActionKey | null>(null);
  const firedActionRef = useRef<WiredActionKey | null>(null);

  const { startBatchOperation, completeBatchOperation, removeDevicesFromBatch } = useBatchActions();
  const selectedMiners = useMemo(() => ids.map((id) => ({ deviceIdentifier: id })), [ids]);
  const minerActions = useMinerActions({
    selectedMiners,
    selectionMode: "subset",
    startBatchOperation,
    completeBatchOperation,
    removeDevicesFromBatch,
  });

  const fetchDeviceIds = useCallback(async (): Promise<string[]> => {
    if (idsLoadedRef.current) return ids;
    const collected: string[] = [];
    const filterInit =
      scope.kind === "building"
        ? { buildingIds: [scope.id] }
        : scope.kind === "rack"
          ? { rackIds: [scope.id] }
          : { siteIds: [scope.id] };
    const filter = create(MinerListFilterSchema, filterInit);
    let cursor = "";
    // Safety cap — 50k miners well exceeds any realistic cohort.
    for (let i = 0; i < 50; i++) {
      const response = await fleetManagementClient.listMinerStateSnapshots({
        pageSize: 1000,
        cursor,
        filter,
      });
      for (const miner of response.miners) collected.push(miner.deviceIdentifier);
      if (!response.cursor) break;
      cursor = response.cursor;
    }
    idsLoadedRef.current = true;
    setIds(collected);
    return collected;
  }, [ids, scope.id, scope.kind]);

  useEffect(() => {
    if (!pendingAction) {
      firedActionRef.current = null;
      return;
    }
    if (ids.length === 0) return;
    if (firedActionRef.current === pendingAction) return;
    const entry = minerActions.popoverActions.find((action) => action.action === pendingAction);
    if (!entry) return;
    firedActionRef.current = pendingAction;
    void entry.actionHandler();
  }, [pendingAction, ids, minerActions.popoverActions]);

  const handleTrigger = useCallback(
    async (key: WiredActionKey) => {
      if (isBusy) return;
      setIsBusy(true);
      const loadingToast = pushToast({
        message: `Loading miners in ${scope.name}…`,
        status: STATUSES.loading,
        longRunning: true,
      });
      let deviceIdentifiers: string[];
      try {
        deviceIdentifiers = await fetchDeviceIds();
      } catch {
        updateToast(loadingToast, {
          message: `Couldn't load miners for ${scope.name}.`,
          status: STATUSES.error,
        });
        setIsBusy(false);
        return;
      }
      removeToast(loadingToast);
      setIsBusy(false);
      if (deviceIdentifiers.length === 0) {
        pushToast({ message: `No miners in ${scope.name}.`, status: STATUSES.queued });
        return;
      }
      // Re-arm even if same action — lets the dispatcher run again.
      firedActionRef.current = null;
      setPendingAction(key);
    },
    [fetchDeviceIds, isBusy, scope.name],
  );

  const clearPendingAction = useCallback(() => {
    setPendingAction(null);
    firedActionRef.current = null;
  }, []);

  const handleConfirmClick = useCallback(() => {
    clearPendingAction();
    void minerActions.handleConfirmation();
  }, [clearPendingAction, minerActions]);

  const handleCancelClick = useCallback(() => {
    clearPendingAction();
    minerActions.handleCancel();
  }, [clearPendingAction, minerActions]);

  // Pool flow stays inline because PoolSelectionPageWrapper's
  // selectedMiners / poolNeededCount vary per callsite.
  const handlePoolFlowDismiss = useCallback(() => {
    clearPendingAction();
    minerActions.handleCancel();
  }, [clearPendingAction, minerActions]);

  const handlePoolFlowComplete = useCallback(
    (batchIdentifier: string, dispatched: string[]) => {
      clearPendingAction();
      minerActions.handleMiningPoolSuccess(batchIdentifier, dispatched);
    },
    [clearPendingAction, minerActions],
  );

  // Before ids load we render all wired entries from local label/icon
  // tables; once loaded we honor the hook's selection-derived filter
  // (e.g. wakeUp drops when no miners are INACTIVE).
  const popoverActions = minerActions.popoverActions;
  const popoverActionByKey = useMemo(() => {
    const map = new Map<SupportedAction, (typeof popoverActions)[number]>();
    for (const action of popoverActions) map.set(action.action, action);
    return map;
  }, [popoverActions]);

  const keepEntry = useCallback(
    (key: WiredActionKey) => {
      if (key === deviceActions.wakeUp && !idsLoadedRef.current) return true;
      return idsLoadedRef.current ? popoverActionByKey.has(key) : true;
    },
    [popoverActionByKey],
  );

  const topWiredEntries = useMemo(() => TOP_WIRED_KEYS.filter(keepEntry), [keepEntry]);
  const bottomWiredEntries = useMemo(() => BOTTOM_WIRED_KEYS.filter(keepEntry), [keepEntry]);
  const visibleExtraActions = useMemo(() => extraActions.filter((entry) => !entry.hidden), [extraActions]);

  // Build the merged RowAction[] used by the shared RowActionsMenu.
  // Cluster boundary rules:
  //   - top → extras: divider if extras exist
  //   - top → bottom: divider only when no extras (Edit + Add-to-group
  //     share a cluster by design)
  //   - extras → bottom: never (last extras `showGroupDivider` ignored)
  //   - inside top/bottom: divider before any entry in DIVIDER_BEFORE_KEY
  const rowActions: RowAction[] = useMemo(() => {
    const entries: RowAction[] = [];
    const fleetTestIdBase = testIdPrefix ?? "fleet-group-actions";

    topWiredEntries.forEach((key, i) => {
      const nextTop = topWiredEntries[i + 1];
      const isLastTop = i === topWiredEntries.length - 1;
      const dividerFromInternal = nextTop !== undefined && DIVIDER_BEFORE_KEY.has(nextTop);
      const dividerFromClusterBoundary =
        isLastTop &&
        (visibleExtraActions.length > 0 || (bottomWiredEntries.length > 0 && visibleExtraActions.length === 0));
      entries.push({
        label: ACTION_LABEL[key],
        icon: ACTION_ICON[key],
        testId: `${fleetTestIdBase}-${key}`,
        onClick: () => void handleTrigger(key),
        showGroupDivider: dividerFromInternal || dividerFromClusterBoundary,
      });
    });

    visibleExtraActions.forEach((action, i) => {
      const isLastExtra = i === visibleExtraActions.length - 1;
      entries.push({
        label: action.label,
        icon: action.icon,
        testId: action.testId,
        onClick: action.onClick,
        // Suppress the trailing divider on the last extras entry so
        // Edit + Add-to-group flow together visually.
        showGroupDivider: !isLastExtra && !!action.showGroupDivider,
      });
    });

    bottomWiredEntries.forEach((key, i) => {
      const nextBottom = bottomWiredEntries[i + 1];
      const dividerAfter = nextBottom !== undefined && DIVIDER_BEFORE_KEY.has(nextBottom);
      entries.push({
        label: ACTION_LABEL[key],
        icon: ACTION_ICON[key],
        testId: `${fleetTestIdBase}-${key}`,
        onClick: () => void handleTrigger(key),
        showGroupDivider: dividerAfter,
      });
    });

    return entries;
  }, [topWiredEntries, visibleExtraActions, bottomWiredEntries, handleTrigger, testIdPrefix]);

  return (
    <>
      <RowActionsMenu
        actions={rowActions}
        ariaLabel={ariaLabel}
        testIdPrefix={testIdPrefix ?? "fleet-group-actions"}
        disabled={isBusy}
      />
      <UnsupportedMinersModal
        open={minerActions.unsupportedMinersInfo.visible}
        unsupportedGroups={minerActions.unsupportedMinersInfo.unsupportedGroups}
        totalUnsupportedCount={minerActions.unsupportedMinersInfo.totalUnsupportedCount}
        noneSupported={minerActions.unsupportedMinersInfo.noneSupported}
        onContinue={minerActions.handleUnsupportedMinersContinue}
        onDismiss={minerActions.handleUnsupportedMinersDismiss}
      />
      {minerActions.popoverActions
        .filter((action) => action.requiresConfirmation && action.confirmation)
        .map((action) => {
          const open =
            minerActions.currentAction === action.action &&
            pendingAction === action.action &&
            !minerActions.unsupportedMinersInfo.visible;
          return (
            <BulkActionConfirmDialog
              key={action.action}
              open={open}
              actionConfirmation={action.confirmation!}
              onConfirmation={handleConfirmClick}
              onCancel={handleCancelClick}
              testId={`${testIdPrefix ?? "fleet-group-actions"}-${action.action}-confirm`}
            />
          );
        })}
      <PoolSelectionPageWrapper
        open={minerActions.showPoolSelectionPage ? !!minerActions.fleetCredentials : false}
        selectedMiners={selectedMiners}
        selectionMode="subset"
        userUsername={minerActions.fleetCredentials?.username}
        userPassword={minerActions.fleetCredentials?.password}
        onSuccess={handlePoolFlowComplete}
        onError={minerActions.handleMiningPoolError}
        onWarning={minerActions.handleMiningPoolWarning}
        onDismiss={handlePoolFlowDismiss}
      />
      <MinerActionModalStack
        minerActions={minerActions}
        selectedMinerIds={ids}
        selectionMode="subset"
        displayCount={ids.length}
        onActionBoundary={clearPendingAction}
      />
    </>
  );
};

export default FleetGroupActionsMenu;
