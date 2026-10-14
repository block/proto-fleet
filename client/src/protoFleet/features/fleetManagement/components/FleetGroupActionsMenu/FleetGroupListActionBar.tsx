import { useMemo, useRef } from "react";

import FleetGroupActionsMenu, { type GroupScope } from "./FleetGroupActionsMenu";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import MiningActionsMenu from "@/protoFleet/features/containers/MiningActionsMenu";
import FleetListActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar/FleetListActionBar";
import useActionBarLifecycle from "@/protoFleet/features/fleetManagement/components/ActionBar/useActionBarLifecycle";
import { ActionBarSelectionControls } from "@/shared/components/ActionBar";
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
  const { isActionBusy, onActionStart, onActionComplete } = useActionBarLifecycle(
    selectedScopes.length + selectedTanks.length,
    onActionBusyChange,
  );
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

  return (
    <FleetListActionBar
      isActionBusy={isActionBusy}
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
    >
      <div className="flex flex-wrap gap-3">
        {activeSelectedTanks.length > 0 && onRefetchTanks ? (
          <MiningActionsMenu
            kind="tank"
            items={activeSelectedTanks}
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
    </FleetListActionBar>
  );
};

export default FleetGroupListActionBar;
