import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";

import FleetGroupActionsMenu, { type GroupScope } from "./FleetGroupActionsMenu";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import TankActionsMenu from "@/protoFleet/features/containers/TankActionsMenu";
import { useSetActionBarVisible } from "@/protoFleet/store";
import ActionBar, { ActionBarSelectionControls } from "@/shared/components/ActionBar";
import { type RowAction } from "@/shared/components/RowActionsMenu";

interface FleetGroupListActionBarProps {
  selectedScopes: GroupScope[];
  kind: "site" | "building" | "rack";
  // Extra actions inserted into the bulk popover — used by lists that
  // need bulk reparent (e.g. Add racks to building / site).
  bulkExtraActions?: RowAction[];
  onClearSelection: () => void;
  onSelectAllVisible: () => void;
  onActionBusyChange?: (busy: boolean) => void;
  selectedTanks?: InventoryItem[];
  onRefetchTanks?: () => void;
}

const PLURAL_KIND: Record<FleetGroupListActionBarProps["kind"], string> = {
  site: "sites",
  building: "buildings",
  rack: "racks",
};

const FleetGroupListActionBar = ({
  selectedScopes,
  kind,
  bulkExtraActions,
  onClearSelection,
  onSelectAllVisible,
  onActionBusyChange,
  selectedTanks = [],
  onRefetchTanks,
}: FleetGroupListActionBarProps) => {
  const setActionBarVisible = useSetActionBarVisible();
  const [isActionBusy, setIsActionBusy] = useState(false);
  const selectionRef = useRef({ scopes: selectedScopes, tanks: selectedTanks });
  if (!isActionBusy) {
    selectionRef.current = { scopes: selectedScopes, tanks: selectedTanks };
  }
  const { scopes: activeSelectedScopes, tanks: activeSelectedTanks } = selectionRef.current;
  const selectedIds = useMemo(
    () => [
      ...activeSelectedScopes.map((scope) => `${scope.kind}:${scope.id}`),
      ...activeSelectedTanks.map((tank) => `tank:${tank.ref!.id}`),
    ],
    [activeSelectedScopes, activeSelectedTanks],
  );
  const itemNoun =
    activeSelectedTanks.length > 0
      ? activeSelectedScopes.length > 0
        ? { singular: "equipment group", plural: "equipment groups" }
        : { singular: "tank", plural: "tanks" }
      : { singular: kind, plural: PLURAL_KIND[kind] };
  const pluralKind = PLURAL_KIND[kind];
  // Tracks whether the bar is still mounted so a late-arriving onActionComplete
  // can't resurrect the global toaster push-up after the user navigated away.
  const mountedRef = useRef(true);
  // Tracks current selection length so onActionComplete reflects the latest
  // count, not a value captured when the action was dispatched.
  const selectedCountRef = useRef(selectedIds.length);
  selectedCountRef.current = selectedScopes.length + selectedTanks.length;

  useEffect(() => {
    setActionBarVisible(selectedIds.length > 0 && !isActionBusy);
  }, [selectedIds.length, isActionBusy, setActionBarVisible]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      setActionBarVisible(false);
      onActionBusyChange?.(false);
    };
  }, [onActionBusyChange, setActionBarVisible]);

  const handleActionComplete = useCallback(
    (setHidden: (hidden: boolean) => void) => {
      setIsActionBusy(false);
      onActionBusyChange?.(false);
      setHidden(false);
      if (!mountedRef.current) return;
      setActionBarVisible(selectedCountRef.current > 0);
    },
    [onActionBusyChange, setActionBarVisible],
  );

  return createPortal(
    <ActionBar
      className="fixed right-0 bottom-4 left-0 z-[45] tablet:left-16 desktop:left-50"
      selectedItems={selectedIds}
      selectionMode="subset"
      itemNoun={itemNoun}
      onClose={onClearSelection}
      selectionControls={
        <ActionBarSelectionControls
          onSelectAll={onSelectAllVisible}
          onSelectNone={onClearSelection}
          visibleOnly
          selectAllTestId={`select-all-visible-${pluralKind}-button`}
          selectNoneTestId={`select-none-${pluralKind}-button`}
        />
      }
      renderActions={(setHidden) => {
        const onActionStart = () => {
          setIsActionBusy(true);
          onActionBusyChange?.(true);
          setHidden(true);
          setActionBarVisible(false);
        };
        const onActionComplete = () => handleActionComplete(setHidden);

        return (
          <div className="flex flex-wrap gap-3">
            {activeSelectedTanks.length > 0 && onRefetchTanks ? (
              <TankActionsMenu
                tanks={activeSelectedTanks}
                onRefresh={onRefetchTanks}
                presentation="bulk"
                onActionStart={onActionStart}
                onActionComplete={onActionComplete}
              />
            ) : null}
            {activeSelectedScopes.length > 0 ? (
              <FleetGroupActionsMenu
                scopes={activeSelectedScopes}
                ariaLabel={`Bulk actions for selected ${pluralKind}`}
                testIdPrefix={`fleet-bulk-${kind}-actions`}
                presentation="bulk"
                bulkExtraActions={bulkExtraActions}
                onActionStart={onActionStart}
                onActionComplete={onActionComplete}
              />
            ) : null}
          </div>
        );
      }}
    />,
    document.body,
  );
};

export default FleetGroupListActionBar;
