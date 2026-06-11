import { type ReactElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import BulkActionConfirmDialog from "../BulkActions/BulkActionConfirmDialog";
import UnsupportedMinersModal from "../BulkActions/UnsupportedMinersModal";
import RowActionsMenu, { type RowAction } from "../RowActionsMenu";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  MinerListFilterSchema,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import PoolSelectionPageWrapper from "@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage";
import { ACTION_PERMISSIONS } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/actionPermissions";
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
import { usePermissions } from "@/protoFleet/store";
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
  // Rendered between the wired top + bottom bulk clusters.
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

// Mapped to `showGroupDivider: true` on the PREVIOUS row in the
// merged action list (RowActionsMenu renders dividers after the row).
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

const MAX_SNAPSHOT_PAGES = 50;
const SNAPSHOT_PAGE_SIZE = 1000;
const MAX_MINERS = MAX_SNAPSHOT_PAGES * SNAPSHOT_PAGE_SIZE;

const FleetGroupActionsMenu = ({ scope, ariaLabel, testIdPrefix, extraActions = [] }: FleetGroupActionsMenuProps) => {
  // Scoped miners — fetched lazily on first action click, re-fetched
  // each click to catch membership changes from unpair / assignment.
  const [ids, setIds] = useState<string[]>([]);
  // Snapshots feed useMinerActions's per-miner gates (firmware model
  // compatibility, unauthenticated-miner pairing status, etc.).
  const [minerSnapshots, setMinerSnapshots] = useState<Record<string, MinerStateSnapshot>>({});
  const idsLoadedRef = useRef(false);
  const [isBusy, setIsBusy] = useState(false);

  // `tick` forces the effect to re-run when the same action is clicked
  // twice — React skips equal state updates so direct (non-modal)
  // dispatches like Download logs would otherwise never refire.
  const [pendingAction, setPendingAction] = useState<{ key: WiredActionKey; tick: number } | null>(null);
  const tickRef = useRef(0);
  const firedTickRef = useRef(-1);

  const { startBatchOperation, completeBatchOperation, removeDevicesFromBatch } = useBatchActions();
  const selectedMiners = useMemo(() => ids.map((id) => ({ deviceIdentifier: id })), [ids]);
  const minerActions = useMinerActions({
    selectedMiners,
    selectionMode: "subset",
    startBatchOperation,
    completeBatchOperation,
    removeDevicesFromBatch,
    miners: minerSnapshots,
  });
  const permissions = usePermissions();

  // Mirrors `usePermittedActions` from MinerActionsMenu so read-only
  // roles don't see entries that would 403 on click. Server enforces.
  const permittedKeys = useMemo(() => {
    const allowed = new Set<WiredActionKey>();
    const lookup: Record<string, string | readonly string[] | undefined> = ACTION_PERMISSIONS;
    const hasAll = (required: string | readonly string[] | undefined): boolean => {
      if (required === undefined) return true;
      if (typeof required === "string") return permissions.includes(required);
      return required.every((key) => permissions.includes(key));
    };
    for (const key of [...TOP_WIRED_KEYS, ...BOTTOM_WIRED_KEYS]) {
      if (hasAll(lookup[key])) allowed.add(key);
    }
    return allowed;
  }, [permissions]);

  const fetchDeviceIds = useCallback(async (): Promise<string[]> => {
    const collected: string[] = [];
    const snapshotMap: Record<string, MinerStateSnapshot> = {};
    const filterInit =
      scope.kind === "building"
        ? { buildingIds: [scope.id] }
        : scope.kind === "rack"
          ? { rackIds: [scope.id] }
          : { siteIds: [scope.id] };
    const filter = create(MinerListFilterSchema, filterInit);
    let cursor = "";
    let exhausted = false;
    for (let i = 0; i < MAX_SNAPSHOT_PAGES; i++) {
      const response = await fleetManagementClient.listMinerStateSnapshots({
        pageSize: SNAPSHOT_PAGE_SIZE,
        cursor,
        filter,
      });
      for (const miner of response.miners) {
        collected.push(miner.deviceIdentifier);
        snapshotMap[miner.deviceIdentifier] = miner;
      }
      if (!response.cursor) {
        exhausted = true;
        break;
      }
      cursor = response.cursor;
    }
    if (!exhausted) {
      throw new Error(`Too many miners in ${scope.name} (over ${MAX_MINERS}). Filter the list and try again.`);
    }
    idsLoadedRef.current = true;
    setIds(collected);
    setMinerSnapshots(snapshotMap);
    return collected;
  }, [scope.id, scope.kind, scope.name]);

  useEffect(() => {
    if (!pendingAction) return;
    if (ids.length === 0) return;
    if (firedTickRef.current === pendingAction.tick) return;
    const entry = minerActions.popoverActions.find((action) => action.action === pendingAction.key);
    if (!entry) return;
    firedTickRef.current = pendingAction.tick;
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
      } catch (err) {
        const message = err instanceof Error && err.message ? err.message : `Couldn't load miners for ${scope.name}.`;
        updateToast(loadingToast, { message, status: STATUSES.error });
        setIsBusy(false);
        return;
      }
      removeToast(loadingToast);
      setIsBusy(false);
      if (deviceIdentifiers.length === 0) {
        pushToast({ message: `No miners in ${scope.name}.`, status: STATUSES.queued });
        return;
      }
      tickRef.current += 1;
      setPendingAction({ key, tick: tickRef.current });
    },
    [fetchDeviceIds, isBusy, scope.name],
  );

  const clearPendingAction = useCallback(() => {
    setPendingAction(null);
    idsLoadedRef.current = false;
  }, []);

  const handleConfirmClick = useCallback(() => {
    clearPendingAction();
    void minerActions.handleConfirmation();
  }, [clearPendingAction, minerActions]);

  const handleCancelClick = useCallback(() => {
    clearPendingAction();
    minerActions.handleCancel();
  }, [clearPendingAction, minerActions]);

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

  // Pre-fetch: render all wired entries from local label/icon tables.
  // Post-fetch: honor the hook's selection-derived filter (e.g. wakeUp
  // drops when no selected miners are INACTIVE).
  const popoverActions = minerActions.popoverActions;
  const popoverActionByKey = useMemo(() => {
    const map = new Map<SupportedAction, (typeof popoverActions)[number]>();
    for (const action of popoverActions) map.set(action.action, action);
    return map;
  }, [popoverActions]);

  const keepEntry = useCallback(
    (key: WiredActionKey) => {
      if (!permittedKeys.has(key)) return false;
      if (key === deviceActions.wakeUp && !idsLoadedRef.current) return true;
      return idsLoadedRef.current ? popoverActionByKey.has(key) : true;
    },
    [permittedKeys, popoverActionByKey],
  );

  const topWiredEntries = useMemo(() => TOP_WIRED_KEYS.filter(keepEntry), [keepEntry]);
  const bottomWiredEntries = useMemo(() => BOTTOM_WIRED_KEYS.filter(keepEntry), [keepEntry]);
  const visibleExtraActions = useMemo(() => extraActions.filter((entry) => !entry.hidden), [extraActions]);

  // Cluster boundary rules: divider between top↔extras and (when no
  // extras) top↔bottom; never between extras↔bottom so Edit and
  // Add-to-group share a cluster. Internal dividers from DIVIDER_BEFORE_KEY.
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
            pendingAction?.key === action.action &&
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
        // Use the capability-filtered subset when the unsupported-miners
        // gate narrowed the selection; otherwise dispatch against the
        // full scoped set. Mirrors the pattern in MinerActionsMenu.
        selectedMiners={
          minerActions.poolFilteredDeviceIds
            ? minerActions.poolFilteredDeviceIds.map((id) => ({ deviceIdentifier: id }))
            : selectedMiners
        }
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
