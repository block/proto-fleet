import { Fragment, type ReactElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import BulkActionConfirmDialog from "../BulkActions/BulkActionConfirmDialog";
import UnsupportedMinersModal from "../BulkActions/UnsupportedMinersModal";
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
  Ellipsis,
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
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { type RowAction } from "@/shared/components/RowActionsMenu";
import { positions } from "@/shared/constants";
import { pushToast, removeToast, STATUSES, updateToast } from "@/shared/features/toaster";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

export type GroupScope = {
  kind: "site" | "building" | "rack";
  id: bigint;
  name: string;
};

interface FleetGroupActionsMenuProps {
  scope: GroupScope;
  ariaLabel: string;
  testIdPrefix?: string;
  // Non-bulk row actions (view *, add *, ...) rendered below the
  // wired bulk-action group with a divider in between. The host page
  // composes these (navigation, modal callbacks) since they're not
  // part of the miner bulk-action vocabulary.
  extraActions?: RowAction[];
}

// Bulk actions wired through useMinerActions. Order matches Figma.
// Each entry resolves a popoverActions row at click time — the hook
// owns its label, icon, confirmation copy, dispatcher, modal flow,
// and per-device progress toast — so this list only ships ordering
// + local label/icon overrides (Figma wants "Sleep miners" instead
// of the hook's miner-list-flavored "Sleep"). Split into top + bottom
// clusters so the host's extras (View / Edit / Add / Assign) can land
// between them, matching the Figma menu shape.
const TOP_WIRED_KEYS = [
  deviceActions.shutdown,
  deviceActions.wakeUp,
  deviceActions.reboot,
  deviceActions.downloadLogs,
  performanceActions.managePower,
  deviceActions.firmwareUpdate,
  settingsActions.miningPool,
] as const;

// Bottom cluster runs below the host's view / edit extras. "Add to group"
// leads (re-parents the row's descendant miners into a group via the same
// modal as the miner-list bulk action). Manage security + Unpair close out
// the menu, separated by a divider so they read as a distinct cluster.
const BOTTOM_WIRED_KEYS = [groupActions.addToGroup, settingsActions.security, deviceActions.unpair] as const;

type WiredActionKey = (typeof TOP_WIRED_KEYS)[number] | (typeof BOTTOM_WIRED_KEYS)[number];

// Render a thick divider above any key whose entry is in this set.
// Mirrors the figma grouping: separates "destructive" from
// "perf/settings" in the top cluster, and "add to group" from
// "security/unpair" in the bottom cluster.
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

const FleetGroupActionsMenu = (props: FleetGroupActionsMenuProps) => (
  <PopoverProvider>
    <FleetGroupActionsMenuInner {...props} />
  </PopoverProvider>
);

const FleetGroupActionsMenuInner = ({
  scope,
  ariaLabel,
  testIdPrefix,
  extraActions = [],
}: FleetGroupActionsMenuProps) => {
  const { triggerRef, setPopoverRenderMode } = usePopover();
  useEffect(() => {
    setPopoverRenderMode("portal-fixed");
  }, [setPopoverRenderMode]);

  const [isOpen, setIsOpen] = useState(false);
  const onClickOutside = useCallback(() => setIsOpen(false), []);
  useClickOutside({
    ref: triggerRef,
    onClickOutside,
    ignoreSelectors: [".popover-content"],
  });

  // Device IDs land here once the first action click resolves the
  // listMinerStateSnapshots fetch. Stored as state so useMinerActions
  // rebuilds its popoverActions handlers against the freshly-loaded
  // selection; the ref tracks "have we fetched yet" so repeat clicks
  // skip the network.
  const [ids, setIds] = useState<string[]>([]);
  const idsLoadedRef = useRef(false);
  const [isBusy, setIsBusy] = useState(false);

  // Pending action set by handleTrigger; cleared once the operator
  // resolves the resulting modal (confirms / cancels / dismisses).
  // useEffect dispatch can't capture the latest popoverActions
  // closure synchronously, so we defer via state — by the time the
  // effect fires, popoverActions has been rebuilt and finds an
  // actionHandler bound to the right deviceSelector. Doubles as the
  // gate for BulkActionConfirmDialog (confirm-only actions) so the
  // dialog stays open between the handler firing currentAction and
  // the user clicking Confirm / Cancel.
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

  // Fire the queued action once popoverActions reflects the loaded
  // selection. The hook rebuilds popoverActions whenever
  // selectedMiners changes, so the find() below resolves to a fresh
  // actionHandler closed over the right deviceSelector.
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
      setIsOpen(false);
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
      // Re-arm the effect even if the previous click was the same
      // action — clearing the ref lets the dispatcher run again.
      firedActionRef.current = null;
      setPendingAction(key);
    },
    [fetchDeviceIds, isBusy, scope.name],
  );

  const clearPendingAction = useCallback(() => {
    setPendingAction(null);
    firedActionRef.current = null;
  }, []);

  // Confirmation-dialog handlers (Sleep / Wake / Reboot). The hook's
  // handleConfirmation runs the dispatch + batch stream + toast
  // updates internally.
  const handleConfirmClick = useCallback(() => {
    clearPendingAction();
    void minerActions.handleConfirmation();
  }, [clearPendingAction, minerActions]);

  const handleCancelClick = useCallback(() => {
    clearPendingAction();
    minerActions.handleCancel();
  }, [clearPendingAction, minerActions]);

  // Pool flow keeps its own dismiss + complete wrappers because
  // PoolSelectionPageWrapper is rendered inline here (not by the shared
  // modal stack — its selectedMiners / poolNeededCount props vary per
  // callsite, see MinerActionModalStack).
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

  // Resolve the action entries that actually exist in the hook's
  // popoverActions (some entries depend on selection state — for
  // example wakeUp only surfaces when at least one miner is INACTIVE
  // in the per-miner case; in subset mode both sleep + wake show).
  // Falls back to local label + icon when the hook hasn't seen
  // selectedMiners yet (ids haven't loaded) so the menu still renders.
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

  const triggerTestId = testIdPrefix ? `${testIdPrefix}-trigger` : "fleet-group-actions-trigger";
  const popoverTestId = testIdPrefix ? `${testIdPrefix}-popover` : "fleet-group-actions-popover";

  return (
    <div className="relative" ref={triggerRef}>
      <Button
        className="-my-[10px] !p-[14px]"
        size={sizes.compact}
        variant={variants.textOnly}
        prefixIcon={<Ellipsis width={iconSizes.small} className="text-text-primary-70" />}
        ariaLabel={ariaLabel}
        testId={triggerTestId}
        disabled={isBusy}
        onClick={() => setIsOpen((prev) => !prev)}
      />
      {isOpen ? (
        <Popover
          className="!space-y-0 !rounded-2xl px-0 pt-2 pb-1"
          position={positions["bottom right"]}
          size={popoverSizes.small}
          offset={8}
          testId={popoverTestId}
        >
          {topWiredEntries.map((key) => (
            <Fragment key={key}>
              {DIVIDER_BEFORE_KEY.has(key) ? <Divider dividerStyle="thick" /> : null}
              <div className="px-4">
                <Row
                  className="text-emphasis-300"
                  prefixIcon={ACTION_ICON[key]}
                  testId={`${testIdPrefix ?? "fleet-group-actions"}-${key}`}
                  onClick={() => {
                    void handleTrigger(key);
                  }}
                  compact
                  divider={false}
                >
                  {ACTION_LABEL[key]}
                </Row>
              </div>
            </Fragment>
          ))}
          {visibleExtraActions.length > 0 ? <Divider dividerStyle="thick" /> : null}
          {visibleExtraActions.map((action, index) => (
            <Fragment key={action.testId ?? action.label}>
              <div className="px-4">
                <Row
                  className="text-emphasis-300"
                  prefixIcon={action.icon}
                  testId={action.testId}
                  onClick={() => {
                    setIsOpen(false);
                    action.onClick();
                  }}
                  compact
                  divider={false}
                >
                  {action.label}
                </Row>
              </div>
              {action.showGroupDivider && index < visibleExtraActions.length - 1 ? (
                <Divider dividerStyle="thick" />
              ) : null}
            </Fragment>
          ))}
          {/* No always-on divider between extras and bottom: the host's
              extras end with the edit/add cluster, and addToGroup (the
              first bottom entry) belongs to that same cluster.
              DIVIDER_BEFORE_KEY handles the separator before security so
              the destructive cluster still reads distinct. */}
          {bottomWiredEntries.length > 0 && visibleExtraActions.length === 0 && topWiredEntries.length > 0 ? (
            <Divider dividerStyle="thick" />
          ) : null}
          {bottomWiredEntries.map((key) => (
            <Fragment key={key}>
              {DIVIDER_BEFORE_KEY.has(key) ? <Divider dividerStyle="thick" /> : null}
              <div className="px-4">
                <Row
                  className="text-emphasis-300"
                  prefixIcon={ACTION_ICON[key]}
                  testId={`${testIdPrefix ?? "fleet-group-actions"}-${key}`}
                  onClick={() => {
                    void handleTrigger(key);
                  }}
                  compact
                  divider={false}
                >
                  {ACTION_LABEL[key]}
                </Row>
              </div>
            </Fragment>
          ))}
        </Popover>
      ) : null}

      {/* Modal surfaces driven by useMinerActions internal state.
          currentAction guards mirror the SingleMinerActionsMenu
          pattern so a stale modal can't bleed between actions. */}
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
    </div>
  );
};

export default FleetGroupActionsMenu;
